// Package source fetches job ads from the configured boards.
package source

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/config"
	"github.com/IceShack/job-scout/scraper/internal/model"
)

// Source fetches job ads from one site.
type Source interface {
	Name() string
	// Domains lists the hosts this source covers so web search can skip
	// them — its hits would only duplicate what the board already gives us.
	Domains() []string
	Fetch(ctx context.Context) ([]model.Job, error)
}

// DescriptionFetcher is implemented by boards whose listing pages carry no
// ad text. The scraper re-reads the offer page for those, to check what
// language the ad is actually written in.
type DescriptionFetcher interface {
	FetchDescription(ctx context.Context, url string) (string, error)
	// VerifyLimit caps how many pages are fetched per run.
	VerifyLimit() int
}

// Deps carries what sources need beyond the config file: API keys from the
// environment and the crawl cache.
type Deps struct {
	Config          *config.Config
	SerperAPIKey    string
	FirecrawlAPIKey string
	Crawled         CrawlCache
}

// Domains that never repay a crawl: aggregators that republish the boards
// above, and social platforms Firecrawl refuses outright.
var alwaysSkipDomains = []string{
	"linkedin.com", "indeed.", "glassdoor.", "ziprecruiter.com",
	"jooble.org", "talent.com", "jobrapido.com", "bebee.com", "dailyremote.com",
	"facebook.com", "instagram.com", "twitter.com", "x.com",
	"youtube.com", "reddit.com", "tiktok.com",
}

// All builds every enabled source. Boards needing an API key or a listing
// URL are skipped (with a log line) when it is missing, so a config with
// no keys still runs the free boards.
func All(d Deps) []Source {
	cfg := d.Config
	var sources []Source
	add := func(name string, build func(config.Source) Source) {
		if !cfg.SourceEnabled(name) {
			return
		}
		if s := build(cfg.Source(name)); s != nil {
			sources = append(sources, s)
		}
	}
	// pages returns nil (skipping the source) when no listing URL is set —
	// what to search for is exactly what these boards take from the config.
	pages := func(name string, c config.Source) []string {
		if len(c.Pages) == 0 {
			slog.Warn("source disabled: no pages configured", "source", name)
			return nil
		}
		return c.Pages
	}

	add("remoteok", func(config.Source) Source { return RemoteOK{} })
	add("remotive", func(c config.Source) Source {
		categories := c.Categories
		if len(categories) == 0 {
			categories = []string{"software-dev"}
		}
		return Remotive{Categories: categories}
	})
	add("weworkremotely", func(c config.Source) Source {
		feeds := c.Feeds
		if len(feeds) == 0 {
			feeds = []string{"https://weworkremotely.com/categories/remote-programming-jobs.rss"}
		}
		return WeWorkRemotely{Feeds: feeds}
	})
	add("hackernews", func(c config.Source) Source { return HackerNews{Require: c.Require} })
	add("devbg", func(c config.Source) Source {
		p := pages("devbg", c)
		if p == nil {
			return nil
		}
		return DevBG{Pages: p}
	})
	add("justjoin", func(c config.Source) Source {
		p := pages("justjoin", c)
		if p == nil {
			return nil
		}
		return JustJoin{Pages: p, Limit: c.VerifyLimit}
	})
	add("arcdev", func(c config.Source) Source {
		p := pages("arcdev", c)
		if p == nil {
			return nil
		}
		return ArcDev{Pages: p}
	})
	add("djinni", func(c config.Source) Source {
		p := pages("djinni", c)
		if p == nil {
			return nil
		}
		return Djinni{Pages: p}
	})
	add("jobsbg", func(c config.Source) Source {
		p := pages("jobsbg", c)
		if p == nil {
			return nil
		}
		if d.FirecrawlAPIKey == "" {
			slog.Warn("source disabled: needs FIRECRAWL_API_KEY", "source", "jobsbg")
			return nil
		}
		return JobsBG{Pages: p, Firecrawl: &Firecrawl{APIKey: d.FirecrawlAPIKey}}
	})

	// Serper comes last: it skips the domains the boards above already cover.
	add("serper", func(c config.Source) Source {
		if d.SerperAPIKey == "" || len(c.Queries) == 0 {
			if d.SerperAPIKey == "" {
				slog.Warn("source disabled: needs SERPER_API_KEY", "source", "serper")
			} else {
				slog.Warn("source disabled: no queries configured", "source", "serper")
			}
			return nil
		}
		s := Serper{
			APIKey:      d.SerperAPIKey,
			Queries:     c.Queries,
			MaxCrawl:    c.MaxPagesPerRun,
			Crawled:     d.Crawled,
			SkipDomains: skipDomains(sources, c.SkipDomains),
		}
		if d.FirecrawlAPIKey != "" {
			s.Firecrawl = &Firecrawl{APIKey: d.FirecrawlAPIKey}
		}
		return s
	})
	return sources
}

// skipDomains collects the hosts covered by the other enabled sources,
// so adding a board automatically keeps web search off its turf.
func skipDomains(sources []Source, extra []string) []string {
	domains := make([]string, 0, len(alwaysSkipDomains)+len(extra)+2*len(sources))
	domains = append(domains, alwaysSkipDomains...)
	domains = append(domains, extra...)
	for _, s := range sources {
		domains = append(domains, s.Domains()...)
	}
	return domains
}

var client = &http.Client{Timeout: 30 * time.Second}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) job-scout/1.0")
	req.Header.Set("Accept", "*/*")
	// Boards that localise by request headers must give us the English
	// page: it is the version we store a link to, and the language filter
	// judges ads on the text we actually fetched.
	req.Header.Set("Accept-Language", "en")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

// stripHTML flattens an HTML fragment to plain text.
func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " \n ")), " "))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// lastPathSegment names a listing page by its category slug, e.g.
// "https://dev.bg/company/jobs/php/" → "php".
func lastPathSegment(pageURL string) string {
	path := strings.TrimSuffix(pageURL, "/")
	if u, err := url.Parse(pageURL); err == nil {
		path = strings.TrimSuffix(u.Path, "/")
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
