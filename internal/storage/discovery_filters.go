package db

import "strings"

// NormalizeDiscoveryKeywords standardizes keyword lists for discovery filters.
func NormalizeDiscoveryKeywords(keywords []string) []string {
	normalized := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))

	for _, keyword := range keywords {
		trimmed := strings.ToLower(strings.TrimSpace(keyword))
		if trimmed == "" {
			continue
		}

		if _, ok := seen[trimmed]; ok {
			continue
		}

		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	return normalized
}

// DiscoverySearchText returns the normalized text used for keyword matching.
func DiscoverySearchText(d DiscoveredChannel) string {
	text := strings.TrimSpace(strings.ToLower(strings.TrimSpace(d.Title + " " + d.Description)))

	return strings.TrimSpace(text)
}

// DiscoveryKeywordMatches checks if any keyword appears in the text.
func DiscoveryKeywordMatches(text string, keywords []string) bool {
	if text == "" || len(keywords) == 0 {
		return false
	}

	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}

		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

// EvaluateDiscoveryKeywords returns allow/deny match results and the normalized text.
func EvaluateDiscoveryKeywords(d DiscoveredChannel, allow, deny []string) (allowMatch bool, denyMatch bool, text string) {
	text = DiscoverySearchText(d)
	denyMatch = DiscoveryKeywordMatches(text, deny)

	if len(allow) == 0 {
		return true, denyMatch, text
	}

	allowMatch = DiscoveryKeywordMatches(text, allow)

	return allowMatch, denyMatch, text
}

// FilterDiscoveriesByKeywords filters discoveries using allow/deny keywords.
func FilterDiscoveriesByKeywords(discoveries []DiscoveredChannel, allow, deny []string) ([]DiscoveredChannel, int, int) {
	filtered := make([]DiscoveredChannel, 0, len(discoveries))
	allowMiss := 0
	denyHit := 0

	for _, discovery := range discoveries {
		allowMatch, denyMatch, _ := EvaluateDiscoveryKeywords(discovery, allow, deny)
		if denyMatch {
			denyHit++
			continue
		}

		if len(allow) > 0 && !allowMatch {
			allowMiss++
			continue
		}

		filtered = append(filtered, discovery)
	}

	return filtered, allowMiss, denyHit
}
