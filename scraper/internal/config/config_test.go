package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The shipped example is the documentation for this schema, so it has to
// keep parsing.
func TestLoadExample(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("load config.example.yaml: %v", err)
	}
	if cfg.App.Title == "" || len(cfg.Profile.Keywords) == 0 {
		t.Fatal("example config is missing a title or keywords")
	}
	if !cfg.SourceEnabled("remoteok") || cfg.SourceEnabled("devbg") {
		t.Fatal("example config: remoteok should be on and devbg off")
	}
	if got := cfg.Source("justjoin").VerifyLimit; got != 25 {
		t.Fatalf("justjoin verify_limit = %d, want 25", got)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimal = "profile:\n  keywords:\n    - {term: golang, weight: 3}\n"

func TestDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if time.Duration(cfg.ScrapeInterval) != 6*time.Hour {
		t.Errorf("scrape_interval = %v, want 6h", time.Duration(cfg.ScrapeInterval))
	}
	if cfg.MinScore != 4 || cfg.Profile.TitleBoostWeight != 2 {
		t.Errorf("min_score = %d, title_boost_weight = %d", cfg.MinScore, cfg.Profile.TitleBoostWeight)
	}
	if !cfg.Location.KeepUnverified {
		t.Error("keep_unverified should default to true")
	}
	// Boards left out of the file run with their defaults.
	if !cfg.SourceEnabled("remoteok") {
		t.Error("an unlisted source should be enabled")
	}
	if cfg.Source("serper").MaxPagesPerRun != 10 {
		t.Error("serper max_pages_per_run should default to 10")
	}
}

func TestCoreTitleTermsDefaultToRequireAny(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimal+"  require_any: [golang, typescript]\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Profile.CoreTitleTerms; len(got) != 2 || got[0] != "golang" {
		t.Fatalf("core_title_terms = %v, want require_any", got)
	}
}

func TestSourceAcceptsBoolOrMapping(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimal+"\nsources:\n  remoteok: false\n  devbg:\n    enabled: true\n    pages: [https://dev.bg/company/jobs/go/]\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SourceEnabled("remoteok") {
		t.Error("remoteok: false should disable the source")
	}
	if !cfg.SourceEnabled("devbg") || len(cfg.Source("devbg").Pages) != 1 {
		t.Error("devbg mapping form was not parsed")
	}
}

func TestRejectsBadConfig(t *testing.T) {
	if _, err := Load(writeConfig(t, "min_score: 3\n")); err == nil {
		t.Error("expected an error when keywords are empty")
	}
	if _, err := Load(writeConfig(t, minimal+"\nsources:\n  linkedin: true\n")); err == nil {
		t.Error("expected an error for an unknown source name")
	}
}
