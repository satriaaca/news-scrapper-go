package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/mmcdole/gofeed"
)

const OutputJSON = "rss_entries.json"

var whitelistDomains = []string{
	"kompas.com",
	"rri.co.id",
	"tbinterpol.com",
	"jejakkasus.info",
	"bisnisbali.com",
	"balipost.com",
	"mediapelangi.com",
	"suryaindonesia.net",
	"kabarnusa.com",
	"radarnusantara.com",
	"baliportalnews.com",
	"nusabali.com",
}

type RssEntry struct {
	Title           string `json:"title"`
	Link            string `json:"link"`
	PublishedParsed string `json:"published_parsed"`
}

func main() {
	fmt.Println("TABANAN NEWS RSS FETCHER (Tahap 1 - Whitelist Mode)")

	// 1. Rangkai query dengan operator OR dan site:
	// Hasil: Tabanan (site:kompas.com OR site:rri.co.id OR ...)
	var siteQueries []string
	for _, domain := range whitelistDomains {
		siteQueries = append(siteQueries, fmt.Sprintf("site:%s", domain))
	}
	
	rawQuery := fmt.Sprintf("Tabanan (%s)", strings.Join(siteQueries, " OR "))
	encodedQuery := url.QueryEscape(rawQuery)

	// 2. Bentuk URL RSS akhir
	rssURL := fmt.Sprintf("https://news.google.com/rss/search?q=%s+when:1d&hl=id&gl=ID&ceid=ID:id", encodedQuery)
	fmt.Printf("[RSS] Fetching URL: %s\n", rssURL)

	// 3. Ambil RSS
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(rssURL)
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

	// 4. Simpan ke JSON
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

	fmt.Printf("[SUKSES] Menyimpan %d artikel (Hanya dari Whitelist) ke %s\n", len(entries), OutputJSON)
}