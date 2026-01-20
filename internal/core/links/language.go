package links

import (
	"strings"
	"unicode"
)

const (
	langEnglish   = "en"
	langRussian   = "ru"
	langUkrainian = "uk"
	langGreek     = "el"

	// Language detection thresholds
	cyrillicThreshold = 0.3 // If >30% Cyrillic, consider Cyrillic language
	latinThreshold    = 0.5 // If >50% Latin, consider Latin-based language
	greekThreshold    = 0.2 // If >20% Greek, consider Greek

	englishStopwordMin   = 1
	englishStopwordRatio = 0.08
)

var englishStopwords = map[string]struct{}{
	"the": {}, "and": {}, "of": {}, "to": {}, "in": {}, "is": {}, "for": {}, "on": {}, "with": {},
	"as": {}, "by": {}, "from": {}, "at": {}, "that": {}, "this": {}, "be": {}, "are": {}, "was": {},
	"were": {}, "has": {}, "have": {}, "will": {}, "its": {}, "it": {},
}

// DetectLanguage returns a short language code for the text ("en", "ru", "uk", "el") or "" if unknown.
func DetectLanguage(text string) string {
	if text == "" {
		return ""
	}

	latinCount, cyrillicCount, greekCount, totalLetters, hasUkrainian := countLanguageChars(text)

	if totalLetters == 0 {
		return ""
	}

	cyrillicRatio := float64(cyrillicCount) / float64(totalLetters)
	latinRatio := float64(latinCount) / float64(totalLetters)
	greekRatio := float64(greekCount) / float64(totalLetters)

	if cyrillicRatio >= cyrillicThreshold {
		if hasUkrainian {
			return langUkrainian
		}

		return langRussian
	}

	if greekRatio >= greekThreshold {
		return langGreek
	}

	if latinRatio >= latinThreshold {
		if isLikelyEnglish(text) {
			return langEnglish
		}

		return ""
	}

	return ""
}

func countLanguageChars(text string) (latinCount, cyrillicCount, greekCount, totalLetters int, hasUkrainian bool) {
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}

		totalLetters++

		if isCyrillic(r) {
			cyrillicCount++

			if isUkrainianLetter(r) {
				hasUkrainian = true
			}
		} else if isGreek(r) {
			greekCount++
		} else if isLatin(r) {
			latinCount++
		}
	}

	return
}

func isCyrillic(r rune) bool {
	return (r >= 0x0400 && r <= 0x04FF) || // Cyrillic
		(r >= 0x0500 && r <= 0x052F) // Cyrillic Supplement
}

func isLatin(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
		(r >= 0x00C0 && r <= 0x00FF) || // Latin-1 Supplement
		(r >= 0x0100 && r <= 0x017F) // Latin Extended-A
}

func isGreek(r rune) bool {
	return (r >= 0x0370 && r <= 0x03FF) || // Greek and Coptic
		(r >= 0x1F00 && r <= 0x1FFF) // Greek Extended
}

func isUkrainianLetter(r rune) bool {
	switch r {
	case '\u0456', '\u0457', '\u0454', '\u0491', '\u0406', '\u0407', '\u0404', '\u0490':
		return true
	default:
		return false
	}
}

func isLikelyEnglish(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})

	if len(words) == 0 {
		return false
	}

	matches := 0

	for _, w := range words {
		if _, ok := englishStopwords[w]; ok {
			matches++
		}
	}

	if matches < englishStopwordMin {
		return false
	}

	return float64(matches)/float64(len(words)) >= englishStopwordRatio
}
