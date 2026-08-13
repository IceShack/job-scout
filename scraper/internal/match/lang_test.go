package match

import "testing"

func testFilter(t *testing.T, keep ...string) *LangFilter {
	t.Helper()
	f, err := NewLangFilter(keep)
	if err != nil {
		t.Fatalf("NewLangFilter(%v): %v", keep, err)
	}
	return f
}

func TestExcluded(t *testing.T) {
	f := testFilter(t, "en", "de")
	cases := []struct {
		name    string
		text    string
		exclude bool
	}{
		{
			name:    "english ad",
			text:    "Senior PHP Developer — you will work with our team building Symfony APIs. Experience with MySQL required.",
			exclude: false,
		},
		{
			name:    "german ad",
			text:    "Senior PHP Entwickler — wir suchen dich! Deine Aufgaben: Symfony APIs. Gute Kenntnisse in MySQL, Erfahrung mit Docker.",
			exclude: false,
		},
		{
			name:    "polish ad",
			text:    "Programista PHP — oferujemy pracę w zespole, wymagania: znajomość Symfony, doświadczenie z MySQL. Mile widziane Docker.",
			exclude: true,
		},
		{
			name:    "polish title with diacritics",
			text:    "Młodszy Programista PHP — praca zdalna, umowa B2B, znajomość Symfony",
			exclude: true,
		},
		{
			name:    "bulgarian cyrillic ad",
			text:    "Търсим PHP програмист за екипа ни в София. Изисквания: опит със Symfony и MySQL.",
			exclude: true,
		},
		{
			name:    "french ad",
			text:    "Développeur PHP Symfony — nous recherchons pour notre équipe. Missions: développement d'APIs. Expérience requise, maîtrise de MySQL.",
			exclude: true,
		},
		{
			name:    "polish title only, no prose",
			text:    "Programista PHP (PrestaShop/Symfony) Symfony PHP MySQL B2B",
			exclude: true,
		},
		{
			name:    "neutral tech keywords",
			text:    "PHP Symfony MySQL Docker Kubernetes REST API",
			exclude: false,
		},
		{
			name:    "short english title",
			text:    "Backend Engineer (PHP)",
			exclude: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.Excluded(tc.text); got != tc.exclude {
				t.Fatalf("Excluded = %v, want %v", got, tc.exclude)
			}
		})
	}
}

func TestExcludedJob(t *testing.T) {
	f := testFilter(t, "en", "de")
	// A Cyrillic title must not be rescued by Latin tech tags.
	if !f.ExcludedJob("ПРОГРАМИСТ - FULLSTACK PHP DEVELOPER", "PHP React ReactNative MySQL SQL JavaScript HTML/CSS Docker Linux") {
		t.Fatal("mixed cyrillic title should be excluded")
	}
	if f.ExcludedJob("Senior PHP Developer", "PHP Symfony MySQL") {
		t.Fatal("english title should pass")
	}
}

// The kept set decides what counts as foreign: the same texts flip when
// the profile reads Polish or Bulgarian instead of English.
func TestKeptSetDecidesWhatIsForeign(t *testing.T) {
	polish := "Programista PHP — oferujemy pracę w zespole, wymagania: znajomość Symfony, doświadczenie z MySQL."
	english := "Senior PHP Developer — you will work with our team building Symfony APIs. Experience with MySQL required."
	cyrillic := "Търсим PHP програмист за екипа ни в София. Изисквания: опит със Symfony и MySQL."

	pl := testFilter(t, "pl")
	if pl.Excluded(polish) {
		t.Fatal("polish ad should pass when polish is kept")
	}
	if !pl.Excluded(english) {
		t.Fatal("english ad should be excluded when only polish is kept")
	}

	bg := testFilter(t, "bg", "en")
	if bg.Excluded(cyrillic) {
		t.Fatal("bulgarian ad should pass when bulgarian is kept")
	}
}

func TestNoLanguagesDisablesFiltering(t *testing.T) {
	f := testFilter(t)
	if f.Excluded("Търсим PHP програмист за екипа ни в София.") {
		t.Fatal("an empty keep list must not exclude anything")
	}
}

func TestUnsupportedLanguage(t *testing.T) {
	if _, err := NewLangFilter([]string{"tlh"}); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}
