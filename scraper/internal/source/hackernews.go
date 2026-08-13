package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// HackerNews reads the latest "Ask HN: Who is hiring?" thread via the
// Algolia API. Require narrows the thread to posts containing that text
// (typically "remote"); empty keeps every post.
type HackerNews struct {
	Require string
}

func (HackerNews) Name() string { return "hackernews" }

func (HackerNews) Domains() []string { return []string{"news.ycombinator.com"} }

type hnSearch struct {
	Hits []struct {
		ObjectID string `json:"objectID"`
		Title    string `json:"title"`
	} `json:"hits"`
}

type hnItem struct {
	ID        int    `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	Children  []hnItem
}

func (h HackerNews) Fetch(ctx context.Context) ([]model.Job, error) {
	body, err := get(ctx, "https://hn.algolia.com/api/v1/search_by_date?query=%22who%20is%20hiring%22&tags=story,author_whoishiring&hitsPerPage=5")
	if err != nil {
		return nil, err
	}
	var search hnSearch
	if err := json.Unmarshal(body, &search); err != nil {
		return nil, err
	}
	var storyID string
	for _, hit := range search.Hits {
		if strings.Contains(strings.ToLower(hit.Title), "who is hiring") {
			storyID = hit.ObjectID
			break
		}
	}
	if storyID == "" {
		return nil, fmt.Errorf("no 'who is hiring' thread found")
	}

	body, err = get(ctx, "https://hn.algolia.com/api/v1/items/"+storyID)
	if err != nil {
		return nil, err
	}
	var story hnItem
	if err := json.Unmarshal(body, &story); err != nil {
		return nil, err
	}

	var jobs []model.Job
	for _, c := range story.Children {
		text := stripHTML(c.Text)
		if text == "" || !strings.Contains(strings.ToLower(text), strings.ToLower(h.Require)) {
			continue
		}
		// Convention: first line is "Company | Role | Location | ...".
		header := text
		if i := strings.IndexAny(text, ".\n"); i > 0 && i < 200 {
			header = text[:i]
		}
		company := header
		if i := strings.Index(header, "|"); i > 0 {
			company = strings.TrimSpace(header[:i])
		}
		j := model.Job{
			Source:      "hackernews",
			Title:       truncate(header, 140),
			Company:     truncate(company, 60),
			Location:    "see post",
			URL:         fmt.Sprintf("https://news.ycombinator.com/item?id=%d", c.ID),
			Description: truncate(text, 2000),
		}
		if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
			j.PostedAt = t
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
