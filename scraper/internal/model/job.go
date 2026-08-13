package model

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	neturl "net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Status is how far an application has got. The zero value means the job
// is only a match so far — nothing has been sent.
type Status string

const (
	StatusNone Status = ""
	// StatusApplied is the initial state once you apply: sent, no reply yet.
	StatusApplied Status = "applied"
	// StatusInterviewing is a positive reply — the process is running.
	StatusInterviewing Status = "interviewing"
	// StatusDeclined is a negative outcome, from either side.
	StatusDeclined Status = "declined"
)

// Statuses lists the statuses in the order an application moves through
// them. The UI builds its menus from this, so adding one here is enough.
var Statuses = []Status{StatusApplied, StatusInterviewing, StatusDeclined}

// Valid reports whether s is a known status (StatusNone included: it is
// how you take a job back out of the pipeline).
func (s Status) Valid() bool {
	return s == StatusNone || slices.Contains(Statuses, s)
}

// Job is a single job ad, normalised across sources.
type Job struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Title       string    `json:"title"`
	Company     string    `json:"company"`
	Location    string    `json:"location"`
	URL         string    `json:"url"`
	Tags        []string  `json:"tags,omitempty"`
	Description string    `json:"description,omitempty"`
	PostedAt    time.Time `json:"posted_at,omitzero"`

	// Set by the matcher.
	Score   int      `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
	// Fit explains how the job's geography matches: the configured
	// location.local_label or remote_label, followed by the marker that
	// decided it — e.g. "germany (berlin)", "eu-remote (unverified)".
	Fit string `json:"fit"`

	// Set by the store.
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	// Hidden is set when the user marks the job "not interested"; the entry
	// stays in the store so it never reappears as new.
	Hidden bool `json:"hidden,omitempty"`
	// Status tracks the application; StatusAt is when it last changed.
	Status   Status    `json:"status,omitempty"`
	StatusAt time.Time `json:"status_at,omitzero"`
}

// Tracked reports whether the job is in the application pipeline. Tracked
// jobs are an application log: they survive pruning and profile changes.
func (j *Job) Tracked() bool { return j.Status != StatusNone }

// UnmarshalJSON reads stores written before statuses existed, where the
// only state was an "applied" bool.
func (j *Job) UnmarshalJSON(data []byte) error {
	type job Job // alias, so decoding doesn't recurse into this method
	legacy := struct {
		*job
		Applied   bool      `json:"applied"`
		AppliedAt time.Time `json:"applied_at"`
	}{job: (*job)(j)}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if j.Status == StatusNone && legacy.Applied {
		j.Status, j.StatusAt = StatusApplied, legacy.AppliedAt
	}
	return nil
}

// ComputeID derives a stable ID from source and URL.
func (j *Job) ComputeID() string {
	h := sha1.Sum([]byte(j.Source + "|" + j.URL))
	return hex.EncodeToString(h[:8])
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// ContentKey identifies the same ad independent of its URL — boards like
// justjoin.it publish one URL per city for a single offer, so a hidden job
// must stay hidden when it resurfaces under a sibling URL.
func (j *Job) ContentKey() string {
	norm := func(s string) string {
		return strings.TrimSpace(nonAlnum.ReplaceAllString(strings.ToLower(s), " "))
	}
	h := sha1.Sum([]byte(norm(j.Company) + "|" + norm(j.Title)))
	return hex.EncodeToString(h[:8])
}

// Tracking query parameters that vary between fetches of the same page;
// they would give the same ad a fresh ID every scrape.
var trackingParam = regexp.MustCompile(`^(utm_|srsltid$|gclid$|fbclid$|mc_|_hs|ref$)`)

// NormalizeURL strips tracking parameters and fragments.
func NormalizeURL(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for key := range q {
		if trackingParam.MatchString(strings.ToLower(key)) {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String()
}
