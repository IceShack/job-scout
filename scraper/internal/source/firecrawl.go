package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Firecrawl fetches a page as markdown via api.firecrawl.dev, used to turn
// Serper search hits into scoreable job descriptions.
type Firecrawl struct {
	APIKey string
}

type firecrawlResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Markdown string `json:"markdown"`
		Metadata struct {
			Title      string `json:"title"`
			StatusCode int    `json:"statusCode"`
		} `json:"metadata"`
	} `json:"data"`
	Error string `json:"error"`
}

// Scrape returns the page's main content as markdown plus its title. On a
// 429 (the per-minute rate limit) it waits once for the window to reset —
// the scraper runs on a multi-hour cadence, so a minute of patience beats
// losing the page until the next run.
func (f *Firecrawl) Scrape(ctx context.Context, url string) (markdown, title string, err error) {
	markdown, title, err = f.scrapeOnce(ctx, url)
	if err != nil && strings.Contains(err.Error(), "Rate limit") {
		select {
		case <-time.After(65 * time.Second):
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
		return f.scrapeOnce(ctx, url)
	}
	return markdown, title, err
}

func (f *Firecrawl) scrapeOnce(ctx context.Context, url string) (markdown, title string, err error) {
	payload, err := json.Marshal(map[string]any{
		"url":             url,
		"formats":         []string{"markdown"},
		"onlyMainContent": true,
	})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.firecrawl.dev/v1/scrape", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+f.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out firecrawlResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("firecrawl %s: %w", url, err)
	}
	if !out.Success {
		return "", "", fmt.Errorf("firecrawl %s: %s (http %d)", url, out.Error, resp.StatusCode)
	}
	return out.Data.Markdown, out.Data.Metadata.Title, nil
}
