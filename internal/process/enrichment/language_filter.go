package enrichment

import "strings"

var languageAliases = map[string][]string{
	langEnglish:   {"eng"},
	langRussian:   {"rus"},
	langUkrainian: {"ukr"},
	langGreek:     {"ell", "gre"},
}

func normalizeLanguage(lang string) string {
	return strings.ToLower(strings.TrimSpace(lang))
}

func isUnknownLanguage(lang string) bool {
	normalized := normalizeLanguage(lang)
	return normalized == "" || normalized == langUnknown
}

func languageMatches(expected, actual string) bool {
	expected = normalizeLanguage(expected)
	if expected == "" || expected == langUnknown {
		return true
	}

	actual = normalizeLanguage(actual)
	if actual == "" {
		return false
	}

	if expected == actual {
		return true
	}

	for _, alias := range languageAliases[expected] {
		if actual == alias {
			return true
		}
	}

	return false
}
