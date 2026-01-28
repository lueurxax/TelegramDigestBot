package crawler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codeberg.org/readeck/go-readability/v2"
	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
)

const (
	extractionTimeout  = 30 * time.Second
	feedFetchTimeout   = 10 * time.Second
	maxContentLength   = 10 * 1024 * 1024 // 10MB
	maxExtractedLength = 100000           // 100KB of text
	maxExcerptLength   = 500
	minContentLength   = 100 // Minimum content length to accept
	hrefPrefixLen      = 6   // len(`href="`)
	contentAttrLen     = 9   // len(`content="`)
	metaLookbackWindow = 200 // bytes to search backwards for content attr
	extractorHeaderCT  = "Content-Type"

	// OpenGraph and meta tag property names.
	ogTitle            = "og:title"
	ogDescription      = "og:description"
	ogLocale           = "og:locale"
	articlePublishedAt = "article:published_time"

	// Content type constants for RSS/Atom detection.
	ctApplicationRSS  = "application/rss"
	ctApplicationAtom = "application/atom"
	ctApplicationXML  = "application/xml"
	ctTextXML         = "text/xml"

	// Log/error message constants.
	msgContentTooShort = "content too short"
	logKeyDomain       = "domain"
	logKeyURL          = "url"

	// Error format strings.
	errFmtParseFeed = "parse feed: %w"
	errFmtFetchFeed = "fetch feed: %w"
)

// Extractor errors.
var (
	errTooManyRedirects       = errors.New("too many redirects")
	errHTTPError              = errors.New("HTTP error")
	errUnsupportedContentType = errors.New("unsupported content type")
	errContentTooShort        = errors.New(msgContentTooShort)
	errFeedFetchFailed        = errors.New("feed fetch failed")
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
	feedParser *gofeed.Parser
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
		feedParser: gofeed.NewParser(),
		userAgent:  userAgent,
		logger:     logger,
	}
}

// Extract fetches and extracts content from a URL.
// Fallback chain: JSON-LD → RSS/Atom → OG → Readability → raw text.
func (e *Extractor) Extract(ctx context.Context, rawURL string) (*ExtractionResult, error) {
	// Parse URL to get domain
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	// Fetch the page
	body, contentType, err := e.fetchPage(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var result *ExtractionResult

	// Check if this is an RSS/Atom feed - use feed extraction
	if isFeedContentType(contentType) {
		e.logger.Debug().
			Str(logKeyURL, rawURL).
			Str("content_type", contentType).
			Msg("Detected RSS/Atom feed, using feed extraction")

		result, err = e.buildFeedResult(parsed, body)
		if err != nil {
			e.logger.Debug().Err(err).
				Str(logKeyURL, rawURL).
				Msg("Feed extraction failed, falling back to raw text")

			result = e.buildRawTextResult(parsed, body)
		}
	} else {
		// HTML content - use Readability extraction
		article, readErr := readability.FromReader(strings.NewReader(string(body)), parsed)
		if readErr != nil {
			return nil, fmt.Errorf("readability extraction: %w", readErr)
		}

		// Use readability if it extracted content, otherwise fall back to raw text
		if article.Node != nil {
			result = e.buildResult(ctx, article, parsed, body)
		} else {
			// Fallback: extract raw text from HTML
			e.logger.Debug().
				Str(logKeyURL, rawURL).
				Str(logKeyDomain, parsed.Host).
				Msg("Readability failed, falling back to raw text extraction")

			result = e.buildRawTextResult(parsed, body)
		}
	}

	// Validate minimum content length (proposal: reject < 100 chars)
	if len(result.Content) < minContentLength {
		e.logger.Warn().
			Str(logKeyURL, rawURL).
			Str(logKeyDomain, parsed.Host).
			Int("content_len", len(result.Content)).
			Int("min_required", minContentLength).
			Msg(msgContentTooShort)

		return nil, fmt.Errorf("%w: %d chars (min %d)", errContentTooShort, len(result.Content), minContentLength)
	}

	return result, nil
}

// isFeedContentType checks if the content type indicates an RSS/Atom feed.
func isFeedContentType(contentType string) bool {
	ct := strings.ToLower(contentType)

	return strings.Contains(ct, ctApplicationRSS) ||
		strings.Contains(ct, ctApplicationAtom) ||
		strings.Contains(ct, ctApplicationXML) ||
		strings.Contains(ct, ctTextXML)
}

// fetchPage fetches a web page and returns its body and content type.
func (e *Extractor) fetchPage(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set(headerUserAgent, e.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: %d", errHTTPError, resp.StatusCode)
	}

	// Get content type
	contentType := resp.Header.Get(extractorHeaderCT)

	// Accept HTML, XHTML, and XML (for RSS/Atom feeds)
	if !isAcceptableContentType(contentType) {
		return nil, "", fmt.Errorf("%w: %s", errUnsupportedContentType, contentType)
	}

	// Read body with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentLength))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	return body, contentType, nil
}

// isAcceptableContentType checks if we can process this content type.
func isAcceptableContentType(contentType string) bool {
	ct := strings.ToLower(contentType)

	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "application/xhtml") ||
		strings.Contains(ct, ctApplicationRSS) ||
		strings.Contains(ct, ctApplicationAtom) ||
		strings.Contains(ct, ctApplicationXML) ||
		strings.Contains(ct, ctTextXML)
}

// buildResult builds an ExtractionResult from a readability article.
// Uses fallback chain: JSON-LD -> RSS/Atom -> OG meta tags -> Readability for better metadata extraction.
// Note: article.Node must not be nil - caller should check before calling.
func (e *Extractor) buildResult(ctx context.Context, article readability.Article, parsed *url.URL, body []byte) *ExtractionResult {
	htmlContent := string(body)

	// Extract JSON-LD structured data (most reliable source)
	jsonLD := extractJSONLD(htmlContent)

	// Try RSS/Atom feed metadata as second fallback (per proposal)
	feedMeta := e.extractFeedMetadataForPage(ctx, htmlContent, parsed)

	// Extract OG meta tags for better metadata (third fallback)
	ogTitleVal := extractMetaContent(htmlContent, ogTitle)
	ogDescVal := extractMetaContent(htmlContent, ogDescription)
	ogLocaleVal := extractMetaContent(htmlContent, ogLocale)
	articlePubVal := extractMetaContent(htmlContent, articlePublishedAt)

	// Extract text content using v2 API
	textContent := extractArticleText(article)

	// Build result with fallback chain: JSON-LD -> RSS/Atom -> OG -> Readability
	result := &ExtractionResult{
		Title:       coalesce(jsonLD.Headline, feedMeta.Title, ogTitleVal, article.Title()),
		Content:     truncateContent(textContent, maxExtractedLength),
		Description: truncateContent(coalesce(jsonLD.Description, feedMeta.Description, ogDescVal, article.Excerpt()), maxExcerptLength),
		Author:      coalesce(jsonLD.Author, feedMeta.Author, article.Byline()),
		Domain:      parsed.Host,
		Links:       extractLinks(htmlContent, parsed),
	}

	result.PublishedAt = parsePublishedDate(jsonLD.DatePublished, feedMeta.Published, articlePubVal, getArticlePublishedTime(article))
	result.Language = detectLanguage(jsonLD.Language, ogLocaleVal, article.Title(), textContent)

	return result
}

// feedMetadata holds metadata extracted from RSS/Atom feed for a page.
type feedMetadata struct {
	Title       string
	Description string
	Author      string
	Published   string
}

// extractFeedMetadataForPage tries to find RSS/Atom feed links in HTML and extract metadata for the current page.
// Returns empty feedMetadata if no feed found or page not in feed.
func (e *Extractor) extractFeedMetadataForPage(ctx context.Context, htmlContent string, pageURL *url.URL) feedMetadata {
	// Extract feed links from HTML <link> elements
	feedLinks := extractFeedLinks(htmlContent, pageURL)
	if len(feedLinks) == 0 {
		return feedMetadata{}
	}

	// Try each feed until we find one with the page
	pageURLStr := pageURL.String()
	pageURLNoWWW := strings.Replace(pageURLStr, "://www.", "://", 1)

	for _, feedLink := range feedLinks {
		meta := e.fetchFeedMetadataForURL(ctx, feedLink, pageURLStr, pageURLNoWWW)
		if meta.Title != "" || meta.Description != "" {
			return meta
		}
	}

	return feedMetadata{}
}

// extractFeedLinks extracts RSS/Atom feed URLs from HTML <link> elements.
func extractFeedLinks(htmlContent string, baseURL *url.URL) []string {
	var feeds []string

	// Match <link rel="alternate" type="application/rss+xml" href="...">
	// and <link rel="alternate" type="application/atom+xml" href="...">
	linkPattern := `<link[^>]+rel=["']alternate["'][^>]+type=["'](application/rss\+xml|application/atom\+xml)["'][^>]+href=["']([^"']+)["']`
	linkPatternAlt := `<link[^>]+href=["']([^"']+)["'][^>]+type=["'](application/rss\+xml|application/atom\+xml)["']`

	// Simple regex-free extraction for performance
	lowerHTML := strings.ToLower(htmlContent)
	links := extractLinkElements(lowerHTML, htmlContent)

	for _, link := range links {
		if (strings.Contains(link.linkType, "rss") || strings.Contains(link.linkType, "atom")) &&
			strings.Contains(link.rel, "alternate") {
			href := resolveLink(link.href, baseURL)
			if href != "" {
				feeds = append(feeds, href)
			}
		}
	}

	// Also check for common patterns if regex approach needed
	_ = linkPattern
	_ = linkPatternAlt

	return feeds
}

// linkElement represents a parsed <link> element.
type linkElement struct {
	rel      string
	linkType string
	href     string
}

// extractLinkElements extracts <link> elements from HTML.
// Note: lowerHTML and originalHTML may have different byte lengths due to Unicode
// case conversion, so we must validate bounds before slicing.
func extractLinkElements(lowerHTML, originalHTML string) []linkElement {
	var elements []linkElement

	originalLen := len(originalHTML)
	idx := 0

	for {
		start := strings.Index(lowerHTML[idx:], "<link")
		if start == -1 {
			break
		}

		start += idx
		end := strings.Index(lowerHTML[start:], ">")

		if end == -1 {
			break
		}

		end += start

		// Validate bounds before slicing - strings may have different lengths
		// due to Unicode case conversion (e.g., Turkish İ, German ß)
		if end+1 > originalLen {
			idx = end + 1
			continue
		}

		tagContent := originalHTML[start : end+1]
		lowerTag := lowerHTML[start : end+1]

		elem := linkElement{
			rel:      extractAttr(lowerTag, tagContent, "rel"),
			linkType: extractAttr(lowerTag, tagContent, "type"),
			href:     extractAttr(lowerTag, tagContent, "href"),
		}

		if elem.href != "" {
			elements = append(elements, elem)
		}

		idx = end + 1
	}

	return elements
}

// extractAttr extracts an attribute value from a tag.
// Note: lowerTag and originalTag may have different byte lengths due to Unicode
// case conversion, so we must validate bounds before slicing.
func extractAttr(lowerTag, originalTag, attrName string) string {
	// Find attribute in lowercase tag
	patterns := []string{attrName + `="`, attrName + `='`}
	originalLen := len(originalTag)

	for _, pattern := range patterns {
		idx := strings.Index(lowerTag, pattern)
		if idx == -1 {
			continue
		}

		start := idx + len(pattern)

		// Validate bounds before slicing
		if start >= originalLen {
			continue
		}

		quote := pattern[len(pattern)-1]
		end := strings.IndexByte(originalTag[start:], quote)

		if end == -1 {
			continue
		}

		// Validate end bounds
		if start+end > originalLen {
			continue
		}

		return originalTag[start : start+end]
	}

	return ""
}

// fetchFeedMetadataForURL fetches a feed and extracts metadata for a specific page URL.
func (e *Extractor) fetchFeedMetadataForURL(ctx context.Context, feedURL, pageURL, pageURLNoWWW string) feedMetadata {
	feed, err := e.fetchFeed(ctx, feedURL)
	if err != nil {
		return feedMetadata{}
	}

	// Find item matching the page URL
	for _, item := range feed.Items {
		if item.Link == pageURL || item.Link == pageURLNoWWW ||
			strings.TrimSuffix(item.Link, "/") == strings.TrimSuffix(pageURL, "/") {
			meta := feedMetadata{
				Title:       item.Title,
				Description: item.Description,
			}

			if len(item.Authors) > 0 {
				meta.Author = item.Authors[0].Name
			}

			if item.PublishedParsed != nil {
				meta.Published = item.PublishedParsed.Format(time.RFC3339)
			} else if item.UpdatedParsed != nil {
				meta.Published = item.UpdatedParsed.Format(time.RFC3339)
			}

			return meta
		}
	}

	return feedMetadata{}
}

// fetchFeed fetches and parses an RSS/Atom feed.
func (e *Extractor) fetchFeed(ctx context.Context, feedURL string) (*gofeed.Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create feed request: %w", err)
	}

	req.Header.Set(headerUserAgent, e.userAgent)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf(errFmtFetchFeed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errFeedFetchFailed, resp.StatusCode)
	}

	feed, err := e.feedParser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(errFmtParseFeed, err)
	}

	return feed, nil
}

// extractArticleText extracts text content from a readability Article using v2 API.
func extractArticleText(article readability.Article) string {
	var buf bytes.Buffer
	if err := article.RenderText(&buf); err != nil {
		return ""
	}

	return buf.String()
}

// getArticlePublishedTime extracts published time from readability Article, returning nil on error.
func getArticlePublishedTime(article readability.Article) *time.Time {
	t, err := article.PublishedTime()
	if err != nil {
		return nil
	}

	return &t
}

// / parsePublishedDate extracts published date using fallback chain: JSON-LD -> RSS/Atom -> meta tag -> readability.
func parsePublishedDate(jsonLDDate, feedDate, metaDate string, readabilityTime *time.Time) time.Time {
	if jsonLDDate != "" {
		if t, err := time.Parse(time.RFC3339, jsonLDDate); err == nil {
			return t
		}
	}

	if feedDate != "" {
		if t, err := time.Parse(time.RFC3339, feedDate); err == nil {
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

// buildRawTextResult extracts content from HTML when Readability fails.
// This is the last fallback in the extraction chain.
func (e *Extractor) buildRawTextResult(parsed *url.URL, body []byte) *ExtractionResult {
	htmlContent := string(body)

	// Extract JSON-LD and OG data (same as buildResult)
	jsonLD := extractJSONLD(htmlContent)
	ogTitleVal := extractMetaContent(htmlContent, ogTitle)
	ogDescVal := extractMetaContent(htmlContent, ogDescription)
	ogLocaleVal := extractMetaContent(htmlContent, ogLocale)
	articlePubVal := extractMetaContent(htmlContent, articlePublishedAt)

	// Extract title from HTML <title> tag if not in JSON-LD or OG
	htmlTitle := extractHTMLTitle(htmlContent)

	// Extract raw text by stripping HTML tags
	rawText := extractRawText(htmlContent)

	result := &ExtractionResult{
		Title:       coalesce(jsonLD.Headline, ogTitleVal, htmlTitle),
		Content:     truncateContent(rawText, maxExtractedLength),
		Description: truncateContent(coalesce(jsonLD.Description, ogDescVal), maxExcerptLength),
		Author:      jsonLD.Author,
		Domain:      parsed.Host,
		Links:       extractLinks(htmlContent, parsed),
	}

	result.PublishedAt = parsePublishedDate(jsonLD.DatePublished, "", articlePubVal, nil)
	result.Language = detectLanguage(jsonLD.Language, ogLocaleVal, result.Title, rawText)

	return result
}

// buildFeedResult extracts content from an RSS/Atom feed.
// If the feed has items, extracts the first item's content.
// Otherwise, uses the feed's title and description.
func (e *Extractor) buildFeedResult(parsed *url.URL, body []byte) (*ExtractionResult, error) {
	feed, err := e.feedParser.ParseString(string(body))
	if err != nil {
		return nil, fmt.Errorf(errFmtParseFeed, err)
	}

	result := &ExtractionResult{
		Domain: parsed.Host,
	}

	// If feed has items, use the first item for content
	if len(feed.Items) > 0 {
		item := feed.Items[0]
		result.Title = item.Title
		result.Description = truncateContent(item.Description, maxExcerptLength)

		// Use item content if available, otherwise description
		content := item.Content
		if content == "" {
			content = item.Description
		}

		// Strip HTML from content
		result.Content = truncateContent(extractRawText(content), maxExtractedLength)

		// Extract author
		if len(item.Authors) > 0 {
			result.Author = item.Authors[0].Name
		}

		// Extract published date
		if item.PublishedParsed != nil {
			result.PublishedAt = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			result.PublishedAt = *item.UpdatedParsed
		}

		// Extract links from item
		result.Links = []string{item.Link}
	} else {
		// No items - use feed metadata
		result.Title = feed.Title
		result.Description = truncateContent(feed.Description, maxExcerptLength)
		result.Content = truncateContent(extractRawText(feed.Description), maxExtractedLength)

		if len(feed.Authors) > 0 {
			result.Author = feed.Authors[0].Name
		}

		if feed.PublishedParsed != nil {
			result.PublishedAt = *feed.PublishedParsed
		} else if feed.UpdatedParsed != nil {
			result.PublishedAt = *feed.UpdatedParsed
		}
	}

	// Detect language from content
	result.Language = links.DetectLanguage(result.Title + " " + result.Content)

	return result, nil
}

// extractHTMLTitle extracts the content of the <title> tag.
// Uses consistent lowercase string to avoid index mismatches with multi-byte characters.
func extractHTMLTitle(html string) string {
	const (
		titleStart = "<title>"
		titleEnd   = "</title>"
	)

	lowerHTML := strings.ToLower(html)

	startIdx := strings.Index(lowerHTML, titleStart)
	if startIdx == -1 {
		return ""
	}

	startIdx += len(titleStart)

	// Validate bounds before slicing
	if startIdx >= len(html) {
		return ""
	}

	endIdx := strings.Index(lowerHTML[startIdx:], titleEnd)
	if endIdx == -1 {
		return ""
	}

	// Validate end bounds
	if startIdx+endIdx > len(html) {
		return ""
	}

	return strings.TrimSpace(html[startIdx : startIdx+endIdx])
}

// extractRawText removes HTML tags and extracts visible text content.
// Removes script, style, and other non-visible elements first.
func extractRawText(html string) string {
	// Remove script and style blocks
	html = removeTagBlock(html, "script")
	html = removeTagBlock(html, "style")
	html = removeTagBlock(html, "noscript")
	html = removeTagBlock(html, "nav")
	html = removeTagBlock(html, "header")
	html = removeTagBlock(html, "footer")
	html = removeTagBlock(html, "aside")

	// Remove all remaining HTML tags
	var result strings.Builder

	inTag := false

	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false

			result.WriteRune(' ') // Replace tag with space
		case !inTag:
			result.WriteRune(r)
		}
	}

	// Normalize whitespace
	return normalizeWhitespace(result.String())
}

// removeTagBlock removes all content between <tag> and </tag> including nested tags.
func removeTagBlock(html, tag string) string {
	startTag := "<" + tag
	endTag := "</" + tag + ">"

	result := html

	for {
		lowerResult := strings.ToLower(result)
		startIdx := strings.Index(lowerResult, startTag)

		if startIdx == -1 {
			break
		}

		// Validate bounds before slicing
		if startIdx >= len(result) {
			break
		}

		// Find matching end tag
		endIdx := strings.Index(lowerResult[startIdx:], endTag)
		if endIdx == -1 {
			// No closing tag, remove to end of string
			result = result[:startIdx]

			break
		}

		// Validate end bounds
		endPos := startIdx + endIdx + len(endTag)
		if endPos > len(result) {
			result = result[:startIdx]

			break
		}

		result = result[:startIdx] + result[endPos:]
	}

	return result
}

// normalizeWhitespace collapses multiple whitespace characters into single spaces.
func normalizeWhitespace(s string) string {
	var result strings.Builder

	prevWasSpace := true // Start true to trim leading whitespace

	for _, r := range s {
		isSpace := r == ' ' || r == '\t' || r == '\n' || r == '\r'
		if isSpace {
			if !prevWasSpace {
				result.WriteRune(' ')
			}

			prevWasSpace = true
		} else {
			result.WriteRune(r)

			prevWasSpace = false
		}
	}

	return strings.TrimSpace(result.String())
}
