package source

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// RemoteOK reads the public JSON API. Per its terms, results must link back
// to remoteok.com (they do — we keep the original URLs).
type RemoteOK struct{}

func (RemoteOK) Name() string { return "remoteok" }

func (RemoteOK) Domains() []string { return []string{"remoteok.com"} }

type remoteOKItem struct {
	Slug        string   `json:"slug"`
	Position    string   `json:"position"`
	Company     string   `json:"company"`
	Tags        []string `json:"tags"`
	Location    string   `json:"location"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Date        string   `json:"date"`
}

func (RemoteOK) Fetch(ctx context.Context) ([]model.Job, error) {
	body, err := get(ctx, "https://remoteok.com/api")
	if err != nil {
		return nil, err
	}
	var items []remoteOKItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	var jobs []model.Job
	for _, it := range items {
		if it.Position == "" || it.URL == "" {
			continue // first element is a legal notice
		}
		j := model.Job{
			Source:      "remoteok",
			Title:       it.Position,
			Company:     it.Company,
			Location:    it.Location,
			URL:         it.URL,
			Tags:        it.Tags,
			Description: truncate(stripHTML(it.Description), 2000),
		}
		if t, err := time.Parse(time.RFC3339, it.Date); err == nil {
			j.PostedAt = t
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
