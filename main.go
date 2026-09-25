package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/go-shiori/go-readability"
)

const (
	InputJSON  = "rss_entries.json"
	MaxWorkers = 5 // Batasi maksimal 5 browser berjalan bersamaan
)

var (
	idDays   = []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	idMonths = []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
)

type RssEntry struct {
	Title           string `json:"title"`
	Link            string `json:"link"`
	PublishedParsed string `json:"published_parsed"`
}

type Job struct {
	Entry RssEntry
}

type Result struct {
	Success map[string]string
	Failed  map[string]string
}

func main() {
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("TABANAN NEWS PROCESSOR (CHROMEDP)")
	fmt.Println(strings.Repeat("=", 80))

	// 1. Baca data dari JSON (Tahap 1)
	file, err := os.Open(InputJSON)
	if err != nil {
		log.Fatalf("[ERROR] Gagal membaca %s. Pastikan fetcher sudah dijalankan. %v", InputJSON, err)
	}
	defer file.Close()

	var entries []RssEntry
	if err := json.NewDecoder(file).Decode(&entries); err != nil {
		log.Fatalf("[ERROR] Gagal parse JSON: %v", err)
	}

	fmt.Printf("[JSON] Membaca %d artikel untuk diproses...\n\n", len(entries))

	// 2. Siapkan output CSV
	today := time.Now().Format("2006-01-02")
	successDir := filepath.Join("output", "success")
	failedDir := filepath.Join("output", "failed")
	os.MkdirAll(successDir, os.ModePerm)
	os.MkdirAll(failedDir, os.ModePerm)

	successCSV := filepath.Join(successDir, fmt.Sprintf("%s.csv", today))
	failedCSV := filepath.Join(failedDir, fmt.Sprintf("%s.csv", today))

	// 3. Konfigurasi Concurrency Worker Pool
	jobs := make(chan Job, len(entries))
	results := make(chan Result, len(entries))
	var wg sync.WaitGroup

	// Jalankan workers
	for w := 1; w <= MaxWorkers; w++ {
		wg.Add(1)
		go worker(w, jobs, results, &wg)
	}

	// Kirim pekerjaan ke antrean
	for _, entry := range entries {
		jobs <- Job{Entry: entry}
	}
	close(jobs)
	wg.Wait()
	close(results)

	// 4. Pengumpulan Hasil
	var successRows []map[string]string
	var failedRows []map[string]string

	for res := range results {
		if res.Success != nil {
			successRows = append(successRows, res.Success)
		} else {
			failedRows = append(failedRows, res.Failed)
		}
	}

	// 5. Simpan ke CSV
	saveCSV(successCSV, getSuccessHeaders(), successRows)
	saveCSV(failedCSV, []string{"title", "google_news_link", "reason"}, failedRows)

	writeGithubOutput(successCSV, failedCSV)

	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("SELESAI\nSuccess: %d\nFailed: %d\n", len(successRows), len(failedRows))
}

// --- FUNGSI WORKER & SCRAPER ---

func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		entry := job.Entry
		title := entry.Title
		if title == "" {
			title = "(no title)"
		}

		fmt.Printf("[Worker %d] Memproses: %.80s...\n", id, title)

		// 1. Resolve URL asli menggunakan chromedp (Pengganti Selenium)
		resolvedURL := resolveURLWithChrome(entry.Link)
		fmt.Printf("      [URL] %s\n", resolvedURL)

		// 2. Scrape isi menggunakan go-readability
		article, err := readability.FromURL(resolvedURL, 30*time.Second)
		if err != nil {
			results <- Result{Failed: map[string]string{
				"title":            title,
				"google_news_link": entry.Link,
				"reason":           fmt.Sprintf("Scrape failed: %v", err),
			}}
			continue
		}

		fullText := strings.TrimSpace(article.TextContent)
		if len(fullText) < 50 {
			results <- Result{Failed: map[string]string{
				"title":            title,
				"google_news_link": resolvedURL,
				"reason":           "Empty body content",
			}}
			continue
		}

		// Parsing string waktu kembali ke time.Time
		pubDt, err := time.Parse("2006-01-02T15:04:05Z07:00", entry.PublishedParsed)
		if err != nil {
			pubDt = time.Now()
		}

		results <- Result{Success: buildSuccessRow(entry, pubDt, fullText)}
	}
}

func resolveURLWithChrome(rawURL string) string {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	// Timeout 15 detik untuk proses resolve
	ctx, cancelTimeout := context.WithTimeout(ctx, 15*time.Second)
	defer cancelTimeout()

	var finalURL string

	// Akses URL dan tunggu JS mengeksekusi redirect selama 2 detik
	err := chromedp.Run(ctx,
		chromedp.Navigate(rawURL),
		chromedp.Sleep(2*time.Second),
		chromedp.Location(&finalURL),
	)

	if err != nil {
		fmt.Printf("      [WARNING] URL resolve error: %v\n", err)
		return rawURL
	}

	if strings.Contains(finalURL, "google.com") {
		return rawURL
	}

	return finalURL
}

// --- FUNGSI PEMBANTU (HELPERS) ---

func buildSuccessRow(entry RssEntry, pubDt time.Time, fullText string) map[string]string {
	originalTitle := entry.Title
	cleanedTitle := cleanTitle(originalTitle)
	sentences := splitSentences(fullText)

	what := getSentences(sentences, 0, 2)
	why := getSentences(sentences, 2, 4)
	prefix := formatHowPrefix(pubDt)

	return map[string]string{
		"title":               cleanedTitle,
		"when_":               pubDt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"where":               "Kabupaten Tabanan, Bali, Indonesia",
		"who":                 originalTitle,
		"what":                what,
		"why":                 why,
		"how":                 fmt.Sprintf("%s\n\n%s", prefix, fullText),
		"source_agent_report": extractSource(originalTitle),
		"location_name":       "Dajan Peken, 82114, Tabanan, Tabanan, Bali, Indonesia",
		"latitude":            "-8.536609490935",
		"longitude":           "115.13552944186335",
		"category":            "5",
		"data_of_information": "2, 7",
		"hashtags":            "kejaksaan, kejatibali, kejaritabanan, tabanan, news",
	}
}

func cleanTitle(title string) string {
	if idx := strings.LastIndex(title, "-"); idx != -1 {
		title = title[:idx]
	}
	re := regexp.MustCompile(`[^a-zA-Z0-9\s]`)
	title = re.ReplaceAllString(title, "")
	return strings.Join(strings.Fields(title), " ")
}

func extractSource(title string) string {
	if idx := strings.LastIndex(title, "-"); idx != -1 {
		return strings.TrimSpace(title[idx+1:])
	}
	return "News"
}

func formatHowPrefix(dt time.Time) string {
	dayName := idDays[dt.Weekday()]
	monthName := idMonths[dt.Month()]
	return fmt.Sprintf("Pada Hari %s , %d %s %d %02d:%02d, Tabanan, Tabanan, Bali",
		dayName, dt.Day(), monthName, dt.Year(), dt.Hour(), dt.Minute())
}

func splitSentences(text string) []string {
	parts := strings.Split(text, ". ")
	var sentences []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			sentences = append(sentences, strings.TrimSpace(p))
		}
	}
	return sentences
}

func getSentences(sentences []string, start, end int) string {
	if start >= len(sentences) {
		return ""
	}
	if end > len(sentences) {
		end = len(sentences)
	}
	return strings.Join(sentences[start:end], ". ") + "."
}

func getSuccessHeaders() []string {
	return []string{"title", "when_", "where", "who", "what", "why", "how", "source_agent_report", "location_name", "latitude", "longitude", "category", "data_of_information", "hashtags"}
}

func saveCSV(filename string, headers []string, rows []map[string]string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("[WARNING] Gagal membuat CSV %s: %v", filename, err)
		return
	}
	defer file.Close()

	file.WriteString("\xef\xbb\xbf") // BOM for Excel UTF-8 compatibility
	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write(headers)
	for _, row := range rows {
		record := make([]string, len(headers))
		for i, h := range headers {
			record[i] = row[h]
		}
		writer.Write(record)
	}
}

func writeGithubOutput(successCSV, failedCSV string) {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return
	}
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		successPosix := strings.ReplaceAll(successCSV, "\\", "/")
		failedPosix := strings.ReplaceAll(failedCSV, "\\", "/")
		fmt.Fprintf(f, "success_csv=%s\nfailed_csv=%s\n", successPosix, failedPosix)
	}
}
