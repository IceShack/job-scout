package source

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// ArcDev scrapes arc.dev remote job listings from the __NEXT_DATA__ JSON
// embedded in the category pages. Each job carries requiredCountries, so
// non-EU-eligible ads are dropped here with certainty instead of relying
// on the matcher's text heuristics.
type ArcDev struct {
	Pages []string
}

func (ArcDev) Name() string { return "arcdev" }

func (ArcDev) Domains() []string { return []string{"arc.dev"} }

var arcNextDataRe = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

// arc.dev states eligibility as country codes. EU/EEA plus Switzerland is
// treated as one hiring area; the configured location markers then decide
// whether such an ad actually fits.
var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "HR": true, "CY": true, "CZ": true,
	"DK": true, "EE": true, "FI": true, "FR": true, "DE": true, "GR": true,
	"HU": true, "IE": true, "IT": true, "LV": true, "LT": true, "LU": true,
	"MT": true, "NL": true, "PL": true, "PT": true, "RO": true, "SK": true,
	"SI": true, "ES": true, "SE": true, "NO": true, "IS": true, "LI": true,
	"CH": true,
}

type arcDevJob struct {
	RandomKey         string   `json:"randomKey"`
	Title             string   `json:"title"`
	JobType           string   `json:"jobType"`
	RequiredCountries []string `json:"requiredCountries"`
	ExperienceLevels  []string `json:"experienceLevels"`
	URLString         string   `json:"urlString"`
	PostedAt          int64    `json:"postedAt"`
	Company           struct {
		Name string `json:"name"`
	} `json:"company"`
	Categories []struct {
		Name string `json:"name"`
	} `json:"categories"`
}

type arcNextData struct {
	Props struct {
		PageProps struct {
			ExternalJobs []arcDevJob `json:"externalJobs"`
		} `json:"pageProps"`
	} `json:"props"`
}

func (a ArcDev) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, page := range a.Pages {
		body, err := get(ctx, page)
		if err != nil {
			lastErr = err
			continue
		}
		m := arcNextDataRe.FindSubmatch(body)
		if m == nil {
			lastErr = fmt.Errorf("arcdev: no __NEXT_DATA__ on %s", page)
			continue
		}
		var data arcNextData
		if err := json.Unmarshal(m[1], &data); err != nil {
			lastErr = err
			continue
		}
		for _, it := range data.Props.PageProps.ExternalJobs {
			j, ok := arcDevToJob(it)
			if ok && !seen[j.URL] {
				seen[j.URL] = true
				jobs = append(jobs, j)
			}
		}
	}
	if len(jobs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return jobs, nil
}

func arcDevToJob(it arcDevJob) (model.Job, bool) {
	location, ok := arcDevLocation(it.RequiredCountries)
	if !ok || it.Title == "" || it.URLString == "" {
		return model.Job{}, false
	}
	var tags []string
	for _, c := range it.Categories {
		if len(tags) < 15 {
			tags = append(tags, c.Name)
		}
	}
	tags = append(tags, it.ExperienceLevels...)
	if it.JobType != "" {
		tags = append(tags, it.JobType)
	}
	j := model.Job{
		Source:      "arcdev",
		Title:       it.Title,
		Company:     it.Company.Name,
		Location:    location,
		URL:         "https://arc.dev/remote-jobs/j/" + it.URLString + "-" + it.RandomKey,
		Tags:        tags,
		Description: strings.Join(tags, " "),
	}
	if it.PostedAt > 0 {
		j.PostedAt = time.Unix(it.PostedAt, 0)
	}
	return j, true
}

// arcDevLocation maps requiredCountries to a location string, rejecting
// jobs that cannot be worked from Bulgaria/the EU.
func arcDevLocation(countries []string) (string, bool) {
	if len(countries) == 0 {
		return "Remote, worldwide", true
	}
	eu := false
	for _, c := range countries {
		switch {
		case strings.EqualFold(c, "BG"):
			return "Bulgaria (remote)", true
		case euCountries[strings.ToUpper(c)]:
			eu = true
		}
	}
	if eu {
		return "Remote, Europe", true
	}
	return "", false
}
