package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/IceShack/job-scout/scraper/internal/model"
)

var junkTag = regexp.MustCompile(`(?i)^(new|remote|locations?|super offer|undisclosed.*|salary)$|\d+d left|^[\d\s,.-]+$|(EUR|PLN|USD|GBP)/`)

var (
	jjLDJSONRe = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)
)

// FetchDescription loads an offer page and returns the ad text from its
// JSON-LD JobPosting. Listing cards carry only tags, so this is the only
// way to see which language an offer is written in.
func (JustJoin) FetchDescription(ctx context.Context, url string) (string, error) {
	body, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	for _, m := range jjLDJSONRe.FindAllSubmatch(body, -1) {
		var posting struct {
			Type        string `json:"@type"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(m[1], &posting); err != nil {
			continue
		}
		if posting.Type == "JobPosting" && posting.Description != "" {
			return truncate(stripHTML(posting.Description), 3000), nil
		}
	}
	return "", fmt.Errorf("justjoin: no JobPosting JSON-LD on %s", url)
}

// JustJoin scrapes justjoin.it (Polish/EU tech board, lots of remote B2B
// contracts). Configure Pages with its listing URLs; adding
// "?workplace=remote" pre-filters them to remote-only offers.
type JustJoin struct {
	Pages []string
	// Limit caps the offer pages fetched per run for the language check.
	Limit int
}

func (JustJoin) Name() string { return "justjoin" }

func (JustJoin) Domains() []string { return []string{"justjoin.it"} }

func (j JustJoin) VerifyLimit() int { return j.Limit }

func (j JustJoin) Fetch(ctx context.Context) ([]model.Job, error) {
	var jobs []model.Job
	seen := map[string]bool{}
	var lastErr error
	for _, page := range j.Pages {
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

		doc.Find("a.offer_list_offer_title_link").Each(func(_ int, s *goquery.Selection) {
			url := s.AttrOr("href", "")
			title := strings.TrimSpace(s.Text())
			if url == "" || title == "" || seen[url] {
				return
			}
			seen[url] = true

			// The card container is a few levels above the title link; the
			// company name is its first <p> (the "Remote" badge is also a
			// <p>, but comes later).
			card := s.Parent()
			for i := 0; i < 4 && card.Length() > 0 && card.Find("p").Length() == 0; i++ {
				card = card.Parent()
			}
			company := strings.TrimSpace(card.Find("p").First().Text())
			if company == "Remote" {
				company = ""
			}
			remote := strings.Contains(card.Text(), "Remote")

			// Card texts double as tags (skills, seniority, B2B) for the
			// matcher to score; drop salary/expiry/location noise.
			var tags []string
			card.Find("div, span").Each(func(_ int, t *goquery.Selection) {
				txt := strings.TrimSpace(t.Text())
				if txt != "" && len(txt) <= 20 && t.Children().Length() == 0 &&
					len(tags) < 15 && !junkTag.MatchString(txt) {
					tags = append(tags, txt)
				}
			})
			location := "Poland"
			if remote {
				location = "Remote, Europe"
			}
			jobs = append(jobs, model.Job{
				Source:      "justjoin",
				Title:       title,
				Company:     company,
				Location:    location,
				URL:         url,
				Tags:        dedupe(tags),
				Description: strings.Join(dedupe(tags), " "),
			})
		})
	}
	if len(jobs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return jobs, nil
}
