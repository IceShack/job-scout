package source

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// JobsBG scrapes jobs.bg (the largest Bulgarian general job board) and its
// IT-focused skin tech.bg — same platform, same database, same ad ids, so
// both feed one source and dedupe by id. The sites sit behind Cloudflare
// bot protection, so listing pages are fetched through Firecrawl and
// parsed from the returned markdown. Ads always link to the English pages
// (jobs.bg/en/job/<id>) regardless of which skin found them.
type JobsBG struct {
	Pages     []string
	Firecrawl *Firecrawl
}

func (JobsBG) Name() string { return "jobsbg" }

func (JobsBG) Domains() []string { return []string{"jobs.bg", "tech.bg"} }

func (j JobsBG) Fetch(ctx context.Context) ([]model.Job, error) {
	if j.Firecrawl == nil {
		return nil, fmt.Errorf("jobsbg requires FIRECRAWL_API_KEY")
	}
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, page := range j.Pages {
		md, _, err := j.Firecrawl.Scrape(ctx, page)
		if err != nil {
			lastErr = err
			continue
		}
		for _, job := range parseJobsBG(md) {
			if !seen[job.URL] {
				seen[job.URL] = true
				jobs = append(jobs, job)
			}
		}
	}
	if len(jobs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return jobs, nil
}

// Markdown patterns: skill icons precede each job link; the company link
// carries the job id in its query string.
var (
	jobsBGSkillRe = regexp.MustCompile(`!\[([^\]]+)\]\(https://static\.(?:jobs|tech)\.bg/mobile/images/skills/`)
	jobsBGJobRe   = regexp.MustCompile(`\]\(https://www\.(?:jobs|tech)\.bg/job/(\d+)[^)]*?"([^"]+)"\)`)
	// Company URLs use either numeric ids or slugs. The ?job=<id> query is
	// present on some links but not all; without it the company attaches
	// to the preceding job card.
	jobsBGCompanyRe = regexp.MustCompile(`\(https://www\.(?:jobs|tech)\.bg/company/[\w-]+(?:\?job=(\d+))?[^)]*?"([^"]+)"\)`)
)

type jobsBGEvent struct {
	pos  int
	kind int // 0 skill, 1 job, 2 company
	a, b string
}

// parseJobsBG walks the markdown in document order: skill icons accumulate
// until a job link consumes them; company links attach to their job by id.
func parseJobsBG(md string) []model.Job {
	var events []jobsBGEvent
	for _, m := range jobsBGSkillRe.FindAllStringSubmatchIndex(md, -1) {
		events = append(events, jobsBGEvent{pos: m[0], kind: 0, a: md[m[2]:m[3]]})
	}
	for _, m := range jobsBGJobRe.FindAllStringSubmatchIndex(md, -1) {
		events = append(events, jobsBGEvent{pos: m[0], kind: 1, a: md[m[2]:m[3]], b: md[m[4]:m[5]]})
	}
	for _, m := range jobsBGCompanyRe.FindAllStringSubmatchIndex(md, -1) {
		id := ""
		if m[2] >= 0 {
			id = md[m[2]:m[3]]
		}
		events = append(events, jobsBGEvent{pos: m[0], kind: 2, a: id, b: md[m[4]:m[5]]})
	}
	sort.Slice(events, func(i, k int) bool { return events[i].pos < events[k].pos })

	byID := map[string]*model.Job{}
	var order []string
	var skills []string
	lastID := ""
	for _, e := range events {
		switch e.kind {
		case 0:
			skills = append(skills, e.a)
		case 1:
			id, title := e.a, strings.TrimSpace(e.b)
			if existing, ok := byID[id]; ok {
				// Repeated links to the same ad: keep the longest title.
				if len(title) > len(existing.Title) {
					existing.Title = title
				}
			} else {
				byID[id] = &model.Job{
					Source:      "jobsbg",
					Title:       title,
					Location:    "Bulgaria",
					// Link to the English version of the ad page.
					URL:         "https://www.jobs.bg/en/job/" + id,
					Tags:        dedupe(skills),
					Description: strings.Join(dedupe(skills), " "),
				}
				order = append(order, id)
			}
			lastID = id
			skills = nil
		case 2:
			id := e.a
			if id == "" {
				id = lastID
			}
			if job, ok := byID[id]; ok && job.Company == "" {
				job.Company = strings.TrimSpace(e.b)
			}
		}
	}

	jobs := make([]model.Job, 0, len(order))
	for _, id := range order {
		jobs = append(jobs, *byID[id])
	}
	return jobs
}
