package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
)

const (
	extractionTimeout  = 30 * time.Second
	maxContentLength   = 10 * 1024 * 1024 // 10MB
	maxExtractedLength = 100000           // 100KB of text
	maxExcerptLength   = 500
	hrefPrefixLen      = 6   // len(`href="`)
	contentAttrLen     = 9   // len(`content="`)
	metaLookbackWindow = 200 // bytes to search backwards for content attr
	extractorHeaderCT  = "Content-Type"
)

// Extractor errors.
var (
	errTooManyRedirects       = errors.New("too many redirects")
	errHTTPError              = errors.New("HTTP error")
	errUnsupportedContentType = errors.New("unsupported content type")
)

// ExtractionResult holds the extracted content from a web page.
type ExtractionResult struct {
	Title       string
	Content     string
	Description string
	Author      string
	Language    string
	Domain      string
	PublishedAt time.Time
	Links       []string
}

// Extractor extracts content from web pages.
type Extractor struct {
	httpClient *http.Client
	userAgent  string
	logger     *zerolog.Logger
}

// NewExtractor creates a new Extractor.
func NewExtractor(userAgent string, logger *zerolog.Logger) *Extractor {
	return &Extractor{
		httpClient: &http.Client{
			Timeout: extractionTimeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errTooManyRedirects
				}

				return nil
			},
		},
		userAgent: userAgent,
		logger:    logger,
	}
}

// Extract fetches and extracts content from a URL.
func (e *Extractor) Extract(ctx context.Context, rawURL string) (*ExtractionResult, error) {
	// Parse URL to get domain
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	// Fetch the page
	body, err := e.fetchPage(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	// Use go-readability to extract article content
	article, err := readability.FromReader(strings.NewReader(string(body)), parsed)
	if err != nil {
		return nil, fmt.Errorf("readability extraction: %w", err)
	}

	return e.buildResult(article, parsed, body), nil
}

// fetchPage fetches a web page and returns its body.
func (e *Extractor) fetchPage(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", e.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", errHTTPError, resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get(extractorHeaderCT)
	if !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml") {
		return nil, fmt.Errorf("%w: %s", errUnsupportedContentType, contentType)
	}

	// Read body with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentLength))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}

// buildResult builds an ExtractionResult from a readability article.
// Uses fallback chain: JSON-LD -> OG meta tags -> Readability for better metadata extraction.
func (e *Extractor) buildResult(article readability.Article, parsed *url.URL, body []byte) *ExtractionResult {
	html := string(body)

	// Extract JSON-LD structured data (most reliable source)
	jsonLD := extractJSONLD(html)

	// Extract OG meta tags for better metadata (fallback)
	ogTitle := extractMetaContent(html, "og:title")
	ogDescription := extractMetaContent(html, "og:description")
	ogLocale := extractMetaContent(html, "og:locale")
	articlePublished := extractMetaContent(html, "article:published_time")

	// Build result with fallback chain: JSON-LD -> OG -> Readability
	result := &ExtractionResult{
		Title:       coalesce(jsonLD.Headline, ogTitle, article.Title),
		Content:     truncateContent(article.TextContent, maxExtractedLength),
		Description: truncateContent(coalesce(jsonLD.Description, ogDescription, article.Excerpt), maxExcerptLength),
		Author:      coalesce(jsonLD.Author, article.Byline),
		Domain:      parsed.Host,
		Links:       extractLinks(html, parsed),
	}

	result.PublishedAt = parsePublishedDate(jsonLD.DatePublished, articlePublished, article.PublishedTime)
	result.Language = detectLanguage(jsonLD.Language, ogLocale, article.Title, article.TextContent)

	return result
}

// parsePublishedDate extracts published date using fallback chain: JSON-LD -> meta tag -> readability.
func parsePublishedDate(jsonLDDate, metaDate string, readabilityTime *time.Time) time.Time {
	if jsonLDDate != "" {
		if t, err := time.Parse(time.RFC3339, jsonLDDate); err == nil {
			return t
		}
	}

	if metaDate != "" {
		if t, err := time.Parse(time.RFC3339, metaDate); err == nil {
			return t
		}
	}

	if readabilityTime != nil {
		return *readabilityTime
	}

	return time.Time{}
}

// detectLanguage detects language using fallback chain: JSON-LD -> og:locale -> content detection.
func detectLanguage(jsonLDLang, ogLocale, title, content string) string {
	const minLangCodeLen = 2

	if jsonLDLang != "" && len(jsonLDLang) >= minLangCodeLen {
		return strings.ToLower(jsonLDLang[:minLangCodeLen])
	}

	if ogLocale != "" && len(ogLocale) >= minLangCodeLen {
		return strings.ToLower(ogLocale[:minLangCodeLen])
	}

	const maxLangDetectionLen = 1000

	textForLang := title + " " + content
	if len(textForLang) > maxLangDetectionLen {
		textForLang = textForLang[:maxLangDetectionLen]
	}

	return links.DetectLanguage(textForLang)
}

// jsonLDData holds extracted JSON-LD structured data.
type jsonLDData struct {
	Headline      string
	Description   string
	Author        string
	DatePublished string
	Language      string
}

// extractJSONLD extracts structured data from JSON-LD script tags.
func extractJSONLD(html string) jsonLDData {
	var data jsonLDData

	// Find JSON-LD script tag
	const (
		scriptStart = `<script type="application/ld+json">`
		scriptEnd   = `</script>`
	)

	idx := strings.Index(html, scriptStart)
	if idx == -1 {
		return data
	}

	start := idx + len(scriptStart)
	end := strings.Index(html[start:], scriptEnd)

	if end == -1 {
		return data
	}

	jsonStr := strings.TrimSpace(html[start : start+end])

	// Extract fields using simple string matching (avoids full JSON parsing)
	data.Headline = extractJSONField(jsonStr, "headline")
	data.Description = extractJSONField(jsonStr, "description")
	data.DatePublished = extractJSONField(jsonStr, "datePublished")
	data.Language = extractJSONField(jsonStr, "inLanguage")

	// Author can be a string or object
	author := extractJSONField(jsonStr, "author")
	if author == "" {
		// Try nested author.name
		authorIdx := strings.Index(jsonStr, `"author"`)
		if authorIdx != -1 {
			nameIdx := strings.Index(jsonStr[authorIdx:], `"name"`)
			if nameIdx != -1 && nameIdx < 200 {
				data.Author = extractJSONField(jsonStr[authorIdx:], "name")
			}
		}
	} else {
		data.Author = author
	}

	return data
}

// extractJSONField extracts a simple string field from JSON.
func extractJSONField(json, field string) string {
	pattern := `"` + field + `"`
	idx := strings.Index(json, pattern)

	if idx == -1 {
		return ""
	}

	// Skip past field name, colon, and whitespace to find opening quote
	start := skipJSONFieldPrefix(json, idx+len(pattern))
	if start == -1 {
		return ""
	}

	// Find closing quote
	end := findJSONStringEnd(json, start)
	if end == -1 {
		return ""
	}

	return json[start:end]
}

// skipJSONFieldPrefix skips colon and whitespace after a field name, returning position after opening quote.
func skipJSONFieldPrefix(json string, start int) int {
	for start < len(json) && (json[start] == ':' || json[start] == ' ' || json[start] == '\t' || json[start] == '\n') {
		start++
	}

	if start >= len(json) || json[start] != '"' {
		return -1
	}

	return start + 1 // Position after opening quote
}

// findJSONStringEnd finds the closing quote of a JSON string, handling escapes.
func findJSONStringEnd(json string, start int) int {
	for i := start; i < len(json); i++ {
		if json[i] == '\\' && i+1 < len(json) {
			i++ // Skip escaped character

			continue
		}

		if json[i] == '"' {
			return i
		}
	}

	return -1
}

// extractMetaContent extracts content from a meta tag by property or name.
func extractMetaContent(html, property string) string {
	// Try common meta tag patterns (OG tags use property=, others use name=)
	patterns := []string{
		`property="` + property + `" content="`,
		`property='` + property + `' content='`,
		`name="` + property + `" content="`,
	}

	for _, prefix := range patterns {
		idx := strings.Index(html, prefix)
		if idx == -1 {
			continue
		}

		start := idx + len(prefix)
		quote := `"`

		if strings.Contains(prefix, `'`) {
			quote = `'`
		}

		end := strings.Index(html[start:], quote)

		if end == -1 {
			continue
		}

		return html[start : start+end]
	}

	// Reverse order: content="..." property="..."
	propMarker := `property="` + property + `"`
	idx := strings.Index(html, propMarker)

	if idx != -1 {
		// Look backwards for content="
		searchStart := idx - metaLookbackWindow
		if searchStart < 0 {
			searchStart = 0
		}

		segment := html[searchStart:idx]
		contentIdx := strings.LastIndex(segment, `content="`)

		if contentIdx != -1 {
			start := searchStart + contentIdx + contentAttrLen
			end := strings.Index(html[start:], `"`)

			if end != -1 {
				return html[start : start+end]
			}
		}
	}

	return ""
}

// coalesce returns the first non-empty string.
func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// truncateContent truncates content to maxLen characters.
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}

	return content[:maxLen]
}

// extractLinks extracts all links from HTML content.
func extractLinks(html string, base *url.URL) []string {
	var links []string

	seen := make(map[string]bool)

	// Simple href extraction (avoid full HTML parsing for performance)
	for _, match := range findHrefs(html) {
		link := resolveLink(match, base)
		if link == "" || seen[link] {
			continue
		}

		seen[link] = true
		links = append(links, link)
	}

	return links
}

// findHrefs finds all href values in HTML.
func findHrefs(html string) []string {
	var hrefs []string

	idx := 0

	for {
		// Find href="
		hrefStart := strings.Index(html[idx:], `href="`)
		if hrefStart == -1 {
			break
		}

		idx += hrefStart + hrefPrefixLen

		// Find closing quote
		hrefEnd := strings.Index(html[idx:], `"`)
		if hrefEnd == -1 {
			break
		}

		href := html[idx : idx+hrefEnd]
		if href != "" && href != "#" {
			hrefs = append(hrefs, href)
		}

		idx += hrefEnd + 1
	}

	return hrefs
}

// resolveLink resolves a relative link against a base URL.
func resolveLink(href string, base *url.URL) string {
	// Skip javascript:, mailto:, tel:, etc.
	if strings.HasPrefix(href, "javascript:") ||
		strings.HasPrefix(href, "mailto:") ||
		strings.HasPrefix(href, "tel:") ||
		strings.HasPrefix(href, "#") {
		return ""
	}

	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}

	resolved := base.ResolveReference(parsed)

	// Only allow HTTP(S)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}

	return resolved.String()
}
