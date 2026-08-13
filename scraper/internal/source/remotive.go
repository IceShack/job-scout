package source

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// Remotive reads the public API, one request per configured category.
type Remotive struct {
	Categories []string
}

func (Remotive) Name() string { return "remotive" }

func (Remotive) Domains() []string { return []string{"remotive.com"} }

type remotiveResponse struct {
	Jobs []struct {
		URL              string   `json:"url"`
		Title            string   `json:"title"`
		CompanyName      string   `json:"company_name"`
		JobType          string   `json:"job_type"`
		RequiredLocation string   `json:"candidate_required_location"`
		PublicationDate  string   `json:"publication_date"`
		Description      string   `json:"description"`
		Tags             []string `json:"tags"`
	} `json:"jobs"`
}

func (r Remotive) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, category := range r.Categories {
		body, err := get(ctx, "https://remotive.com/api/remote-jobs?category="+url.QueryEscape(category))
		if err != nil {
			lastErr = err
			continue
		}
		var resp remotiveResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			lastErr = err
			continue
		}
		for _, it := range resp.Jobs {
			if seen[it.URL] {
				continue
			}
			seen[it.URL] = true
			tags := it.Tags
			if it.JobType != "" {
				tags = append(tags, it.JobType)
			}
			j := model.Job{
				Source:      "remotive",
				Title:       it.Title,
				Company:     it.CompanyName,
				Location:    it.RequiredLocation,
				URL:         it.URL,
				Tags:        tags,
				Description: truncate(stripHTML(it.Description), 2000),
			}
			if t, err := time.Parse("2006-01-02T15:04:05", it.PublicationDate); err == nil {
				j.PostedAt = t
			}
			jobs = append(jobs, j)
		}
	}
	if len(jobs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return jobs, nil
}
