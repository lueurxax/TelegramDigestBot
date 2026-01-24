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
	hrefPrefixLen      = 6 // len(`href="`)
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
func (e *Extractor) buildResult(article readability.Article, parsed *url.URL, body []byte) *ExtractionResult {
	// Extract links from the page
	extractedLinks := extractLinks(string(body), parsed)

	// Build result
	result := &ExtractionResult{
		Title:       article.Title,
		Content:     truncateContent(article.TextContent, maxExtractedLength),
		Description: truncateContent(article.Excerpt, maxExcerptLength),
		Author:      article.Byline,
		Domain:      parsed.Host,
		Links:       extractedLinks,
	}

	// Parse published date if available
	if article.PublishedTime != nil {
		result.PublishedAt = *article.PublishedTime
	}

	// Detect language from content
	textForLang := article.Title + " " + article.TextContent
	if len(textForLang) > 1000 {
		textForLang = textForLang[:1000]
	}

	result.Language = links.DetectLanguage(textForLang)

	return result
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
