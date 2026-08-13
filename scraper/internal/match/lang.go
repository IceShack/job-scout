package match

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// An ad written in a language you don't read implies a working language
// (and often a location) that doesn't fit. Detection is heuristic: script
// ratio, language-specific diacritics, and distinctive stopwords, weighed
// against evidence for the languages you kept.

// language carries the evidence that distinguishes one language from the
// others. Any field may be empty: "da" is recognised by its job titles
// alone, "bg" by its script alone.
type language struct {
	// stopwords is a regex alternation of common function words.
	stopwords string
	// titleWords are job-title words that pin the language on their own —
	// vital for listing cards where the title is all the text there is.
	// English and German have none on purpose: their role words ("developer",
	// "engineer") show up in ads written in any language.
	titleWords string
	// script is the dominant non-Latin script, if any.
	script *unicode.RangeTable
}

var languages = map[string]language{
	"en": {stopwords: `the|and|you|your|with|will|team|experience|work|skills`},
	"de": {stopwords: `und|der|die|das|wir|dich|deine|erfahrung|kenntnisse|aufgaben`},
	"pl": {
		stopwords:  `oraz|które|którzy|będzie|będziesz|pracy|praca|znajomość|doświadczenie|zespołu|zespole|umowa|wymagania|oferujemy|mile widziane|programista|stanowisko`,
		titleWords: `programista|programistka`,
	},
	"fr": {
		stopwords:  `nous|vous|équipe|expérience|développeur|développement|poste|entreprise|missions|profil recherché|maîtrise`,
		titleWords: `développeur|développeuse`,
	},
	"es": {
		stopwords:  `desarrollo|experiencia|equipo|trabajo|empresa|buscamos|conocimientos|años|puesto|requisitos`,
		titleWords: `desarrollador|desarrolladora`,
	},
	"nl": {
		stopwords:  `ervaring|kennis|werkzaamheden|functie|wij zoeken|jij bent|binnen ons|vaardigheden`,
		titleWords: `ontwikkelaar`,
	},
	"it": {
		stopwords:  `esperienza|sviluppo|lavoro|azienda|cerchiamo|conoscenza|requisiti|competenze`,
		titleWords: `sviluppatore`,
	},
	"pt": {
		stopwords:  `experiência|desenvolvimento|trabalho|empresa|procuramos|conhecimento|requisitos|equipe|equipa`,
		titleWords: `programador`,
	},
	"da": {titleWords: `udvikler`},
	"sv": {titleWords: `utvecklare`},
	"fi": {titleWords: `kehittäjä`},
	"bg": {titleWords: `програмист`, script: unicode.Cyrillic},
	"ru": {titleWords: `разработчик`, script: unicode.Cyrillic},
	"uk": {script: unicode.Cyrillic},
}

// SupportedLanguages are the codes accepted in profile.languages.
func SupportedLanguages() []string {
	codes := make([]string, 0, len(languages))
	for code := range languages {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	return codes
}

// Distinctive letters, counted as extra evidence for a language group.
// German umlauts and ß are deliberately excluded everywhere: they also
// occur in the loanwords English ads are full of.
var diacritics = []struct {
	chars string
	min   int
	langs []string
}{
	{chars: "ąćęłńśźż", min: 4, langs: []string{"pl"}},
	{chars: "àâçéèêëîïôùûñíóáãõ", min: 6, langs: []string{"fr", "es", "it", "pt"}},
}

func langRe(alternation string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^a-zà-ž])(` + alternation + `)($|[^a-zà-ž])`)
}

func countMatches(re *regexp.Regexp, s string) int {
	return len(re.FindAllString(s, 25))
}

// LangFilter rejects ads written in a language outside the kept set.
type LangFilter struct {
	// keep is empty when no filtering was configured.
	keep []string
	// kept matches stopwords of every language being kept, as one vote.
	kept *regexp.Regexp
	// foreign holds one stopword matcher per other language; the strongest
	// single language votes, so unrelated matches don't add up.
	foreign []*regexp.Regexp
	// foreignTitles pins an ad's language from its title alone.
	foreignTitles *regexp.Regexp
	// foreignScripts are writing systems no kept language uses.
	foreignScripts []*unicode.RangeTable
	// foreignChars are the diacritic groups still in play.
	foreignChars []struct {
		chars string
		min   int
	}
}

// NewLangFilter builds a filter that keeps ads in the given languages. An
// empty list disables language filtering.
func NewLangFilter(keep []string) (*LangFilter, error) {
	f := &LangFilter{keep: keep}
	if len(keep) == 0 {
		return f, nil
	}
	var keptWords []string
	for _, code := range keep {
		lang, ok := languages[code]
		if !ok {
			return nil, fmt.Errorf("unsupported language %q (supported: %s)",
				code, strings.Join(SupportedLanguages(), ", "))
		}
		if lang.stopwords != "" {
			keptWords = append(keptWords, lang.stopwords)
		}
	}
	if len(keptWords) > 0 {
		f.kept = langRe(strings.Join(keptWords, "|"))
	}

	var titleWords []string
	for code, lang := range languages {
		if slices.Contains(keep, code) {
			continue
		}
		if lang.stopwords != "" {
			f.foreign = append(f.foreign, langRe(lang.stopwords))
		}
		if lang.titleWords != "" {
			titleWords = append(titleWords, lang.titleWords)
		}
		if lang.script != nil && !f.keepsScript(lang.script) {
			f.foreignScripts = appendUnique(f.foreignScripts, lang.script)
		}
	}
	if len(titleWords) > 0 {
		slices.Sort(titleWords) // map iteration order must not change the regex
		f.foreignTitles = langRe(strings.Join(titleWords, "|"))
	}

	for _, group := range diacritics {
		for _, code := range group.langs {
			if !slices.Contains(keep, code) {
				f.foreignChars = append(f.foreignChars, struct {
					chars string
					min   int
				}{group.chars, group.min})
				break
			}
		}
	}
	return f, nil
}

func (f *LangFilter) keepsScript(script *unicode.RangeTable) bool {
	for _, code := range f.keep {
		if languages[code].script == script {
			return true
		}
	}
	return false
}

func appendUnique(tables []*unicode.RangeTable, t *unicode.RangeTable) []*unicode.RangeTable {
	if slices.Contains(tables, t) {
		return tables
	}
	return append(tables, t)
}

// ExcludedJob checks the title alone as well as the full text: a foreign
// title must not be rescued by a pile of Latin tech tags in the
// description.
func (f *LangFilter) ExcludedJob(title, description string) bool {
	return f.Excluded(title) || f.Excluded(title+" "+description)
}

// Excluded reports whether the text is clearly in a language outside the
// kept set. Neutral or ambiguous text (tech keywords, short titles)
// passes.
func (f *LangFilter) Excluded(text string) bool {
	if len(f.keep) == 0 {
		return false
	}
	text = strings.ToLower(text)

	letters := 0
	scriptCounts := make([]int, len(f.foreignScripts))
	charCounts := make([]int, len(f.foreignChars))
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
			for i, script := range f.foreignScripts {
				if unicode.Is(script, r) {
					scriptCounts[i]++
				}
			}
		}
		for i, group := range f.foreignChars {
			if strings.ContainsRune(group.chars, r) {
				charCounts[i]++
			}
		}
	}
	if letters == 0 {
		return false
	}
	for _, count := range scriptCounts {
		if float64(count)/float64(letters) > 0.15 {
			return true
		}
	}

	keptScore := 0
	if f.kept != nil {
		keptScore = countMatches(f.kept, text)
	}
	if f.foreignTitles != nil && countMatches(f.foreignTitles, text) >= 1 && keptScore <= 1 {
		return true
	}
	foreign := 0
	for _, re := range f.foreign {
		if n := countMatches(re, text); n > foreign {
			foreign = n
		}
	}
	// Diacritics reinforce the vote: a handful of ł/ż or é/ç in otherwise
	// keyword-heavy text is a strong signal.
	for i, group := range f.foreignChars {
		if charCounts[i] >= group.min {
			foreign += 2
			break
		}
	}
	return foreign >= 3 && foreign > keptScore
}
