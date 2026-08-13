package source

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/IceShack/job-scout/scraper/internal/model"
)

// Djinni scrapes djinni.co (Ukrainian board with a lot of remote work for
// EU-based contractors). Configure Pages with its filtered listing URLs,
// e.g. https://djinni.co/jobs/?primary_keyword=PHP&employment=remote.
// Listing cards carry the whole ad, so nothing needs fetching per job;
// each page holds 15 offers, so add "&page=2" for more.
type Djinni struct {
	Pages []string
}

func (Djinni) Name() string { return "djinni" }

func (Djinni) Domains() []string { return []string{"djinni.co"} }

func (d Djinni) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, page := range d.Pages {
		body, err := get(ctx, page)
		if err != nil {
			lastErr = err
			continue
		}
		for _, job := range parseDjinni(body) {
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

// Cards show a relative age ("15m", "3d") with the exact timestamp in the
// tooltip, in Kyiv local time.
var djinniPostedRe = regexp.MustCompile(`^\d{2}:\d{2} \d{2}\.\d{2}\.\d{4}$`)

// djinniZone is where the board's timestamps are written; falling back to
// UTC only shifts PostedAt by a couple of hours.
var djinniZone = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Kyiv"); err == nil {
		return loc
	}
	return time.UTC
}()

func parseDjinni(body []byte) []model.Job {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var jobs []model.Job
	doc.Find("div.job-item").Each(func(_ int, s *goquery.Selection) {
		href := s.Find("a.job_item__header-link").First().AttrOr("href", "")
		title := strings.TrimSpace(s.Find("h2.job-item__position").First().Text())
		if href == "" || title == "" {
			return
		}
		if strings.HasPrefix(href, "/") {
			href = "https://djinni.co" + href
		}

		// The board states the workplace and the eligible region as two
		// separate labels; the matcher wants them in one location string.
		location := strings.TrimSpace(s.Find(".location-text").First().Text())
		if strings.Contains(s.Text(), "Full Remote") {
			if location == "" {
				location = "Remote"
			} else {
				location += " (remote)"
			}
		}

		var tags []string
		s.Find(".job-item__tags .badge").Each(func(_ int, t *goquery.Selection) {
			if txt := strings.TrimSpace(t.Text()); txt != "" && len(tags) < 15 {
				tags = append(tags, txt)
			}
		})

		// The full ad sits in the card, collapsed; the truncated copy is
		// what is shown until you click "More".
		description := s.Find(".js-original-text").First().Text()
		if strings.TrimSpace(description) == "" {
			description = s.Find(".js-truncated-text").First().Text()
		}

		job := model.Job{
			Source:      "djinni",
			Title:       title,
			Company:     strings.TrimSpace(s.Find("header span.small").First().Text()),
			Location:    location,
			URL:         href,
			Tags:        dedupe(tags),
			Description: truncate(stripHTML(description), 3000),
		}
		s.Find("span[title]").EachWithBreak(func(_ int, t *goquery.Selection) bool {
			stamp := t.AttrOr("title", "")
			if !djinniPostedRe.MatchString(stamp) {
				return true
			}
			if posted, err := time.ParseInLocation("15:04 02.01.2006", stamp, djinniZone); err == nil {
				job.PostedAt = posted
			}
			return false
		})
		jobs = append(jobs, job)
	})
	return jobs
}
