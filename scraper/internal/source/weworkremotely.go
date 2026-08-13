package source

import (
	"context"
	"encoding/xml"
	"strings"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// WeWorkRemotely reads the category RSS feeds.
type WeWorkRemotely struct {
	Feeds []string
}

func (WeWorkRemotely) Name() string { return "weworkremotely" }

func (WeWorkRemotely) Domains() []string { return []string{"weworkremotely.com"} }

type wwrRSS struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Region      string `xml:"region"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
	} `xml:"channel>item"`
}

func (w WeWorkRemotely) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, feed := range w.Feeds {
		body, err := get(ctx, feed)
		if err != nil {
			lastErr = err
			continue
		}
		var rss wwrRSS
		if err := xml.Unmarshal(body, &rss); err != nil {
			lastErr = err
			continue
		}
		for _, it := range rss.Items {
			if seen[it.Link] {
				continue
			}
			seen[it.Link] = true
			// Titles are "Company: Role".
			company, title, found := strings.Cut(it.Title, ":")
			if !found {
				company, title = "", it.Title
			}
			j := model.Job{
				Source:      "weworkremotely",
				Title:       strings.TrimSpace(title),
				Company:     strings.TrimSpace(company),
				Location:    stripHTML(it.Region),
				URL:         it.Link,
				Description: truncate(stripHTML(it.Description), 2000),
			}
			if t, err := time.Parse(time.RFC1123Z, it.PubDate); err == nil {
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
