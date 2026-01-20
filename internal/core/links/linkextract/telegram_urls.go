package linkextract

import (
	"encoding/json"
	"strings"
)

// ExtractURLsFromJSON parses Telegram entities/media JSON and returns any HTTP(S) URLs.
func ExtractURLsFromJSON(entitiesJSON, mediaJSON []byte) []string {
	seen := make(map[string]bool)
	urls := make([]string, 0)

	addURL := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}

		url := normalizeURL(raw)
		if url == "" || seen[url] {
			return
		}

		seen[url] = true
		urls = append(urls, url)
	}

	collectURLs(entitiesJSON, addURL)
	collectURLs(mediaJSON, addURL)

	return urls
}

func collectURLs(raw []byte, add func(string)) {
	if len(raw) == 0 {
		return
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}

	visitJSON(payload, add)
}

func visitJSON(val any, add func(string)) {
	switch v := val.(type) {
	case map[string]any:
		for key, child := range v {
			if isURLKey(key) {
				if s, ok := child.(string); ok {
					add(s)
				}
			}

			visitJSON(child, add)
		}
	case []any:
		for _, child := range v {
			visitJSON(child, add)
		}
	}
}

func isURLKey(key string) bool {
	switch strings.ToLower(key) {
	case "url", "displayurl":
		return true
	default:
		return false
	}
}

func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}

	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}

	if strings.Contains(raw, ".") {
		return "https://" + raw
	}

	return ""
}
