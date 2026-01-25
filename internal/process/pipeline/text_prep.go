package pipeline

import (
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links/linkextract"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func extractPreviewText(mediaJSON []byte) string {
	return links.ExtractPreviewText(mediaJSON)
}

func previewTextFromMessage(msg *db.RawMessage) string {
	if msg == nil {
		return ""
	}

	preview := strings.TrimSpace(msg.PreviewText)
	if preview != "" {
		return preview
	}

	return extractPreviewText(msg.MediaJSON)
}

func combinePreviewText(text, preview string) string {
	text = strings.TrimSpace(text)
	preview = strings.TrimSpace(preview)

	if preview == "" {
		return text
	}

	if text == "" || len(text) < domain.ShortMessageThreshold {
		if text == "" {
			return preview
		}

		return strings.TrimSpace(text + "\n\n" + preview)
	}

	return text
}

func detectLanguageForFilter(text, preview string) string {
	if lang := links.DetectLanguage(text); lang != "" {
		return lang
	}

	if preview != "" {
		if lang := links.DetectLanguage(preview); lang != "" {
			return lang
		}
	}

	return ""
}

func hasLinkOrPreview(msg *db.RawMessage, previewText string) bool {
	if strings.TrimSpace(previewText) != "" {
		return true
	}

	if msg != nil && strings.TrimSpace(msg.PreviewText) != "" {
		return true
	}

	if len(linkextract.ExtractLinks(msg.Text)) > 0 {
		return true
	}

	return len(linkextract.ExtractURLsFromJSON(msg.EntitiesJSON, msg.MediaJSON)) > 0
}
