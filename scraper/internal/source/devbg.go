package source

import (
	"bytes"
	"context"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/IceShack/job-scout/scraper/internal/model"
)

// DevBG scrapes dev.bg listing pages — the main Bulgarian IT job board.
// Configure Pages with its category URLs, e.g.
// https://dev.bg/company/jobs/php/.
type DevBG struct {
	Pages []string
}

func (DevBG) Name() string { return "devbg" }

// h512.com is dev.bg's English mirror, so both hosts are ours.
func (DevBG) Domains() []string { return []string{"dev.bg", "h512.com"} }

func (d DevBG) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, page := range d.Pages {
		body, err := get(ctx, page)
		if err != nil {
			lastErr = err
			continue
		}
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		doc.Find("div.job-list-item").Each(func(_ int, s *goquery.Selection) {
			url := s.Find("a.overlay-link").First().AttrOr("href", "")
			title := strings.TrimSpace(s.Find(".job-title").First().Text())
			if url == "" || title == "" || seen[url] {
				return
			}
			// h512.com is dev.bg's English mirror (hreflang=en), same paths.
			url = strings.Replace(url, "https://dev.bg/", "https://h512.com/", 1)
			seen[url] = true
			location := "Bulgaria"
			badges := s.Find(".badge").AttrOr("class", "")
			switch {
			case strings.Contains(badges, "remote"):
				location = "Bulgaria (remote)"
			case strings.Contains(badges, "hybrid"):
				location = "Bulgaria (hybrid)"
			}
			jobs = append(jobs, model.Job{
				Source:   "devbg",
				Title:    title,
				Company:  strings.TrimSpace(s.Find(".company-name").First().Text()),
				Location: location,
				URL:      url,
				// Listing pages carry no description; the title and the
				// category slug imply the stack.
				Description: lastPathSegment(page),
			})
		})
	}
	if len(jobs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return jobs, nil
}
