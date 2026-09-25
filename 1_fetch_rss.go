package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/mmcdole/gofeed"
)

const (
	RssURL     = "https://news.google.com/rss/search?q=Tabanan+when:1d&hl=id&gl=ID&ceid=ID:id"
	OutputJSON = "rss_entries.json"
)

// Struktur sederhana untuk disimpan di JSON
type RssEntry struct {
	Title           string `json:"title"`
	Link            string `json:"link"`
	PublishedParsed string `json:"published_parsed"` // Disimpan sebagai string ISO 8601
}

func main() {
	fmt.Println("TABANAN NEWS RSS FETCHER (Tahap 1)")
	fmt.Printf("[RSS] Fetching: %s\n", RssURL)

	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(RssURL)
	if err != nil {
		log.Fatalf("[ERROR] Gagal parsing RSS: %v", err)
	}

	var entries []RssEntry
	for _, item := range feed.Items {
		pubDate := ""
		if item.PublishedParsed != nil {
			pubDate = item.PublishedParsed.Format("2006-01-02T15:04:05Z07:00")
		}

		entries = append(entries, RssEntry{
			Title:           item.Title,
			Link:            item.Link,
			PublishedParsed: pubDate,
		})
	}

	// Simpan ke JSON
	file, err := os.Create(OutputJSON)
	if err != nil {
		log.Fatalf("[ERROR] Gagal membuat file JSON: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		log.Fatalf("[ERROR] Gagal menulis JSON: %v", err)
	}

	fmt.Printf("[SUKSES] Menyimpan %d artikel ke %s\n", len(entries), OutputJSON)
}
