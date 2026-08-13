package source

import "testing"

func TestArcDevLocation(t *testing.T) {
	cases := []struct {
		countries []string
		want      string
		ok        bool
	}{
		{nil, "Remote, worldwide", true},
		{[]string{"US"}, "", false},
		{[]string{"US", "CA"}, "", false},
		{[]string{"DE", "AT"}, "Remote, Europe", true},
		{[]string{"US", "DE"}, "Remote, Europe", true},
		{[]string{"BG"}, "Bulgaria (remote)", true},
		{[]string{"GB"}, "", false},
	}
	for _, tc := range cases {
		got, ok := arcDevLocation(tc.countries)
		if got != tc.want || ok != tc.ok {
			t.Errorf("arcDevLocation(%v) = %q,%v want %q,%v", tc.countries, got, ok, tc.want, tc.ok)
		}
	}
}

func TestArcDevToJob(t *testing.T) {
	it := arcDevJob{
		RandomKey:         "pb8sv8qhr7",
		Title:             "(Senior) Fullstack Engineer",
		JobType:           "permanent",
		RequiredCountries: []string{"DE"},
		ExperienceLevels:  []string{"senior"},
		URLString:         "chargecloud-senior-fullstack-engineer-all-genders",
		PostedAt:          1786327771,
	}
	it.Company.Name = "chargecloud"
	it.Categories = []struct {
		Name string `json:"name"`
	}{{Name: "PHP"}, {Name: "Symfony"}}

	j, ok := arcDevToJob(it)
	if !ok {
		t.Fatal("expected job to be kept")
	}
	if j.URL != "https://arc.dev/remote-jobs/j/chargecloud-senior-fullstack-engineer-all-genders-pb8sv8qhr7" {
		t.Errorf("url = %q", j.URL)
	}
	if j.Location != "Remote, Europe" || j.Company != "chargecloud" {
		t.Errorf("location=%q company=%q", j.Location, j.Company)
	}
	if len(j.Tags) != 4 {
		t.Errorf("tags = %v", j.Tags)
	}

	it.RequiredCountries = []string{"US"}
	if _, ok := arcDevToJob(it); ok {
		t.Error("US-only job must be dropped")
	}
}
