package factcheck

import (
	"strings"
	"unicode"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
)

const (
	DefaultMinClaimLength = 40
	maxClaimLength        = 300
)

// BuildClaimFromSummary extracts a short, single-sentence claim from a summary.
func BuildClaimFromSummary(summary string) string {
	text := htmlutils.StripHTMLTags(summary)
	text = strings.TrimSpace(text)
	text = strings.TrimLeftFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})

	if text == "" {
		return ""
	}

	claim := firstSentence(text)
	if len(claim) > maxClaimLength {
		claim = claim[:maxClaimLength]
	}

	return strings.TrimSpace(claim)
}

// NormalizeClaim produces a stable cache key for a claim string.
func NormalizeClaim(claim string) string {
	fields := strings.Fields(strings.ToLower(claim))
	return strings.Join(fields, " ")
}

func firstSentence(text string) string {
	var last int

	for i, r := range text {
		if r == '.' || r == '!' || r == '?' {
			last = i + 1
			break
		}
	}

	if last == 0 {
		return strings.TrimSpace(text)
	}

	sentence := strings.TrimSpace(text[:last])

	return strings.TrimRightFunc(sentence, unicode.IsSpace)
}
