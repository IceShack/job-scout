// Package match scores job ads against the configured profile and decides
// whether they fit the search.
package match

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/IceShack/job-scout/scraper/internal/config"
	"github.com/IceShack/job-scout/scraper/internal/model"
)

// Matcher scores jobs against the configured profile: keyword weights,
// title exclusions, the core stack, the ad's language and its geography.
type Matcher struct {
	cfg              *config.Config
	lang             *LangFilter
	keywords         []compiledKeyword
	exclude          []*regexp.Regexp
	excludeCompanies []string
	boost            []*regexp.Regexp
	requireAny       []*regexp.Regexp
	coreTitle        []*regexp.Regexp
	adjacentTitles   []*regexp.Regexp
	bonuses          []compiledBonus
}

type compiledKeyword struct {
	term   string
	weight int
	re     *regexp.Regexp
}

type compiledBonus struct {
	reason string
	weight int
	terms  []*regexp.Regexp
}

func wordRe(term string) *regexp.Regexp {
	// \b fails around non-word runes like "#" or "."; use lookarounds via
	// explicit boundaries instead: term must not be glued to a letter/digit.
	return regexp.MustCompile(`(?i)(^|[^a-z0-9])` + regexp.QuoteMeta(term) + `($|[^a-z0-9])`)
}

func wordRes(terms []string) []*regexp.Regexp {
	res := make([]*regexp.Regexp, 0, len(terms))
	for _, t := range terms {
		res = append(res, wordRe(t))
	}
	return res
}

func New(cfg *config.Config) (*Matcher, error) {
	lang, err := NewLangFilter(cfg.Profile.Languages)
	if err != nil {
		return nil, fmt.Errorf("profile.languages: %w", err)
	}
	p := cfg.Profile
	m := &Matcher{
		cfg:            cfg,
		lang:           lang,
		exclude:        wordRes(p.Exclude),
		boost:          wordRes(p.TitleBoost),
		requireAny:     wordRes(p.RequireAny),
		coreTitle:      wordRes(p.CoreTitleTerms),
		adjacentTitles: wordRes(p.AdjacentTitles),
	}
	for _, k := range p.Keywords {
		w := k.Weight
		if w == 0 {
			w = 1
		}
		m.keywords = append(m.keywords, compiledKeyword{term: k.Term, weight: w, re: wordRe(k.Term)})
	}
	for _, c := range p.ExcludeCompanies {
		m.excludeCompanies = append(m.excludeCompanies, strings.ToLower(c))
	}
	for _, b := range p.BonusTerms {
		if len(b.Terms) == 0 {
			continue
		}
		reason, weight := b.Reason, b.Weight
		if reason == "" {
			reason = b.Terms[0]
		}
		if weight == 0 {
			weight = 1
		}
		m.bonuses = append(m.bonuses, compiledBonus{reason: reason, weight: weight, terms: wordRes(b.Terms)})
	}
	return m, nil
}

// LanguageExcluded reports whether the ad is written in a language outside
// profile.languages.
func (m *Matcher) LanguageExcluded(title, description string) bool {
	return m.lang.ExcludedJob(title, description)
}

// FocusExcluded reports whether the job falls outside the core stack: an
// adjacent-role title without a core term in the title, or (when
// require_any is configured) no core term anywhere in the ad.
func (m *Matcher) FocusExcluded(j *model.Job) bool {
	title := strings.ToLower(j.Title)
	if matchesAny(m.adjacentTitles, title) && !matchesAny(m.coreTitle, title) {
		return true
	}
	if len(m.requireAny) == 0 {
		return false
	}
	return !matchesAny(m.requireAny, haystack(j))
}

// CompanyExcluded reports whether the company is on the exclusion list;
// used both when matching and to purge previously stored jobs.
func (m *Matcher) CompanyExcluded(name string) bool {
	company := strings.ToLower(name)
	for _, c := range m.excludeCompanies {
		if strings.Contains(company, c) {
			return true
		}
	}
	return false
}

func matchesAny(res []*regexp.Regexp, text string) bool {
	for _, re := range res {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func haystack(j *model.Job) string {
	return strings.ToLower(j.Title + "\n" + strings.Join(j.Tags, " ") + "\n" + j.Description)
}

// Evaluate fills Score, Reasons and Fit; it reports whether the job should
// be kept.
func (m *Matcher) Evaluate(j *model.Job) bool {
	title := strings.ToLower(j.Title)
	if matchesAny(m.exclude, title) {
		return false
	}
	if m.CompanyExcluded(j.Company) {
		return false
	}
	if m.LanguageExcluded(j.Title, j.Description) {
		return false
	}
	if m.FocusExcluded(j) {
		return false
	}

	fit, ok := m.fit(j)
	if !ok {
		return false
	}
	j.Fit = fit

	text := haystack(j)
	for _, k := range m.keywords {
		if k.re.MatchString(text) {
			j.Score += k.weight
			j.Reasons = append(j.Reasons, k.term)
		}
	}
	for _, re := range m.boost {
		if re.MatchString(title) {
			j.Score += m.cfg.Profile.TitleBoostWeight
		}
	}
	for _, b := range m.bonuses {
		if matchesAny(b.terms, text) {
			j.Score += b.weight
			j.Reasons = append(j.Reasons, b.reason)
		}
	}
	return j.Score >= m.cfg.MinScore
}

func containsAny(s string, markers []string) (string, bool) {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return m, true
		}
	}
	return "", false
}

// fit classifies the ad's geography against the configured location: local
// first, then the wider region, with reject markers ruling out anything
// that carries no positive signal.
func (m *Matcher) fit(j *model.Job) (string, bool) {
	loc := m.cfg.Location
	if len(loc.LocalMarkers) == 0 && len(loc.RegionMarkers) == 0 && len(loc.RejectMarkers) == 0 {
		return "", true // no geography preference configured
	}
	// The location field and title carry the strongest geography signal
	// (many boards put the hiring region in the title).
	locTitle := strings.ToLower(j.Location) + " " + strings.ToLower(j.Title)
	desc := strings.ToLower(j.Description)

	if marker, ok := containsAny(locTitle, loc.LocalMarkers); ok {
		return fmt.Sprintf("%s (%s)", loc.LocalLabel, marker), true
	}
	if _, ok := containsAny(desc, loc.LocalMarkers); ok {
		return loc.LocalLabel, true
	}
	if _, restricted := containsAny(locTitle, loc.RejectMarkers); restricted {
		return "", false
	}
	if marker, ok := containsAny(locTitle, loc.RegionMarkers); ok {
		return fmt.Sprintf("%s (%s)", loc.RemoteLabel, marker), true
	}
	// "US or Europe" style descriptions are fine — a region marker wins
	// over a reject marker at description level.
	if _, ok := containsAny(desc, loc.RegionMarkers); ok {
		return loc.RemoteLabel, true
	}
	if _, restricted := containsAny(desc, loc.RejectMarkers); restricted {
		return "", false
	}
	// Remote board, no geography stated.
	if !loc.KeepUnverified {
		return "", false
	}
	return loc.RemoteLabel + " (unverified)", true
}
