package source

import (
	"strings"
	"testing"
	"time"
)

// Fixture trimmed from a real djinni.co listing page: two cards, one fully
// remote with the ad collapsed behind "More", one office-based with only
// the truncated text.
const djinniFixture = `
<main id="jobs_main"><ul class="list-unstyled list-jobs mb-4">
<div id="job-item-842796" class="job-item card-link fs-5 mb-4 rounded-2 p-2">
  <div class="d-flex flex-column gap-1">
    <a href="/jobs/842796-senior-software-engineer-go-php-react/" class="job_item__header-link d-flex flex-column gap-1">
      <header class="row gx-2 align-items-start">
        <div class="col">
          <h2 class="job-item__position fs-4 m-0 mb-1">Senior Software Engineer (Go / PHP / React)</h2>
          <div class="d-flex flex-wrap align-items-center column-gap-1">
            <span class="small text-gray-800 opacity-75 font-weight-500">GlobalLogic</span>
          </div>
        </div>
        <div class="col-auto"><div class="fs-5">
          <span class="text-body-tertiary fw-medium" title="Salary level relative to other jobs">$$$</span>
        </div></div>
      </header>
    </a>
    <div class="fw-medium d-flex flex-wrap align-items-center column-gap-1">
      <span class="text-nowrap">Full Remote</span>
      <span class="middot"> · </span>
      <span><span class="location-text">Countries of Europe or Ukraine</span></span>
      <span class="middot"> · </span>
      <span class="text-nowrap">5 years of experience</span>
    </div>
    <div class="job-item__tags">
      <span class="badge text-bg-light">Outsource</span>
      <span class="badge text-bg-light">Go</span>
    </div>
    <div id="job-description-842796">
      <span class="js-truncated-text">We are looking for a Senior Software Engineer to join...</span>
      <span class="js-original-text description-expandable d-none"><p>We are looking for a Senior Software Engineer.</p><ul><li><strong>Go</strong> and PHP services</li><li>React on the front end</li></ul></span>
      <a href="#job-item-842796" role="button"><span class="text-nowrap">More</span></a>
    </div>
    <div class="d-flex align-items-center gap-1 fs-5">
      <span class="text-nowrap">1163 views</span>
      <span class="middot"> · </span>
      <span class="text-nowrap" title="14:19 13.08.2026" data-bs-toggle="tooltip">15m</span>
    </div>
  </div>
</div>
<div id="job-item-700001" class="job-item card-link fs-5 mb-4 rounded-2 p-2">
  <div class="d-flex flex-column gap-1">
    <a href="/jobs/700001-php-developer/" class="job_item__header-link d-flex flex-column gap-1">
      <header class="row gx-2 align-items-start">
        <div class="col">
          <h2 class="job-item__position fs-4 m-0 mb-1">PHP Developer</h2>
          <div class="d-flex flex-wrap align-items-center column-gap-1">
            <span class="small text-gray-800 opacity-75 font-weight-500">Acme</span>
          </div>
        </div>
      </header>
    </a>
    <div class="fw-medium d-flex flex-wrap align-items-center column-gap-1">
      <span class="text-nowrap">Office Work</span>
      <span class="middot"> · </span>
      <span><span class="location-text">Kyiv</span></span>
    </div>
    <div id="job-description-700001">
      <span class="js-truncated-text">Laravel shop looking for a backend developer...</span>
    </div>
  </div>
</div>
</ul></main>`

func TestParseDjinni(t *testing.T) {
	jobs := parseDjinni([]byte(djinniFixture))
	if len(jobs) != 2 {
		t.Fatalf("parsed %d jobs, want 2: %+v", len(jobs), jobs)
	}

	remote := jobs[0]
	if remote.Title != "Senior Software Engineer (Go / PHP / React)" {
		t.Errorf("title = %q", remote.Title)
	}
	if remote.Company != "GlobalLogic" {
		t.Errorf("company = %q", remote.Company)
	}
	// Relative hrefs must become links that work outside the board.
	if remote.URL != "https://djinni.co/jobs/842796-senior-software-engineer-go-php-react/" {
		t.Errorf("url = %q", remote.URL)
	}
	// Workplace and eligible region are two labels on the page; the matcher
	// reads geography out of one string.
	if remote.Location != "Countries of Europe or Ukraine (remote)" {
		t.Errorf("location = %q", remote.Location)
	}
	// The expanded ad wins over the truncated preview: it is what the
	// keyword scoring and the language filter see.
	if !strings.Contains(remote.Description, "React on the front end") {
		t.Errorf("description is the truncated copy: %q", remote.Description)
	}
	if strings.Contains(remote.Description, "<li>") {
		t.Errorf("description kept its markup: %q", remote.Description)
	}
	if len(remote.Tags) != 2 || remote.Tags[0] != "Outsource" {
		t.Errorf("tags = %v", remote.Tags)
	}
	want := time.Date(2026, 8, 13, 14, 19, 0, 0, djinniZone)
	if !remote.PostedAt.Equal(want) {
		t.Errorf("posted_at = %s, want %s", remote.PostedAt, want)
	}

	office := jobs[1]
	if office.Location != "Kyiv" {
		t.Errorf("office location = %q, want no remote suffix", office.Location)
	}
	if !strings.Contains(office.Description, "Laravel shop") {
		t.Errorf("truncated text not used as a fallback: %q", office.Description)
	}
	// Cards without a timestamp must not invent one.
	if !office.PostedAt.IsZero() {
		t.Errorf("posted_at = %s, want zero", office.PostedAt)
	}
}

func TestParseDjinniIgnoresJunk(t *testing.T) {
	if jobs := parseDjinni([]byte(`<html><body><p>no jobs here</p></body></html>`)); len(jobs) != 0 {
		t.Errorf("parsed %d jobs from a page without cards", len(jobs))
	}
}
