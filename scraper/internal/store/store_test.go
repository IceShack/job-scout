package store

import (
	"testing"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

func TestHiddenSurvivesMerge(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job := model.Job{Source: "test", Title: "PHP Dev", URL: "https://example.com/j/1"}
	now := time.Now()
	fresh, err := s.Merge([]model.Job{job}, now)
	if err != nil || len(fresh) != 1 {
		t.Fatalf("merge: fresh=%d err=%v", len(fresh), err)
	}
	id := fresh[0].ID

	if found, err := s.SetHidden(id, true); !found || err != nil {
		t.Fatalf("SetHidden: found=%v err=%v", found, err)
	}

	if found, err := s.SetApplied(id, true); !found || err != nil {
		t.Fatalf("SetApplied: found=%v err=%v", found, err)
	}

	// The same job coming in from the next scrape must keep its flags and
	// not count as new.
	fresh, err = s.Merge([]model.Job{job}, now.Add(time.Hour))
	if err != nil || len(fresh) != 0 {
		t.Fatalf("re-merge: fresh=%d err=%v", len(fresh), err)
	}
	if jobs := s.List(); len(jobs) != 1 || !jobs[0].Hidden || !jobs[0].Applied || jobs[0].AppliedAt.IsZero() {
		t.Fatalf("want 1 hidden+applied job, got %+v", jobs)
	}

	// Applied jobs survive the 60-day prune.
	if _, err := s.Merge(nil, now.Add(90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if jobs := s.List(); len(jobs) != 1 {
		t.Fatalf("applied job was pruned: %+v", jobs)
	}

	if found, err := s.SetHidden("nope", true); found || err != nil {
		t.Fatalf("SetHidden unknown id: found=%v err=%v", found, err)
	}
}

func TestHiddenSuppressesSiblingURLs(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	wroclaw := model.Job{Source: "justjoin", Title: "PHP Developer (Symfony)", Company: "Acme",
		URL: "https://justjoin.it/job-offer/acme-php-developer-symfony-wroclaw-php"}
	fresh, err := s.Merge([]model.Job{wroclaw}, now)
	if err != nil || len(fresh) != 1 {
		t.Fatalf("merge: fresh=%d err=%v", len(fresh), err)
	}
	if _, err := s.SetHidden(fresh[0].ID, true); err != nil {
		t.Fatal(err)
	}

	// The same offer under the Gdańsk slug — different URL, same ad — must
	// come back already hidden and must not count as new.
	gdansk := wroclaw
	gdansk.URL = "https://justjoin.it/job-offer/acme-php-developer-symfony-gdansk-php"
	fresh, err = s.Merge([]model.Job{gdansk}, now.Add(time.Hour))
	if err != nil || len(fresh) != 0 {
		t.Fatalf("sibling merge: fresh=%d err=%v", len(fresh), err)
	}
	for _, j := range s.List() {
		if !j.Hidden {
			t.Fatalf("sibling stored unhidden: %+v", j)
		}
	}
}

func TestMergePreHiddenNotFresh(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := model.Job{Source: "justjoin", Title: "Programista PHP", Company: "Acme",
		URL: "https://justjoin.it/job-offer/acme-programista-php", Hidden: true}
	fresh, err := s.Merge([]model.Job{j}, time.Now())
	if err != nil || len(fresh) != 0 {
		t.Fatalf("pre-hidden job counted as fresh: fresh=%d err=%v", len(fresh), err)
	}
	if jobs := s.List(); len(jobs) != 1 || !jobs[0].Hidden {
		t.Fatalf("tombstone not stored hidden: %+v", jobs)
	}
}

func TestMergeHiddenSticksOnExistingEntry(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	j := model.Job{Source: "justjoin", Title: "Backend Developer", Company: "Acme",
		URL: "https://justjoin.it/job-offer/acme-backend"}
	if _, err := s.Merge([]model.Job{j}, now); err != nil {
		t.Fatal(err)
	}
	// The same entry later arrives pre-hidden (language verification
	// caught it) — the stored entry must become hidden.
	j.Hidden = true
	fresh, err := s.Merge([]model.Job{j}, now.Add(time.Hour))
	if err != nil || len(fresh) != 0 {
		t.Fatalf("fresh=%d err=%v", len(fresh), err)
	}
	if jobs := s.List(); len(jobs) != 1 || !jobs[0].Hidden {
		t.Fatalf("hidden flag lost on existing entry: %+v", jobs)
	}
}

func TestMergeNormalizesTrackingParams(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	j1 := model.Job{Source: "serper", Title: "PHP Dev", Company: "X",
		URL: "https://example.com/job/1?gh_jid=7&srsltid=AbC123"}
	fresh, err := s.Merge([]model.Job{j1}, now)
	if err != nil || len(fresh) != 1 {
		t.Fatalf("merge: fresh=%d err=%v", len(fresh), err)
	}
	if fresh[0].URL != "https://example.com/job/1?gh_jid=7" {
		t.Fatalf("url not normalized: %q", fresh[0].URL)
	}
	// Same page, different tracking id → same entry, not new.
	j2 := j1
	j2.URL = "https://example.com/job/1?gh_jid=7&srsltid=XyZ999"
	fresh, err = s.Merge([]model.Job{j2}, now.Add(time.Hour))
	if err != nil || len(fresh) != 0 {
		t.Fatalf("re-merge: fresh=%d err=%v", len(fresh), err)
	}
	if got := len(s.List()); got != 1 {
		t.Fatalf("want 1 stored job, got %d", got)
	}
}
