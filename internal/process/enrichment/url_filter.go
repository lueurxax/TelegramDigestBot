package enrichment

import (
	"net/url"
	"regexp"
	"strings"
)

// Navigation URL patterns that typically indicate non-article pages.
// These pages often contain only navigation menus, category listings, or indexes.
var navigationPatterns = []*regexp.Regexp{
	// Category and taxonomy pages
	regexp.MustCompile(`(?i)/categor(y|ies)/`),
	regexp.MustCompile(`(?i)/tag(s)?/`),
	regexp.MustCompile(`(?i)/topic(s)?/`),
	regexp.MustCompile(`(?i)/archive(s)?/`),
	regexp.MustCompile(`(?i)/author(s)?/`),
	regexp.MustCompile(`(?i)/section(s)?/`),
	regexp.MustCompile(`(?i)/rubric(s)?/`),
	regexp.MustCompile(`(?i)/tema/`), // Russian for topic

	// Pagination and index pages
	regexp.MustCompile(`(?i)/page/\d+`),
	regexp.MustCompile(`(?i)/p/\d+`),
	// Match /index.html only at root or category level, not deep article paths
	// This avoids filtering CNN-style articles that end with /index.html
	regexp.MustCompile(`(?i)^/[^/]*/index\.(html?|php|asp)$`),
	regexp.MustCompile(`(?i)/latest/?$`),
	regexp.MustCompile(`(?i)/recent/?$`),
	regexp.MustCompile(`(?i)/all/?$`),

	// Search and listing pages
	regexp.MustCompile(`(?i)/search[/?]`),
	regexp.MustCompile(`(?i)/results[/?]`),
	regexp.MustCompile(`(?i)/feed[/?]`),
	regexp.MustCompile(`(?i)/rss[/?]`),
	regexp.MustCompile(`(?i)/sitemap`),

	// User and profile pages
	regexp.MustCompile(`(?i)/user(s)?/`),
	regexp.MustCompile(`(?i)/profile(s)?/`),
	regexp.MustCompile(`(?i)/member(s)?/`),

	// Media galleries (usually contain lists, not articles)
	regexp.MustCompile(`(?i)/gallery[/?]`),
	regexp.MustCompile(`(?i)/photos?[/?]`),
	regexp.MustCompile(`(?i)/video(s)?[/?]`),
	regexp.MustCompile(`(?i)/media[/?]`),

	// Contact/about pages
	regexp.MustCompile(`(?i)/contact[/?]`),
	regexp.MustCompile(`(?i)/about[/?]`),
	regexp.MustCompile(`(?i)/privacy[/?]`),
	regexp.MustCompile(`(?i)/terms[/?]`),
	regexp.MustCompile(`(?i)/faq[/?]`),
}

// Query parameters that indicate navigation or listing pages.
var navigationQueryParams = []string{
	"page", "p", "offset", "skip",
	"tag", "category", "cat", "topic",
	"sort", "order", "filter",
	"year", "month", "day", // Date-based archives
	"search", "q", "query", // Search queries
}

// URLFilter provides URL filtering for enrichment search results.
type URLFilter struct {
	skipNavigationPages bool
	minPathSegments     int
}

// NewURLFilter creates a URL filter.
func NewURLFilter(skipNavigationPages bool) *URLFilter {
	return &URLFilter{
		skipNavigationPages: skipNavigationPages,
		minPathSegments:     1, // Require at least one path segment beyond domain
	}
}

// IsNavigationURL checks if a URL appears to be a navigation/index page
// rather than an actual article. Returns the skip reason or empty string if allowed.
func (f *URLFilter) IsNavigationURL(rawURL string) string {
	if !f.skipNavigationPages {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	path := parsed.Path

	// Check for homepage (root path only)
	if isHomepage(path) {
		return SkipReasonNavigationPage
	}

	// Check path against navigation patterns
	for _, pattern := range navigationPatterns {
		if pattern.MatchString(path) {
			return SkipReasonNavigationPage
		}
	}

	// Check query parameters for navigation indicators
	if hasNavigationQueryParams(parsed.Query()) {
		return SkipReasonNavigationPage
	}

	// Check for very short paths (likely category pages)
	if f.minPathSegments > 0 && countPathSegments(path) < f.minPathSegments {
		// Allow if path has file extension (likely an article)
		if !hasFileExtension(path) {
			return SkipReasonShortPath
		}
	}

	return ""
}

const (
	// SkipReasonNavigationPage indicates URL was skipped because it's a navigation page.
	SkipReasonNavigationPage = "navigation_page"

	// SkipReasonShortPath indicates URL was skipped because path is too short.
	SkipReasonShortPath = "short_path"

	// Language prefix length constants
	langCodeLen   = 2 // "en", "ru"
	localeCodeLen = 5 // "en-US", "ru-RU"
	localeDashPos = 2 // Position of dash in locale codes
)

func isHomepage(path string) bool {
	path = strings.TrimSuffix(path, "/")
	// Only root paths are homepages - paths with /index.html at end of a longer path are articles
	if path == "" || path == "/home" {
		return true
	}

	// Root index pages (not deep paths with index.html)
	if path == "/index.html" || path == "/index.htm" {
		return true
	}

	return false
}

func hasNavigationQueryParams(query url.Values) bool {
	for _, param := range navigationQueryParams {
		if query.Has(param) {
			return true
		}
	}

	return false
}

func countPathSegments(path string) int {
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}

	segments := strings.Split(path, "/")
	count := 0

	for _, seg := range segments {
		// Skip empty segments
		if seg == "" {
			continue
		}

		// Skip language prefixes (en, ru, de, etc.) - they don't count as content segments
		// Only skip if it looks like a language code (2 letters, all alpha) or locale (xx-YY)
		if isLanguagePrefix(seg) {
			continue
		}

		count++
	}

	return count
}

// isLanguagePrefix checks if a segment looks like a language code (en, ru) or locale (en-US).
func isLanguagePrefix(seg string) bool {
	// Must be 2 chars (en) or 5 chars with dash (en-US)
	isLangCode := len(seg) == langCodeLen
	isLocaleCode := len(seg) == localeCodeLen && seg[localeDashPos] == '-'

	if !isLangCode && !isLocaleCode {
		return false
	}

	// Must be all alphabetic (not numeric like "15" or "2024")
	for _, r := range seg {
		if r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}

	return true
}

func hasFileExtension(path string) bool {
	// Check for common article file extensions
	extensions := []string{".html", ".htm", ".php", ".aspx", ".asp", ".shtml"}

	lowerPath := strings.ToLower(path)
	for _, ext := range extensions {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}

	return false
}
