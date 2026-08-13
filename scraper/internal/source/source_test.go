package source

import (
	"slices"
	"testing"

	"github.com/IceShack/job-scout/scraper/internal/config"
)

func fullConfig() *config.Config {
	return &config.Config{Sources: map[string]config.Source{
		"devbg":    {Pages: []string{"https://dev.bg/company/jobs/go/"}},
		"justjoin": {Pages: []string{"https://justjoin.it/job-offers/all-locations/go"}, VerifyLimit: 5},
		"arcdev":   {Pages: []string{"https://arc.dev/remote-jobs/golang"}},
		"djinni":   {Pages: []string{"https://djinni.co/jobs/?primary_keyword=Golang"}},
		"jobsbg":   {Pages: []string{"https://www.jobs.bg/front_job_search.php"}},
		"serper":   {Queries: []string{"golang remote europe"}, MaxPagesPerRun: 3},
	}}
}

func built(t *testing.T, cfg *config.Config) []Source {
	t.Helper()
	return All(Deps{Config: cfg, SerperAPIKey: "serper-key", FirecrawlAPIKey: "firecrawl-key"})
}

func TestAllBuildsEveryKnownSource(t *testing.T) {
	var names []string
	for _, s := range built(t, fullConfig()) {
		names = append(names, s.Name())
	}
	for _, want := range config.KnownSources {
		if !slices.Contains(names, want) {
			t.Errorf("source %q from config.KnownSources was not built; got %v", want, names)
		}
	}
	for _, got := range names {
		if !slices.Contains(config.KnownSources, got) {
			t.Errorf("source %q is built but missing from config.KnownSources", got)
		}
	}
}

func TestSourcesNeedingConfigOrKeysAreSkipped(t *testing.T) {
	// No listing URLs, no API keys: only the boards that need neither.
	sources := All(Deps{Config: &config.Config{}})
	var names []string
	for _, s := range sources {
		names = append(names, s.Name())
	}
	want := []string{"remoteok", "remotive", "weworkremotely", "hackernews"}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("sources = %v, want %v", names, want)
	}
}

func serperFrom(t *testing.T, sources []Source) Serper {
	t.Helper()
	for _, s := range sources {
		if got, ok := s.(Serper); ok {
			return got
		}
	}
	t.Fatal("serper source was not built")
	return Serper{}
}

// Web search must stay off the turf of the boards that are enabled — that
// list is derived, so adding a source cannot forget to extend it.
func TestSerperSkipsTheOtherSourcesDomains(t *testing.T) {
	sources := built(t, fullConfig())
	serper := serperFrom(t, sources)
	for _, s := range sources {
		for _, domain := range s.Domains() {
			if !serper.skip("https://www." + domain + "/some/job") {
				t.Errorf("serper does not skip %q, covered by source %q", domain, s.Name())
			}
		}
	}
	if !serper.skip("https://www.linkedin.com/jobs/view/1") {
		t.Error("serper should skip aggregators regardless of enabled sources")
	}
	if serper.skip("https://careers.example.com/jobs/backend") {
		t.Error("serper should crawl company career pages")
	}
}

func TestDisabledSourceReleasesItsDomain(t *testing.T) {
	cfg := fullConfig()
	off := false
	devbg := cfg.Sources["devbg"]
	devbg.Enabled = &off
	cfg.Sources["devbg"] = devbg

	serper := serperFrom(t, built(t, cfg))
	if serper.skip("https://dev.bg/company/jobs/go/") {
		t.Error("dev.bg should be searchable once its dedicated source is off")
	}
}

func TestLastPathSegment(t *testing.T) {
	cases := map[string]string{
		"https://dev.bg/company/jobs/php/":                  "php",
		"https://arc.dev/remote-jobs/golang":                "golang",
		"https://justjoin.it/job-offers/all-locations/go?w": "go",
	}
	for in, want := range cases {
		if got := lastPathSegment(in); got != want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}
