package enrichment

import (
	"strings"
	"unicode"
)

var russianSuffixes = []string{
	"иями", "ями", "ами", "ыми", "ими",
	"его", "ого", "ему", "ому", "ыми", "ими",
	"иях", "ях", "ах", "ее", "ое", "ые",
	"ий", "ый", "ой", "ая", "яя", "ое", "ее",
	"ам", "ям", "ом", "ем", "ах", "ях",
	"ою", "ею", "ью", "ия", "ья", "ие", "ье",
	"ую", "юю", "ов", "ев", "ей",
	"а", "я", "о", "е", "у", "ю", "ь", "и",
}

func stripCombiningMarks(text string) string {
	var b strings.Builder
	b.Grow(len(text))

	for _, r := range text {
		if unicode.Is(unicode.Mn, r) {
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

func normalizeCyrillic(text string) string {
	text = stripCombiningMarks(text)
	text = strings.ReplaceAll(text, "ё", "е")
	text = strings.ReplaceAll(text, "Ё", "Е")

	return text
}

func normalizeToken(word string) string {
	word = normalizeTokenBase(word)

	if isMostlyCyrillic(word) {
		word = stemRussian(word)
	}

	return word
}

func normalizeTokenBase(word string) string {
	word = normalizeCyrillic(word)
	word = strings.ToLower(word)

	return word
}

func isMostlyCyrillic(word string) bool {
	var letters, cyrillic int

	for _, r := range word {
		if !unicode.IsLetter(r) {
			continue
		}

		letters++

		if isCyrillicRune(r) {
			cyrillic++
		}
	}

	return letters > 0 && cyrillic*2 >= letters
}

func isCyrillicRune(r rune) bool {
	return (r >= 0x0400 && r <= 0x04FF) || (r >= 0x0500 && r <= 0x052F)
}

func stemRussian(word string) string {
	if runeCount(word) < 5 {
		return word
	}

	for _, suffix := range russianSuffixes {
		if strings.HasSuffix(word, suffix) {
			root := trimSuffixRunes(word, suffix)
			if runeCount(root) >= 3 {
				return root
			}
		}
	}

	return word
}

func trimSuffixRunes(word, suffix string) string {
	wordRunes := []rune(word)

	suffixRunes := []rune(suffix)
	if len(suffixRunes) > len(wordRunes) {
		return word
	}

	return string(wordRunes[:len(wordRunes)-len(suffixRunes)])
}

func runeCount(s string) int {
	return len([]rune(s))
}
