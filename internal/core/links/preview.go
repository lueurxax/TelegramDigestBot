package links

import (
	"encoding/json"
	"strings"
)

// Telegram API field names (PascalCase as per gotd/td serialization).
const (
	fieldWebpage     = "Webpage"
	fieldTitle       = "Title"
	fieldDescription = "Description"
	fieldSiteName    = "SiteName"
)

// ExtractPreviewText returns a concise preview string from Telegram webpage metadata.
func ExtractPreviewText(mediaJSON []byte) string {
	if len(mediaJSON) == 0 {
		return ""
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(mediaJSON, &payload); err != nil {
		return ""
	}

	webpage, ok := payload[fieldWebpage].(map[string]interface{})
	if !ok {
		return ""
	}

	parts := collectWebpageTextParts(webpage)
	if len(parts) == 0 {
		return ""
	}

	return strings.TrimSpace(strings.Join(parts, ". "))
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
