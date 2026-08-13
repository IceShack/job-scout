package source

import "testing"

// Fixture mirrors the real Firecrawl markdown for a jobs.bg search page:
// a card is one big link whose images are the skill icons, followed by the
// company card linking back to the job id.
const jobsBGFixture = `
[![PHP](https://static.jobs.bg/mobile/images/skills/php.png?v=1.0.0)\
\
![MySQL](https://static.jobs.bg/mobile/images/skills/mysql.png?v=1.0.0)\
\
![SQL](https://static.jobs.bg/mobile/images/skills/sql.png?v=1.0.0)](https://www.jobs.bg/job/8567072 "Technical Support Specialist")

[![СУПЕРХОСТИНГ.БГ ЕООД](https://assets.jobs.bg/assets/logo/s_d673.png)\
\
СУПЕРХОСТИНГ.БГ ЕООД](https://www.jobs.bg/company/58506?job=8567072 "СУПЕРХОСТИНГ.БГ ЕООД")

[_star_ _star_Senior Full Stack Engineer (PHP)](https://www.jobs.bg/job/8567072 "Senior Full Stack Engineer (PHP)")

[![Laravel](https://static.tech.bg/mobile/images/skills/laravel.png?v=1.0.0)](https://www.tech.bg/job/9999999 "PHP Developer")

[Acme Ltd](https://www.tech.bg/company/acme-ltd "Acme Ltd")
`

func TestParseJobsBG(t *testing.T) {
	jobs := parseJobsBG(jobsBGFixture)
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d: %+v", len(jobs), jobs)
	}
	first := jobs[0]
	// The longer title from the repeated link must win.
	if first.Title != "Senior Full Stack Engineer (PHP)" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Company != "СУПЕРХОСТИНГ.БГ ЕООД" {
		t.Errorf("company = %q", first.Company)
	}
	if first.URL != "https://www.jobs.bg/en/job/8567072" {
		t.Errorf("url = %q", first.URL)
	}
	if len(first.Tags) != 3 || first.Tags[0] != "PHP" {
		t.Errorf("tags = %v", first.Tags)
	}
	// tech.bg cards (same platform, same ids) parse identically and still
	// link to the jobs.bg English page.
	second := jobs[1]
	if second.Company != "Acme Ltd" || len(second.Tags) != 1 || second.Tags[0] != "Laravel" {
		t.Errorf("second job = %+v", second)
	}
	if second.URL != "https://www.jobs.bg/en/job/9999999" {
		t.Errorf("tech.bg ad must link to jobs.bg/en, got %q", second.URL)
	}
}
