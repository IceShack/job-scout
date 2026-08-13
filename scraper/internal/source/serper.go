package source

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// CrawlCache remembers which URLs were already crawled so the Firecrawl
// budget is spent on new pages each run.
type CrawlCache interface {
	WasCrawled(url string) bool
	MarkCrawled(url string)
}

// Serper discovers job pages beyond the fixed boards via Google search
// (google.serper.dev) and enriches fresh hits through Firecrawl so the
// matcher can score the full page text instead of a search snippet.
type Serper struct {
	APIKey    string
	Firecrawl *Firecrawl // nil disables page enrichment
	Queries   []string
	MaxCrawl  int // Firecrawl budget per run
	Crawled   CrawlCache
	// SkipDomains are hosts whose hits would duplicate a dedicated source
	// or waste Firecrawl credits; see skipDomains in source.go.
	SkipDomains []string
}

func (Serper) Name() string { return "serper" }

func (Serper) Domains() []string { return nil }

func (s Serper) skip(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range s.SkipDomains {
		if strings.Contains(host, d) {
			return true
		}
	}
	return false
}

type serperResponse struct {
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic"`
}

func (s Serper) search(ctx context.Context, query string) (*serperResponse, error) {
	payload, err := json.Marshal(map[string]any{
		"q":   query,
		"num": 20,
		// Only results indexed within the last week — the scraper runs
		// several times a day, older hits are already known or stale.
		"tbs": "qdr:w",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://google.serper.dev/search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", s.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out serperResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s Serper) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, q := range s.Queries {
		resp, err := s.search(ctx, q)
		if err != nil {
			lastErr = err
			continue
		}
		for _, r := range resp.Organic {
			if r.Link == "" || seen[r.Link] || s.skip(r.Link) {
				continue
			}
			seen[r.Link] = true
			host := ""
			if u, err := url.Parse(r.Link); err == nil {
				host = strings.TrimPrefix(u.Hostname(), "www.")
			}
			jobs = append(jobs, model.Job{
				Source:      "serper",
				Title:       r.Title,
				Company:     host,
				URL:         r.Link,
				Description: r.Snippet,
			})
		}
	}
	s.enrich(ctx, jobs)
	if len(jobs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return jobs, nil
}

// enrich replaces search snippets with full page text for up to MaxCrawl
// not-yet-crawled URLs. Failures keep the snippet and stay uncached so the
// next run retries them.
func (s Serper) enrich(ctx context.Context, jobs []model.Job) {
	if s.Firecrawl == nil {
		return
	}
	crawls := 0
	for i := range jobs {
		if crawls >= s.MaxCrawl {
			slog.Info("serper: firecrawl budget exhausted", "budget", s.MaxCrawl, "candidates", len(jobs))
			return
		}
		if s.Crawled != nil && s.Crawled.WasCrawled(jobs[i].URL) {
			continue
		}
		crawls++
		markdown, title, err := s.Firecrawl.Scrape(ctx, jobs[i].URL)
		if err != nil {
			slog.Warn("firecrawl", "err", err)
			continue
		}
		if s.Crawled != nil {
			s.Crawled.MarkCrawled(jobs[i].URL)
		}
		jobs[i].Description = truncate(stripHTML(markdown), 4000)
		if title != "" {
			jobs[i].Title = truncate(title, 140)
		}
	}
}
