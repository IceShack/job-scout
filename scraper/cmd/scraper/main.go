package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/config"
	"github.com/IceShack/job-scout/scraper/internal/match"
	"github.com/IceShack/job-scout/scraper/internal/model"
	"github.com/IceShack/job-scout/scraper/internal/notify"
	"github.com/IceShack/job-scout/scraper/internal/source"
	"github.com/IceShack/job-scout/scraper/internal/store"
	"github.com/IceShack/job-scout/scraper/internal/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads KEY=VALUE lines for local development; real env vars win.
// In the cluster env comes from the Deployment, no .env exists there.
func loadDotEnv() {
	for _, path := range []string{".env", "../.env"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for line := range strings.Lines(string(data)) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || os.Getenv(key) != "" {
				continue
			}
			os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
		}
		return
	}
}

type app struct {
	cfg      *config.Config
	matcher  *match.Matcher
	store    *store.Store
	telegram *notify.Telegram
	sources  []source.Source

	mu      sync.Mutex
	lastRun time.Time
	running bool
}

func main() {
	loadDotEnv()
	cfgPath := env("CONFIG_PATH", "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("load config", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	matcher, err := match.New(cfg)
	if err != nil {
		slog.Error("build matcher", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	st, err := store.Open(env("STATE_DIR", "data"))
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}

	a := &app{
		cfg:      cfg,
		matcher:  matcher,
		store:    st,
		telegram: notify.NewTelegram(),
		sources: source.All(source.Deps{
			Config:          cfg,
			SerperAPIKey:    os.Getenv("SERPER_API_KEY"),
			FirecrawlAPIKey: os.Getenv("FIRECRAWL_API_KEY"),
			Crawled:         st,
		}),
	}
	names := make([]string, 0, len(a.sources))
	for _, s := range a.sources {
		names = append(names, s.Name())
	}
	slog.Info("starting", "interval", time.Duration(cfg.ScrapeInterval).String(),
		"min_score", cfg.MinScore, "sources", strings.Join(names, ","),
		"telegram", a.telegram.Enabled())

	// Exclusions are re-applied to stored jobs on every boot, so tightening
	// the profile clears out what no longer fits.
	if n, err := st.Purge(func(j *model.Job) bool {
		return a.matcher.CompanyExcluded(j.Company) ||
			a.matcher.LanguageExcluded(j.Title, j.Description) ||
			(!j.Tracked() && a.matcher.FocusExcluded(j))
	}); err != nil {
		slog.Error("purge excluded jobs", "err", err)
	} else if n > 0 {
		slog.Info("purged stored jobs no longer matching the profile", "removed", n)
	}

	runCh := make(chan struct{}, 1)
	trigger := func() {
		select {
		case runCh <- struct{}{}:
		default:
		}
	}
	go a.loop(runCh)
	trigger()

	srv := web.New(web.Options{
		Title: cfg.App.Title,
		Store: st,
		Run:   trigger,
		LastRun: func() time.Time {
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.lastRun
		},
		Password: os.Getenv("SITE_PASSWORD"),
	})
	if os.Getenv("SITE_PASSWORD") == "" {
		slog.Warn("SITE_PASSWORD not set — web UI is unauthenticated")
	}
	addr := ":" + env("PORT", "8080")
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		slog.Error("http server", "err", err)
		os.Exit(1)
	}
}

func (a *app) loop(runCh <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(a.cfg.ScrapeInterval))
	for {
		select {
		case <-runCh:
		case <-ticker.C:
		}
		a.scrape()
	}
}

// verifyDescriptions re-reads the ad page for sources whose listing cards
// carry no text, so an ad written in a language you don't read can't slip
// through on an English-looking title. Rejected ads are stored as hidden
// tombstones — their content key then also suppresses sibling URLs for the
// same ad. Verified URLs are cached; anything beyond a source's per-run cap
// simply waits for the next run.
func (a *app) verifyDescriptions(ctx context.Context, matched []model.Job) []model.Job {
	verifiers := map[string]source.DescriptionFetcher{}
	for _, s := range a.sources {
		if v, ok := s.(source.DescriptionFetcher); ok && v.VerifyLimit() > 0 {
			verifiers[s.Name()] = v
		}
	}
	if len(verifiers) == 0 {
		return matched
	}

	out := make([]model.Job, 0, len(matched))
	fetches, rejected := map[string]int{}, map[string]int{}
	for _, j := range matched {
		v, ok := verifiers[j.Source]
		if !ok || a.store.WasCrawled(j.URL) {
			out = append(out, j)
			continue
		}
		if fetches[j.Source] >= v.VerifyLimit() {
			continue // re-listed next run, verified then
		}
		fetches[j.Source]++
		desc, err := v.FetchDescription(ctx, j.URL)
		if err != nil {
			slog.Warn("verify description", "source", j.Source, "err", err)
			continue // retry next run
		}
		a.store.MarkCrawled(j.URL)
		if a.matcher.LanguageExcluded(j.Title, desc) {
			j.Hidden = true
			rejected[j.Source]++
		} else {
			j.Description = desc
		}
		out = append(out, j)
	}
	for name, n := range fetches {
		slog.Info("language check", "source", name, "fetched", n, "rejected", rejected[name])
	}
	return out
}

func (a *app) scrape() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		a.lastRun = time.Now()
		a.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		matched []model.Job
	)
	for _, src := range a.sources {
		wg.Add(1)
		go func(src source.Source) {
			defer wg.Done()
			jobs, err := src.Fetch(ctx)
			if err != nil {
				slog.Error("fetch", "source", src.Name(), "err", err)
				return
			}
			kept := 0
			mu.Lock()
			defer mu.Unlock()
			for i := range jobs {
				if a.matcher.Evaluate(&jobs[i]) {
					matched = append(matched, jobs[i])
					kept++
				}
			}
			slog.Info("fetched", "source", src.Name(), "jobs", len(jobs), "matched", kept)
		}(src)
	}
	wg.Wait()

	matched = a.verifyDescriptions(ctx, matched)

	fresh, err := a.store.Merge(matched, time.Now())
	if err != nil {
		slog.Error("store save", "err", err)
	}
	slog.Info("scrape done", "matched", len(matched), "new", len(fresh))
	if err := a.telegram.NotifyNew(ctx, fresh); err != nil {
		slog.Error("telegram notify", "err", err)
	}
}
