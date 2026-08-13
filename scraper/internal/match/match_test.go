package match

import (
	"testing"

	"github.com/IceShack/job-scout/scraper/internal/config"
	"github.com/IceShack/job-scout/scraper/internal/model"
)

// testConfig is a PHP/Go search run from Bulgaria — a worked example of
// every profile and location knob at once.
func testConfig() *config.Config {
	return &config.Config{
		MinScore: 4,
		Profile: config.Profile{
			Keywords: []config.Keyword{
				{Term: "php", Weight: 4},
				{Term: "symfony", Weight: 5},
				{Term: "golang", Weight: 3},
				{Term: "python", Weight: 1},
			},
			TitleBoost:       []string{"php", "backend"},
			TitleBoostWeight: 2,
			Exclude:          []string{"junior", "designer"},
			ExcludeCompanies: []string{"reward gateway"},
			RequireAny:       []string{"php", "symfony", "laravel", "doctrine", "golang"},
			CoreTitleTerms:   []string{"php", "symfony", "laravel", "golang", "go"},
			AdjacentTitles:   []string{"python"},
			Languages:        []string{"en", "de"},
			BonusTerms: []config.Bonus{
				{Terms: []string{"contract", "freelance", "b2b", "contractor"}, Weight: 1, Reason: "contract-friendly"},
			},
		},
		Location: config.Location{
			LocalLabel:     "bulgaria",
			RemoteLabel:    "eu-remote",
			KeepUnverified: true,
			LocalMarkers:   []string{"bulgaria", "sofia", "plovdiv", "varna", "burgas"},
			RegionMarkers: []string{
				"worldwide", "anywhere", "global", "europe", "european", "emea",
				"eu only", "eu-only", "eu timezone", "eu time", "cet", "eet", "utc",
			},
			RejectMarkers: []string{
				"usa only", "us only", "us-only", "united states", "usa", "u.s.",
				"north america", "canada", "latam", "americas", "apac",
				"australia", "uk only", "us timezones", "us time",
				"mexico", "brazil", "india",
			},
		},
	}
}

func newMatcher(t *testing.T, cfg *config.Config) *Matcher {
	t.Helper()
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func testMatcher(t *testing.T) *Matcher { return newMatcher(t, testConfig()) }

func TestFocusExcluded(t *testing.T) {
	m := testMatcher(t)
	cases := []struct {
		name string
		job  model.Job
		want bool
	}{
		{"adjacent role title", model.Job{Title: "Senior Python Developer", Description: "Django, PHP a plus"}, true},
		{"adjacent plus core title", model.Job{Title: "Fullstack PHP/Python Developer", Description: ""}, false},
		{"no core term anywhere", model.Job{Title: "Backend Engineer", Description: "Node.js TypeScript MongoDB"}, true},
		{"golang in tags", model.Job{Title: "Backend Engineer", Tags: []string{"Golang", "Kubernetes"}}, false},
		{"php in description", model.Job{Title: "Software Engineer", Description: "Our stack is PHP and MySQL"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.FocusExcluded(&tc.job); got != tc.want {
				t.Fatalf("FocusExcluded = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	m := testMatcher(t)
	cases := []struct {
		name string
		job  model.Job
		keep bool
		fit  string
	}{
		{
			name: "symfony job in bulgaria",
			job:  model.Job{Title: "Senior PHP Developer", Location: "Sofia, Bulgaria", Description: "Symfony, MySQL"},
			keep: true,
			fit:  "bulgaria (bulgaria)",
		},
		{
			name: "remote europe php",
			job:  model.Job{Title: "Backend Engineer", Location: "Europe", Description: "PHP and Symfony stack"},
			keep: true,
			fit:  "eu-remote (europe)",
		},
		{
			name: "remote board without geography",
			job:  model.Job{Title: "PHP Engineer", Location: "", Description: "Symfony and MySQL"},
			keep: true,
			fit:  "eu-remote (unverified)",
		},
		{
			name: "us only is rejected despite stack",
			job:  model.Job{Title: "PHP Developer", Location: "USA only", Description: "Symfony"},
			keep: false,
		},
		{
			name: "rejected region in title",
			job:  model.Job{Title: "Staff Engineer (Mexico City)", Location: "", Description: "PHP Symfony anywhere"},
			keep: false,
		},
		{
			name: "excluded company",
			job:  model.Job{Title: "Senior PHP Developer", Company: "Reward Gateway Bulgaria", Location: "Sofia", Description: "Symfony"},
			keep: false,
		},
		{
			name: "excluded title",
			job:  model.Job{Title: "Junior PHP Developer", Location: "Bulgaria", Description: "Symfony"},
			keep: false,
		},
		{
			name: "low score is rejected",
			job:  model.Job{Title: "Rust Engineer", Location: "Europe", Description: "Rust only"},
			keep: false,
		},
		{
			name: "keyword inside word does not count",
			job:  model.Job{Title: "Engineer", Location: "Europe", Description: "phpectomy nothing"},
			keep: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := tc.job
			keep := m.Evaluate(&j)
			if keep != tc.keep {
				t.Fatalf("keep = %v, want %v (score %d, fit %q)", keep, tc.keep, j.Score, j.Fit)
			}
			if tc.fit != "" && j.Fit != tc.fit {
				t.Fatalf("fit = %q, want %q", j.Fit, tc.fit)
			}
		})
	}
}

func TestEvaluateRegionBeatsRejectInDescription(t *testing.T) {
	m := testMatcher(t)
	j := model.Job{Title: "PHP Engineer", Location: "Remote", Description: "Hiring in the United States or Europe. Symfony."}
	if !m.Evaluate(&j) {
		t.Fatalf("expected keep, got reject (fit %q)", j.Fit)
	}
}

func TestBonusTermsScore(t *testing.T) {
	m := testMatcher(t)
	plain := model.Job{Title: "PHP Engineer", Location: "Europe", Description: "Symfony"}
	m.Evaluate(&plain)
	contract := model.Job{Title: "PHP Engineer", Location: "Europe", Description: "Symfony, B2B contract"}
	m.Evaluate(&contract)
	if contract.Score != plain.Score+1 {
		t.Fatalf("bonus score = %d, want %d", contract.Score, plain.Score+1)
	}
	if len(contract.Reasons) == 0 || contract.Reasons[len(contract.Reasons)-1] != "contract-friendly" {
		t.Fatalf("reasons = %v, want a contract-friendly entry", contract.Reasons)
	}
}

func TestKeepUnverifiedFalseDropsUnstatedGeography(t *testing.T) {
	cfg := testConfig()
	cfg.Location.KeepUnverified = false
	m := newMatcher(t, cfg)
	j := model.Job{Title: "PHP Engineer", Location: "", Description: "Symfony and MySQL"}
	if m.Evaluate(&j) {
		t.Fatalf("expected reject, got keep (fit %q)", j.Fit)
	}
}

// A second search — Go/TypeScript out of Germany — proves the geography
// and stack rules come from the config rather than the code.
func TestEvaluateWithADifferentProfile(t *testing.T) {
	cfg := &config.Config{
		MinScore: 3,
		Profile: config.Profile{
			Keywords:         []config.Keyword{{Term: "golang", Weight: 4}, {Term: "typescript", Weight: 3}},
			RequireAny:       []string{"golang", "typescript"},
			CoreTitleTerms:   []string{"go", "golang", "typescript"},
			AdjacentTitles:   []string{"php"},
			TitleBoostWeight: 2,
			Languages:        []string{"en", "de"},
		},
		Location: config.Location{
			LocalLabel:    "germany",
			RemoteLabel:   "eu-remote",
			LocalMarkers:  []string{"germany", "berlin"},
			RegionMarkers: []string{"europe"},
			RejectMarkers: []string{"united states"},
		},
	}
	m := newMatcher(t, cfg)

	local := model.Job{Title: "Go Engineer", Location: "Berlin, Germany", Description: "Golang services"}
	if !m.Evaluate(&local) || local.Fit != "germany (germany)" {
		t.Fatalf("local job: keep=%v fit=%q", local.Fit != "", local.Fit)
	}
	rejected := model.Job{Title: "Go Engineer", Location: "United States", Description: "Golang services"}
	if m.Evaluate(&rejected) {
		t.Fatal("US job should be rejected")
	}
	adjacent := model.Job{Title: "PHP Developer", Location: "Europe", Description: "Golang sometimes"}
	if m.Evaluate(&adjacent) {
		t.Fatal("adjacent-role title without a core term should be rejected")
	}
}

func TestUnsupportedLanguageIsAnError(t *testing.T) {
	cfg := testConfig()
	cfg.Profile.Languages = []string{"en", "klingon"}
	if _, err := New(cfg); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}
