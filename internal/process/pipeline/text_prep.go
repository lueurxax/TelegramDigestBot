package pipeline

import (
	"encoding/json"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links/linkextract"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Telegram API field names (PascalCase as per gotd/td serialization)
const (
	fieldWebpage     = "Webpage"
	fieldTitle       = "Title"
	fieldDescription = "Description"
	fieldSiteName    = "SiteName"
)

func extractPreviewText(mediaJSON []byte) string {
	if len(mediaJSON) == 0 {
		return ""
	}

	webpage := parseWebpageFromMedia(mediaJSON)
	if webpage == nil {
		return ""
	}

	parts := collectWebpageTextParts(webpage)

	return strings.TrimSpace(strings.Join(parts, ". "))
}

func parseWebpageFromMedia(mediaJSON []byte) map[string]interface{} {
	var payload map[string]interface{}
	if err := json.Unmarshal(mediaJSON, &payload); err != nil {
		return nil
	}

	webpage, ok := payload[fieldWebpage].(map[string]interface{})
	if !ok {
		return nil
	}

	return webpage
}

func collectWebpageTextParts(webpage map[string]interface{}) []string {
	var parts []string

	for _, field := range []string{fieldTitle, fieldDescription, fieldSiteName} {
		if val, ok := webpage[field].(string); ok && val != "" {
			parts = append(parts, val)
		}
	}

	return parts
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

	if len(linkextract.ExtractLinks(msg.Text)) > 0 {
		return true
	}

	return len(linkextract.ExtractURLsFromJSON(msg.EntitiesJSON, msg.MediaJSON)) > 0
}
