package linkextract

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Default port suffixes for URL normalization.
const (
	httpsDefaultPort = ":443"
	httpDefaultPort  = ":80"
)

// ExtractURLsFromText scans plain text for HTTP(S) URLs and returns them normalized.
func ExtractURLsFromText(text string) []string {
	links := ExtractLinks(text)
	urls := make([]string, 0, len(links))

	for _, link := range links {
		if link.Type == LinkTypeWeb || link.Type == LinkTypeTelegram {
			urls = append(urls, link.URL)
		}
	}

	return urls
}

// ExtractAllURLs extracts URLs from entities/media JSON and raw text, deduplicating results.
// This is the recommended function for link seeding as it covers all URL sources.
func ExtractAllURLs(text string, entitiesJSON, mediaJSON []byte) []string {
	seen := make(map[string]bool)
	urls := make([]string, 0)

	addURL := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}

	// First extract from JSON (more reliable - explicit URL entities)
	for _, u := range ExtractURLsFromJSON(entitiesJSON, mediaJSON) {
		addURL(u)
	}

	// Then extract from text (catches URLs not in entities)
	for _, u := range ExtractURLsFromText(text) {
		addURL(u)
	}

	return urls
}

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

	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		if strings.Contains(raw, ".") {
			raw = "https://" + raw
		} else {
			return ""
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Host = stripDefaultPort(parsed.Scheme, parsed.Host)
	parsed.Fragment = ""

	if len(parsed.Path) > 1 && strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}

	parsed.RawQuery = stripTrackingParams(parsed.Query()).Encode()

	return parsed.String()
}

func stripDefaultPort(scheme, host string) string {
	if scheme == "https" && strings.HasSuffix(host, httpsDefaultPort) {
		return strings.TrimSuffix(host, httpsDefaultPort)
	}

	if scheme == "http" && strings.HasSuffix(host, httpDefaultPort) {
		return strings.TrimSuffix(host, httpDefaultPort)
	}

	return host
}

func stripTrackingParams(values url.Values) url.Values {
	if len(values) == 0 {
		return values
	}

	cleaned := url.Values{}

	for key, vals := range values {
		if isTrackingParam(key) {
			continue
		}

		cleaned[key] = vals
	}

	return cleaned
}

func isTrackingParam(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}

	if strings.HasPrefix(key, "utm_") {
		return true
	}

	switch key {
	case "fbclid", "gclid", "dclid", "yclid", "gbraid", "wbraid", "mc_cid", "mc_eid", "igshid", "_ga", "_gl":
		return true
	default:
		return false
	}
}
