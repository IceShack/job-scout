package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
)

// Store persists matched jobs as a JSON file so restarts and redeploys keep
// history and dedupe state.
type Store struct {
	path        string
	crawledPath string

	mu      sync.Mutex
	jobs    map[string]*model.Job
	crawled map[string]time.Time
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path:        filepath.Join(dir, "jobs.json"),
		crawledPath: filepath.Join(dir, "crawled.json"),
		jobs:        map[string]*model.Job{},
		crawled:     map[string]time.Time{},
	}
	if err := loadJSON(s.path, &s.jobs); err != nil {
		return nil, err
	}
	if err := loadJSON(s.crawledPath, &s.crawled); err != nil {
		return nil, err
	}
	return s, nil
}

func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WasCrawled and MarkCrawled implement source.CrawlCache; entries expire
// after 30 days so pages get a fresh look eventually.
func (s *Store) WasCrawled(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.crawled[url]
	return ok && time.Since(t) < 30*24*time.Hour
}

func (s *Store) MarkCrawled(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crawled[url] = time.Now()
}

// Merge upserts the given jobs, returns the ones never seen before, prunes
// entries stale for over 60 days, and saves.
func (s *Store) Merge(found []model.Job, now time.Time) ([]model.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Content keys of hidden jobs: the same ad resurfacing under a sibling
	// URL (per-city slugs, reposts) must stay hidden and not re-notify.
	hiddenKeys := map[string]bool{}
	for _, j := range s.jobs {
		if j.Hidden {
			hiddenKeys[j.ContentKey()] = true
		}
	}

	var fresh []model.Job
	for i := range found {
		j := found[i]
		j.URL = model.NormalizeURL(j.URL)
		j.ID = j.ComputeID()
		if existing, ok := s.jobs[j.ID]; ok {
			j.FirstSeen = existing.FirstSeen
			// Hidden sticks from either side: the user's click on the
			// stored entry, or a language-rejection tombstone arriving for
			// an entry stored before verification existed.
			j.Hidden = existing.Hidden || j.Hidden
			j.Status = existing.Status
			j.StatusAt = existing.StatusAt
		} else {
			j.FirstSeen = now
			if hiddenKeys[j.ContentKey()] {
				j.Hidden = true
			}
			// Jobs arriving pre-hidden (language-rejected tombstones) are
			// stored for suppression but never notified as new.
			if !j.Hidden {
				fresh = append(fresh, j)
			}
		}
		j.LastSeen = now
		s.jobs[j.ID] = &j
	}
	for id, j := range s.jobs {
		// Tracked entries are an application log — never prune them.
		if !j.Tracked() && now.Sub(j.LastSeen) > 60*24*time.Hour {
			delete(s.jobs, id)
		}
	}
	for url, t := range s.crawled {
		if now.Sub(t) > 30*24*time.Hour {
			delete(s.crawled, url)
		}
	}
	return fresh, s.save()
}

func (s *Store) save() error {
	if err := saveJSON(s.path, s.jobs); err != nil {
		return err
	}
	return saveJSON(s.crawledPath, s.crawled)
}

func saveJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Purge deletes stored jobs matching the predicate — e.g. companies added
// to the exclusion list after the jobs were already saved — and returns how
// many were removed.
func (s *Store) Purge(drop func(*model.Job) bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, j := range s.jobs {
		if drop(j) {
			delete(s.jobs, id)
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.save()
}

// SetHidden marks a job as hidden ("not interested") or restores it; it
// reports whether the job exists.
func (s *Store) SetHidden(id string, hidden bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return false, nil
	}
	j.Hidden = hidden
	return true, s.save()
}

// SetStatus moves a job along the application pipeline, or takes it back
// out with model.StatusNone; it reports whether the job exists.
func (s *Store) SetStatus(id string, status model.Status) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return false, nil
	}
	j.Status = status
	if status == model.StatusNone {
		j.StatusAt = time.Time{}
	} else {
		j.StatusAt = time.Now()
	}
	return true, s.save()
}

// List returns all jobs sorted by score desc, then first-seen desc.
func (s *Store) List() []model.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		return out[a].FirstSeen.After(out[b].FirstSeen)
	})
	return out
}
