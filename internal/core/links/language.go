package links

import "unicode"

const (
	langEnglish   = "en"
	langRussian   = "ru"
	langUkrainian = "uk"

	// Language detection thresholds
	cyrillicThreshold = 0.3 // If >30% Cyrillic, consider Cyrillic language
	latinThreshold    = 0.5 // If >50% Latin, consider English
)

// DetectLanguage returns a short language code for the text ("en", "ru", "uk") or "" if unknown.
func DetectLanguage(text string) string {
	if text == "" {
		return ""
	}

	latinCount, cyrillicCount, totalLetters, hasUkrainian := countLanguageChars(text)

	if totalLetters == 0 {
		return ""
	}

	cyrillicRatio := float64(cyrillicCount) / float64(totalLetters)
	latinRatio := float64(latinCount) / float64(totalLetters)

	if cyrillicRatio >= cyrillicThreshold {
		if hasUkrainian {
			return langUkrainian
		}

		return langRussian
	}

	if latinRatio >= latinThreshold {
		return langEnglish
	}

	return ""
}

func countLanguageChars(text string) (latinCount, cyrillicCount, totalLetters int, hasUkrainian bool) {
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

func isUkrainianLetter(r rune) bool {
	switch r {
	case '\u0456', '\u0457', '\u0454', '\u0491', '\u0406', '\u0407', '\u0404', '\u0490':
		return true
	default:
		return false
	}
}
