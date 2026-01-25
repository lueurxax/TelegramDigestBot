package pipeline

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
)

const (
	weakSummaryMinChars  = 60
	weakSummaryMinTokens = 6
	maxLeadSentenceLen   = 200
)

var (
	sentenceSplitRegex = regexp.MustCompile(`([.!?…]+)\s+`)
	leadNumberRegex    = regexp.MustCompile(`\b\d{1,4}([:/.-]\d{1,2})?\b`)
	leadCapsRegex      = regexp.MustCompile(`\b[A-ZА-Я][\p{L}]+\s+[A-ZА-Я][\p{L}]+\b`)
	leadAcronymRegex   = regexp.MustCompile(`\b[A-Z]{2,5}\b`)
	leadMentionRegex   = regexp.MustCompile(`[@#]\w+`)
)

var defaultSummaryStripPhrases = []string{
	"summary:",
	"summary",
	"digest:",
	"digest",
	"сводка:",
	"итог:",
	"итоги:",
	"дайджест:",
}

func parseSummaryStripPhrases(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}

func postProcessSummary(summary string, maxChars int, stripPhrases []string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return summary
	}

	summary = stripSummaryPrefixes(summary, stripPhrases)
	summary = normalizeWhitespace(summary)
	summary = enforceSentenceLimit(summary)
	summary = truncateSummary(summary, maxChars)

	return summary
}

func stripSummaryPrefixes(summary string, phrases []string) string {
	if len(phrases) == 0 {
		return summary
	}

	lower := strings.ToLower(summary)

	for _, phrase := range phrases {
		trimmed := strings.TrimSpace(phrase)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(lower, strings.ToLower(trimmed)) {
			summary = strings.TrimSpace(summary[len(trimmed):])
			break
		}
	}

	return summary
}

func normalizeWhitespace(text string) string {
	parts := strings.Fields(text)
	return strings.Join(parts, " ")
}

func enforceSentenceLimit(summary string) string {
	sentences := splitSentences(summary)
	if len(sentences) == 0 {
		return summary
	}

	if len(sentences) == 1 {
		return strings.TrimSpace(sentences[0])
	}

	first := strings.TrimSpace(sentences[0])
	second := strings.TrimSpace(sentences[1])

	if second != "" && len([]rune(second)) <= 80 {
		return strings.TrimSpace(first + " " + second)
	}

	return first
}

func truncateSummary(summary string, maxChars int) string {
	if maxChars <= 0 {
		return summary
	}

	runes := []rune(summary)
	if len(runes) <= maxChars {
		return summary
	}

	cut := string(runes[:maxChars])
	if idx := strings.LastIndex(cut, " "); idx > 0 {
		cut = cut[:idx]
	}

	return strings.TrimSpace(cut)
}

func isWeakSummary(summary string) bool {
	clean := htmlutils.StripHTMLTags(summary)
	if len([]rune(clean)) < weakSummaryMinChars {
		return true
	}

	tokens := strings.Fields(clean)

	return len(tokens) < weakSummaryMinTokens
}

func selectLeadSentence(text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}

	sentences := splitSentences(clean)
	best := ""
	bestScore := -1

	for _, sentence := range sentences {
		s := strings.TrimSpace(sentence)
		if s == "" {
			continue
		}

		if len([]rune(s)) > maxLeadSentenceLen {
			continue
		}

		score := scoreLeadSentence(s)
		if score > bestScore || (score == bestScore && len([]rune(s)) > len([]rune(best))) {
			best = s
			bestScore = score
		}
	}

	return best
}

func scoreLeadSentence(sentence string) int {
	score := 0

	if leadNumberRegex.MatchString(sentence) {
		score += 2
	}

	if leadCapsRegex.MatchString(sentence) {
		score += 2
	}

	if leadMentionRegex.MatchString(sentence) {
		score++
	}

	if leadAcronymRegex.MatchString(sentence) {
		score++
	}

	return score
}

func splitSentences(text string) []string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return nil
	}

	parts := sentenceSplitRegex.Split(clean, -1)
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}

func isMostlySymbols(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return false
		}
	}

	return true
}
