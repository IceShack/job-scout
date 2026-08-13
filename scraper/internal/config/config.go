// Package config holds the single YAML file that describes a search: what
// to look for, where you'd work, and which boards to read.
package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration for YAML strings like "6h".
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// KnownSources lists every board the scraper can read. source.All builds
// exactly these; anything else in the config file is a typo.
var KnownSources = []string{
	"remoteok", "remotive", "weworkremotely", "hackernews",
	"devbg", "justjoin", "arcdev", "jobsbg", "serper",
}

type App struct {
	// Title names the instance in the browser tab and the page heading. It
	// also salts the auth cookie, so changing it logs everyone out once.
	Title string `yaml:"title"`
}

type Keyword struct {
	Term   string `yaml:"term"`
	Weight int    `yaml:"weight"`
}

// Bonus adds weight when any of its terms appears anywhere in the ad —
// e.g. contract markers for a freelancer, "visa sponsorship" for someone
// relocating.
type Bonus struct {
	Terms  []string `yaml:"terms"`
	Weight int      `yaml:"weight"`
	// Reason is listed with the score in the UI; defaults to the first term.
	Reason string `yaml:"reason"`
}

// Profile describes what you are looking for.
type Profile struct {
	// RequireAny keeps a job only when at least one of these terms appears
	// somewhere in it — the core stack the search is about.
	RequireAny []string `yaml:"require_any"`
	// CoreTitleTerms is the title-only variant of RequireAny, used to
	// redeem an AdjacentTitles match. Defaults to RequireAny.
	CoreTitleTerms []string `yaml:"core_title_terms"`
	// AdjacentTitles name neighbouring roles: a title containing one is
	// dropped unless a core term is also in the title, so with
	// adjacent_titles [python] and core_title_terms [php], "Python
	// Developer" goes and "PHP/Python Developer" stays.
	AdjacentTitles []string `yaml:"adjacent_titles"`
	// Keywords score the title, tags and description.
	Keywords []Keyword `yaml:"keywords"`
	// TitleBoost terms add TitleBoostWeight when they appear in the title.
	TitleBoost       []string `yaml:"title_boost"`
	TitleBoostWeight int      `yaml:"title_boost_weight"`
	// Exclude drops a job outright when a term appears in its title.
	Exclude []string `yaml:"exclude"`
	// ExcludeCompanies drops jobs whose company name contains one of these
	// (case-insensitive) — e.g. places already applied to or ruled out.
	ExcludeCompanies []string `yaml:"exclude_companies"`
	// Languages lists the ad languages worth reading, as two-letter codes.
	// Empty means no language filtering. See match.SupportedLanguages.
	Languages  []string `yaml:"languages"`
	BonusTerms []Bonus  `yaml:"bonus_terms"`
}

// Location describes where you'd work. Markers are matched as substrings
// against the ad's location, title and description; leaving all three
// lists empty disables the geography check.
type Location struct {
	// LocalLabel is the fit reported for ads that name somewhere you live;
	// RemoteLabel is for ads open to your wider region.
	LocalLabel  string `yaml:"local_label"`
	RemoteLabel string `yaml:"remote_label"`
	// KeepUnverified keeps ads from remote boards that state no geography
	// at all, flagged as unverified. Defaults to true.
	KeepUnverified bool `yaml:"keep_unverified"`
	// LocalMarkers name your country and its cities.
	LocalMarkers []string `yaml:"local_markers"`
	// RegionMarkers name the wider region you can work remotely from.
	RegionMarkers []string `yaml:"region_markers"`
	// RejectMarkers name regions that rule an ad out, unless a region
	// marker also matches.
	RejectMarkers []string `yaml:"reject_markers"`
}

// Source configures one board. In YAML it accepts either a bare bool
// ("remoteok: true") or a mapping with that board's options.
type Source struct {
	// Enabled defaults to true, including for boards left out of the file.
	Enabled *bool `yaml:"enabled"`
	// Pages are listing URLs to scrape (devbg, justjoin, arcdev, jobsbg).
	Pages []string `yaml:"pages"`
	// Feeds are RSS URLs (weworkremotely).
	Feeds []string `yaml:"feeds"`
	// Categories are API category slugs (remotive).
	Categories []string `yaml:"categories"`
	// Require keeps only posts containing this text (hackernews).
	Require string `yaml:"require"`
	// VerifyLimit caps the offer pages fetched per run to check an ad's
	// real language (justjoin).
	VerifyLimit int `yaml:"verify_limit"`
	// Queries are web searches to run (serper).
	Queries []string `yaml:"queries"`
	// MaxPagesPerRun caps Firecrawl scrapes per run (serper credit budget).
	MaxPagesPerRun int `yaml:"max_pages_per_run"`
	// SkipDomains are extra hosts to ignore, on top of the domains the
	// enabled boards already cover (serper).
	SkipDomains []string `yaml:"skip_domains"`
}

func (s *Source) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return err
		}
		s.Enabled = &enabled
		return nil
	}
	// Alias the type so decoding the mapping doesn't recurse into here.
	type source Source
	var raw source
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*s = Source(raw)
	return nil
}

type Config struct {
	App            App               `yaml:"app"`
	ScrapeInterval Duration          `yaml:"scrape_interval"`
	MinScore       int               `yaml:"min_score"`
	Profile        Profile           `yaml:"profile"`
	Location       Location          `yaml:"location"`
	Sources        map[string]Source `yaml:"sources"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		App:            App{Title: "job-scout"},
		ScrapeInterval: Duration(6 * time.Hour),
		MinScore:       4,
		Profile:        Profile{TitleBoostWeight: 2},
		Location:       Location{LocalLabel: "local", RemoteLabel: "remote", KeepUnverified: true},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(cfg.Profile.Keywords) == 0 {
		return nil, fmt.Errorf("%s: profile.keywords must not be empty", path)
	}
	for name := range cfg.Sources {
		if !slices.Contains(KnownSources, name) {
			return nil, fmt.Errorf("%s: unknown source %q (known: %s)",
				path, name, strings.Join(KnownSources, ", "))
		}
	}
	if len(cfg.Profile.CoreTitleTerms) == 0 {
		cfg.Profile.CoreTitleTerms = cfg.Profile.RequireAny
	}
	cfg.applySourceDefaults()
	return cfg, nil
}

// applySourceDefaults fills the per-source budgets, so the values in
// effect are readable off the Config instead of hidden in each source.
func (c *Config) applySourceDefaults() {
	if c.Sources == nil {
		c.Sources = map[string]Source{}
	}
	defaults := map[string]func(*Source){
		"justjoin": func(s *Source) {
			if s.VerifyLimit == 0 {
				s.VerifyLimit = 25
			}
		},
		"serper": func(s *Source) {
			if s.MaxPagesPerRun == 0 {
				s.MaxPagesPerRun = 10
			}
		},
	}
	for name, apply := range defaults {
		s := c.Sources[name]
		apply(&s)
		c.Sources[name] = s
	}
}

// SourceEnabled defaults to true when the source is not listed.
func (c *Config) SourceEnabled(name string) bool {
	s, ok := c.Sources[name]
	return !ok || s.Enabled == nil || *s.Enabled
}

// Source returns the block for a board, zero-valued when absent.
func (c *Config) Source(name string) Source { return c.Sources[name] }
