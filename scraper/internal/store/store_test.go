package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

func TestStatusSurvivesMerge(t *testing.T) {
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

	if found, err := s.SetStatus(id, model.StatusApplied); !found || err != nil {
		t.Fatalf("SetStatus: found=%v err=%v", found, err)
	}

	// The same job coming in from the next scrape must keep its flags and
	// not count as new.
	fresh, err = s.Merge([]model.Job{job}, now.Add(time.Hour))
	if err != nil || len(fresh) != 0 {
		t.Fatalf("re-merge: fresh=%d err=%v", len(fresh), err)
	}
	jobs := s.List()
	if len(jobs) != 1 || !jobs[0].Hidden || jobs[0].Status != model.StatusApplied || jobs[0].StatusAt.IsZero() {
		t.Fatalf("want 1 hidden+applied job, got %+v", jobs)
	}

	// A reply moves it along, and the timestamp moves with it.
	if _, err := s.SetStatus(id, model.StatusInterviewing); err != nil {
		t.Fatal(err)
	}
	if got := s.List()[0].Status; got != model.StatusInterviewing {
		t.Fatalf("status = %q, want interviewing", got)
	}

	// Tracked jobs survive the 60-day prune.
	if _, err := s.Merge(nil, now.Add(90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if jobs := s.List(); len(jobs) != 1 {
		t.Fatalf("tracked job was pruned: %+v", jobs)
	}

	// Clearing the status takes it back out of the pipeline.
	if _, err := s.SetStatus(id, model.StatusNone); err != nil {
		t.Fatal(err)
	}
	if j := s.List()[0]; j.Tracked() || !j.StatusAt.IsZero() {
		t.Fatalf("status not cleared: %+v", j)
	}

	if found, err := s.SetHidden("nope", true); found || err != nil {
		t.Fatalf("SetHidden unknown id: found=%v err=%v", found, err)
	}
	if found, err := s.SetStatus("nope", model.StatusApplied); found || err != nil {
		t.Fatalf("SetStatus unknown id: found=%v err=%v", found, err)
	}
}

// Stores written before statuses existed carry an "applied" bool.
func TestLoadMigratesLegacyApplied(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"abc":{"id":"abc","source":"test","title":"PHP Dev",
	  "url":"https://example.com/j/1","score":5,"first_seen":"2026-01-01T00:00:00Z",
	  "last_seen":"2026-01-02T00:00:00Z","applied":true,"applied_at":"2026-01-03T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	j := s.List()[0]
	if j.Status != model.StatusApplied {
		t.Fatalf("status = %q, want applied", j.Status)
	}
	if got := j.StatusAt.Format(time.RFC3339); got != "2026-01-03T00:00:00Z" {
		t.Fatalf("status_at = %s, want the old applied_at", got)
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
