package pipeline

import (
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

func resolveItemLanguage(c llm.MessageInput, res llm.BatchResult) (string, string) {
	if lang := links.DetectLanguage(c.Text); lang != "" {
		return lang, "original"
	}

	preview := extractPreviewText(c.MediaJSON)
	if lang := links.DetectLanguage(preview); lang != "" {
		return lang, "preview"
	}

	if res.Language != "" {
		return normalizeLanguage(res.Language), "summary"
	}

	return "", ""
}

func normalizeLanguage(lang string) string {
	return strings.ToLower(strings.TrimSpace(lang))
}

func detectSummaryLanguage(summary string, fallback string) string {
	if lang := links.DetectLanguage(summary); lang != "" {
		return lang
	}

	return strings.ToLower(strings.TrimSpace(fallback))
}
