package filters

import (
	"strings"
	"unicode"
)

const footerSkipLines = 2

var boilerplatePrefixes = []string{
	"subscribe",
	"share this",
	"share",
	"donate",
	"support us",
	"follow",
	"подпис",
	"поддерж",
	"подел",
	"донат",
	"спонсор",
}

func IsEmojiOnly(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}

	hasLetterOrDigit := false

	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			hasLetterOrDigit = true
			break
		}
	}

	return !hasLetterOrDigit
}

func StripFooterBoilerplate(text string) (string, bool) {
	lines := splitLines(text)
	if len(lines) < 2 {
		return text, false
	}

	footerStart := findFooterStart(lines)
	if footerStart < 0 || footerStart >= len(lines) {
		return text, false
	}

	if !isBoilerplateBlock(lines[footerStart:]) {
		return text, false
	}

	cleaned := strings.TrimSpace(strings.Join(lines[:footerStart], "\n"))

	return cleaned, true
}

func IsBoilerplateOnly(text string) bool {
	lines := splitLines(text)
	hasNonEmpty := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		hasNonEmpty = true

		if looksLikeURL(line) {
			return false
		}

		if !isBoilerplateLine(line) {
			return false
		}
	}

	return hasNonEmpty
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n")
}

func findFooterStart(lines []string) int {
	lastBlank := -1

	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			lastBlank = i
			break
		}
	}

	if lastBlank >= 0 && lastBlank < len(lines)-1 {
		return lastBlank + 1
	}

	if len(lines) >= 3 {
		return len(lines) - footerSkipLines
	}

	return -1
}

func isBoilerplateBlock(lines []string) bool {
	keywords := 0
	urlLines := 0
	total := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		total++

		if looksLikeURL(trimmed) {
			urlLines++
		}

		if isBoilerplateLine(trimmed) {
			keywords++
		}
	}

	if total == 0 {
		return false
	}

	if keywords >= 2 {
		return true
	}

	return keywords >= 1 && urlLines >= 1
}

func isBoilerplateLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range boilerplatePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	return false
}

func looksLikeURL(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "t.me/")
}
