package crawler

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog"
)

const (
	discoveryTimeout  = 15 * time.Second
	maxFeedEntries    = 50
	maxSitemapURLs    = 100
	maxBodySize       = 10 * 1024 * 1024 // 10MB
	headerUserAgent   = "User-Agent"
	headerContentType = "Content-Type"
	wrapCreateRequest = "create request: %w"
	wrapReadBody      = "read body: %w"
	wrapHTTPStatusFmt = "%w: status %d"
	httpErrorMsg      = "HTTP error"

	// Feed type keywords.
	feedTypeRSS  = "rss"
	feedTypeAtom = "atom"
)

// Discovery errors.
var errDiscoveryHTTPError = errors.New(httpErrorMsg)

// Discovery handles RSS/Atom feed and sitemap discovery.
type Discovery struct {
	httpClient *http.Client
	feedParser *gofeed.Parser
	userAgent  string
	logger     *zerolog.Logger
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(userAgent string, logger *zerolog.Logger) *Discovery {
	return &Discovery{
		httpClient: &http.Client{
			Timeout: discoveryTimeout,
		},
		feedParser: gofeed.NewParser(),
		userAgent:  userAgent,
		logger:     logger,
	}
}

// DiscoverFeeds attempts to discover RSS/Atom feeds and sitemaps for a domain.
func (d *Discovery) DiscoverFeeds(ctx context.Context, sourceURL string) (feeds, sitemaps []string) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return nil, nil
	}

	baseURL := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	// Common feed locations
	feedPaths := []string{
		"/feed",
		"/feed.xml",
		"/rss",
		"/rss.xml",
		"/atom.xml",
		"/index.xml",
		"/feed/atom",
		"/feed/rss",
	}

	for _, path := range feedPaths {
		feedURL := baseURL + path
		if d.isFeed(ctx, feedURL) {
			feeds = append(feeds, feedURL)
		}
	}

	// Common sitemap locations
	sitemapPaths := []string{
		"/sitemap.xml",
		"/sitemap_index.xml",
		"/sitemap-index.xml",
		"/news-sitemap.xml",
	}

	for _, path := range sitemapPaths {
		sitemapURL := baseURL + path
		if d.isSitemap(ctx, sitemapURL) {
			sitemaps = append(sitemaps, sitemapURL)
		}
	}

	return feeds, sitemaps
}

// isFeed checks if a URL is a valid RSS/Atom feed.
func (d *Discovery) isFeed(ctx context.Context, feedURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, feedURL, nil)
	if err != nil {
		return false
	}

	req.Header.Set(headerUserAgent, d.userAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return false
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	contentType := resp.Header.Get(headerContentType)

	return strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, feedTypeRSS) ||
		strings.Contains(contentType, feedTypeAtom)
}

// isSitemap checks if a URL is a valid sitemap.
func (d *Discovery) isSitemap(ctx context.Context, sitemapURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, sitemapURL, nil)
	if err != nil {
		return false
	}

	req.Header.Set(headerUserAgent, d.userAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return false
	}

	_ = resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// FetchFeed fetches and parses an RSS/Atom feed, returning entry URLs.
func (d *Discovery) FetchFeed(ctx context.Context, feedURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf(wrapCreateRequest, err)
	}

	req.Header.Set(headerUserAgent, d.userAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf(errFmtFetchFeed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(wrapHTTPStatusFmt, errDiscoveryHTTPError, resp.StatusCode)
	}

	feed, err := d.feedParser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(errFmtParseFeed, err)
	}

	var urls []string

	for i, item := range feed.Items {
		if i >= maxFeedEntries {
			break
		}

		if item.Link != "" {
			urls = append(urls, item.Link)
		}
	}

	return urls, nil
}

// FetchSitemap fetches and parses a sitemap, returning URLs.
func (d *Discovery) FetchSitemap(ctx context.Context, sitemapURL string) ([]string, error) {
	body, err := d.fetchSitemapBody(ctx, sitemapURL)
	if err != nil {
		return nil, err
	}

	// Try to parse as sitemap index first
	var sitemapIndex SitemapIndex
	if err := xml.Unmarshal(body, &sitemapIndex); err == nil && len(sitemapIndex.Sitemaps) > 0 {
		return d.fetchSitemapIndex(ctx, sitemapIndex)
	}

	return d.parseSitemapURLs(body)
}

// fetchSitemapBody fetches the raw sitemap body from a URL.
func (d *Discovery) fetchSitemapBody(ctx context.Context, sitemapURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, fmt.Errorf(wrapCreateRequest, err)
	}

	req.Header.Set(headerUserAgent, d.userAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch sitemap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(wrapHTTPStatusFmt, errDiscoveryHTTPError, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf(wrapReadBody, err)
	}

	return body, nil
}

// parseSitemapURLs parses a sitemap XML and extracts URLs.
func (d *Discovery) parseSitemapURLs(body []byte) ([]string, error) {
	var sitemap Sitemap
	if err := xml.Unmarshal(body, &sitemap); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}

	var urls []string

	for i, u := range sitemap.URLs {
		if i >= maxSitemapURLs {
			break
		}

		if u.Loc != "" {
			urls = append(urls, u.Loc)
		}
	}

	return urls, nil
}

// fetchSitemapIndex fetches URLs from all sitemaps in a sitemap index.
func (d *Discovery) fetchSitemapIndex(ctx context.Context, index SitemapIndex) ([]string, error) {
	var allURLs []string

	for _, sm := range index.Sitemaps {
		if len(allURLs) >= maxSitemapURLs {
			break
		}

		urls, err := d.FetchSitemap(ctx, sm.Loc)
		if err != nil {
			d.logger.Debug().Err(err).Str("sitemap", sm.Loc).Msg("Failed to fetch sitemap from index")
			continue
		}

		remaining := maxSitemapURLs - len(allURLs)
		if len(urls) > remaining {
			urls = urls[:remaining]
		}

		allURLs = append(allURLs, urls...)
	}

	return allURLs, nil
}

// Sitemap represents a sitemap XML structure.
type Sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []SitemapURL `xml:"url"`
}

// SitemapURL represents a URL entry in a sitemap.
type SitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

// SitemapIndex represents a sitemap index XML structure.
type SitemapIndex struct {
	XMLName  xml.Name            `xml:"sitemapindex"`
	Sitemaps []SitemapIndexEntry `xml:"sitemap"`
}

// SitemapIndexEntry represents a sitemap entry in a sitemap index.
type SitemapIndexEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}
