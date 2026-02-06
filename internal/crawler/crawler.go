// Package crawler implements a web crawler for external link content extraction.
//
// The crawler uses Solr as a work queue, fetching URLs that need to be crawled
// and storing extracted content back. Features include:
//   - Rate limiting to avoid overwhelming target sites
//   - Automatic sitemap discovery and seeding
//   - Content extraction (title, description, text)
//   - Retry handling for transient failures
package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
)

// Crawler errors.
var errCrawlerStopped = errors.New("crawler stopped")

const (
	seedQueueInterval   = 10 * time.Minute
	discoveryInterval   = 5 * time.Minute
	batchProcessTimeout = 30 * time.Second
	fieldCount          = "count"
	fieldSitemap        = "sitemap"
	fieldURL            = "url"
	fieldRetries        = "retries"
	maxErrorMsgLen      = 500
	maxCrawlRetries     = 3 // Max retries before permanent error status
)

// Crawler is a web crawler that uses Solr as a work queue.
type Crawler struct {
	cfg        *Config
	client     *solr.Client
	limiter    *rate.Limiter
	extractor  *Extractor
	discovery  *Discovery
	logger     *zerolog.Logger
	seeds      []string
	lastSeeded time.Time
	podName    string
}

// New creates a new Crawler.
func New(cfg *Config, logger *zerolog.Logger) (*Crawler, error) {
	client := solr.New(solr.Config{
		BaseURL:    cfg.SolrURL,
		Timeout:    cfg.SolrTimeout,
		MaxResults: cfg.CrawlBatchSize,
	})

	// Load seed URLs
	seeds, err := cfg.LoadSeeds()
	if err != nil {
		return nil, fmt.Errorf("load seeds: %w", err)
	}

	// Get pod name for claim tracking
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podName = fmt.Sprintf("crawler-%d", os.Getpid())
	}

	logger.Info().
		Int(fieldCount, len(seeds)).
		Str("pod_name", podName).
		Msg("Loaded seed URLs")

	return &Crawler{
		cfg:       cfg,
		client:    client,
		limiter:   rate.NewLimiter(rate.Limit(cfg.CrawlRateLimitRPS), 1),
		extractor: NewExtractor(cfg.CrawlUserAgent, logger),
		discovery: NewDiscovery(cfg.CrawlUserAgent, logger),
		logger:    logger,
		seeds:     seeds,
		podName:   podName,
	}, nil
}

// Run starts the crawler main loop.
func (c *Crawler) Run(ctx context.Context) error {
	c.logger.Info().
		Str("solr_url", c.cfg.SolrURL).
		Float64("rate_limit_rps", c.cfg.CrawlRateLimitRPS).
		Int("batch_size", c.cfg.CrawlBatchSize).
		Msg("Starting crawler")

	// Seed the queue on startup
	c.seedQueue(ctx)

	ticker := time.NewTicker(batchProcessTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("Crawler shutting down")
			return fmt.Errorf("%w: %w", errCrawlerStopped, ctx.Err())
		case <-ticker.C:
			c.processNextBatch(ctx)
			c.maybeReseed(ctx)
		}
	}
}

// seedQueue adds seed URLs to the crawl queue.
func (c *Crawler) seedQueue(ctx context.Context) {
	if len(c.seeds) == 0 {
		return
	}

	c.logger.Info().Int(fieldCount, len(c.seeds)).Msg("Seeding crawl queue")

	for _, seedURL := range c.seeds {
		if err := c.enqueueURL(ctx, seedURL, 0); err != nil {
			c.logger.Warn().Err(err).Str(fieldURL, seedURL).Msg("Failed to enqueue seed URL")
		}
	}

	c.lastSeeded = time.Now()
}

// maybeReseed re-seeds the queue periodically.
func (c *Crawler) maybeReseed(ctx context.Context) {
	if time.Since(c.lastSeeded) < seedQueueInterval {
		return
	}

	// Check if queue is empty or nearly empty
	resp, err := c.client.Search(ctx, "*:*",
		solr.WithFilterQuery("crawl_status:pending"),
		solr.WithRows(0),
	)
	if err != nil {
		c.logger.Warn().Err(err).Msg("Failed to check queue status")
		return
	}

	if resp.Response.NumFound < c.cfg.CrawlBatchSize {
		c.seedQueue(ctx)
	}
}

// processNextBatch processes the next batch of URLs from the queue.
func (c *Crawler) processNextBatch(ctx context.Context) {
	// Update queue metrics
	if stats, err := c.GetQueueStats(ctx); err == nil {
		UpdateQueueMetrics(stats)
	}

	urls, err := c.claimURLs(ctx, c.cfg.CrawlBatchSize)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to claim URLs")
		return
	}

	if len(urls) == 0 {
		c.logger.Debug().Msg("No URLs to process")
		return
	}

	c.logger.Debug().Int(fieldCount, len(urls)).Msg("Processing batch")

	for _, doc := range urls {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Rate limit
		if err := c.limiter.Wait(ctx); err != nil {
			return
		}

		c.processURL(ctx, doc)
	}
}

// processURL crawls a single URL.
// Includes panic recovery to prevent individual URL failures from crashing the pod.
func (c *Crawler) processURL(ctx context.Context, doc *solr.Document) {
	// Recover from panics to prevent pod crash on malformed HTML
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error().
				Interface("panic", r).
				Str(fieldURL, doc.URL).
				Msg("Recovered from panic during URL processing")
			IncrementExtractionErrors()
		}
	}()

	// Skip documents with empty URLs (likely data corruption or indexing bug)
	if doc.URL == "" {
		c.logger.Warn().Str(fieldDocID, doc.ID).Msg("Skipping document with empty URL")
		c.markError(ctx, doc.ID, "empty URL")

		return
	}

	// Verify claim is still valid before expensive extraction work.
	// Due to Solr replication lag, another pod might have claimed this URL
	// after our ConditionalUpdate appeared to succeed on a stale replica.
	current, err := c.client.Get(ctx, doc.ID)
	if err != nil {
		c.logger.Warn().Err(err).Str(fieldDocID, doc.ID).Msg("Failed to verify claim")
		return
	}

	if current.CrawlClaimedBy != c.podName {
		c.logger.Debug().
			Str(fieldDocID, doc.ID).
			Str("claimed_by", current.CrawlClaimedBy).
			Str("our_pod", c.podName).
			Msg("Claim lost to another pod, skipping")

		return
	}

	c.logger.Debug().Str(fieldURL, doc.URL).Int("depth", doc.CrawlDepth).Msg("Processing URL")

	IncrementProcessed()

	// Extract content
	result, err := c.extractor.Extract(ctx, doc.URL)
	if err != nil {
		c.logger.Warn().Err(err).Str(fieldURL, doc.URL).Msg("Extraction failed")
		c.handleExtractionError(ctx, doc, err)
		IncrementExtractionErrors()

		return
	}

	// Update document with extracted content
	if err := c.updateWithContent(ctx, doc.ID, result); err != nil {
		c.logger.Warn().Err(err).Str(fieldURL, doc.URL).Msg("Failed to update document")
		return
	}

	// Discover new URLs if not at max depth
	if doc.CrawlDepth < c.cfg.CrawlDepth {
		c.discoverURLs(ctx, doc.URL, result.Links, doc.CrawlDepth+1)
	}
}

// discoverURLs enqueues discovered URLs.
// Order per proposal: RSS/Sitemap first (more structured), then link crawling (fallback).
func (c *Crawler) discoverURLs(ctx context.Context, sourceURL string, links []string, depth int) {
	// 1. Try RSS/Atom feeds first (most structured, efficient discovery)
	feeds, sitemaps := c.discovery.DiscoverFeeds(ctx, sourceURL)
	c.processFeedURLs(ctx, feeds, depth)

	// 2. Then try sitemaps (structured but may include non-article URLs)
	c.processSitemapURLs(ctx, sitemaps, depth)

	// 3. Finally fall back to link crawling (least structured, may include noise)
	// Only follow same-domain links per proposal to prevent crawler drift
	c.enqueueLinks(ctx, sourceURL, links, depth)
}

// enqueueLinks enqueues a list of links, filtering to same-domain only.
// Per proposal: "only follow same-domain links" to prevent off-site drift.
func (c *Crawler) enqueueLinks(ctx context.Context, sourceURL string, links []string, depth int) {
	sourceDomain := extractDomain(sourceURL)
	if sourceDomain == "" {
		return
	}

	for _, link := range links {
		if !isValidCrawlURL(link) {
			continue
		}

		// Only follow same-domain links to prevent crawler drift
		linkDomain := extractDomain(link)
		if !isSameDomain(sourceDomain, linkDomain) {
			continue
		}

		if err := c.enqueueURL(ctx, link, depth); err != nil {
			// Log but don't fail - duplicates are expected
			c.logger.Debug().Err(err).Str(fieldURL, link).Msg("Failed to enqueue discovered URL")
		}
	}
}

// extractDomain extracts the domain from a URL, normalizing www prefix.
func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return normalizeDomain(parsed.Host)
}

// normalizeDomain removes www. prefix for consistent comparison.
func normalizeDomain(domain string) string {
	domain = strings.ToLower(domain)
	domain = strings.TrimPrefix(domain, "www.")

	return domain
}

// isSameDomain checks if two domains are the same (ignoring www prefix).
func isSameDomain(domain1, domain2 string) bool {
	return domain1 != "" && domain2 != "" && domain1 == domain2
}

// processFeedURLs fetches and enqueues entries from feed URLs.
func (c *Crawler) processFeedURLs(ctx context.Context, feeds []string, depth int) {
	for _, feedURL := range feeds {
		entries, err := c.discovery.FetchFeed(ctx, feedURL)
		if err != nil {
			c.logger.Debug().Err(err).Str("feed", feedURL).Msg("Failed to fetch feed")
			continue
		}

		for _, entry := range entries {
			if err := c.enqueueURL(ctx, entry, depth); err != nil {
				c.logger.Debug().Err(err).Str(fieldURL, entry).Msg("Failed to enqueue feed entry")
			}
		}
	}
}

// processSitemapURLs fetches and enqueues entries from sitemap URLs.
func (c *Crawler) processSitemapURLs(ctx context.Context, sitemaps []string, depth int) {
	for _, sitemapURL := range sitemaps {
		entries, err := c.discovery.FetchSitemap(ctx, sitemapURL)
		if err != nil {
			c.logger.Debug().Err(err).Str(fieldSitemap, sitemapURL).Msg("Failed to fetch sitemap")
			continue
		}

		for _, entry := range entries {
			if err := c.enqueueURL(ctx, entry, depth); err != nil {
				c.logger.Debug().Err(err).Str(fieldURL, entry).Msg("Failed to enqueue sitemap entry")
			}
		}
	}
}

// updateWithContent updates a document with extracted content.
func (c *Crawler) updateWithContent(ctx context.Context, docID string, result *ExtractionResult) error {
	fields := map[string]interface{}{
		"title":        result.Title,
		"content":      result.Content,
		"description":  result.Description,
		"language":     result.Language,
		"domain":       result.Domain,
		"crawl_status": solr.CrawlStatusDone,
		"crawled_at":   time.Now().UTC().Format(time.RFC3339),
	}

	if !result.PublishedAt.IsZero() {
		// Convert to UTC and format for Solr compatibility
		fields["published_at"] = result.PublishedAt.UTC().Format(time.RFC3339)
	}

	// Add language-specific fields (only for languages supported in Solr schema)
	switch result.Language {
	case "en":
		fields["title_en"] = result.Title
		fields["content_en"] = result.Content
	case "ru":
		fields["title_ru"] = result.Title
		fields["content_ru"] = result.Content
	case "el":
		fields["title_el"] = result.Title
		fields["content_el"] = result.Content
	}

	if err := c.client.AtomicUpdateWithRetry(ctx, docID, fields, solr.DefaultRetryConfig()); err != nil {
		return fmt.Errorf("update document content: %w", err)
	}

	return nil
}

// handleExtractionError handles extraction failures with retry logic.
// If retries < maxCrawlRetries, increments retry count and resets to pending.
// After maxCrawlRetries, marks the document with permanent error status.
func (c *Crawler) handleExtractionError(ctx context.Context, doc *solr.Document, extractionErr error) {
	errMsg := extractionErr.Error()
	if len(errMsg) > maxErrorMsgLen {
		errMsg = errMsg[:maxErrorMsgLen]
	}

	newRetries := doc.CrawlRetries + 1

	if newRetries >= maxCrawlRetries {
		// Max retries reached - mark as permanent error
		c.logger.Warn().
			Str(fieldURL, doc.URL).
			Int(fieldRetries, newRetries).
			Str("error", errMsg).
			Msg("Max retries reached, marking as error")
		c.markError(ctx, doc.ID, errMsg)

		return
	}

	// Still have retries left - increment and reset to pending
	c.logger.Info().
		Str(fieldURL, doc.URL).
		Int(fieldRetries, newRetries).
		Int("max_retries", maxCrawlRetries).
		Msg("Extraction failed, will retry later")

	fields := map[string]interface{}{
		"crawl_status":     solr.CrawlStatusPending,
		"crawl_retries":    newRetries,
		"crawl_error":      errMsg,
		"crawl_claimed_at": nil, // Clear claim fields for cleaner diagnostics
		"crawl_claimed_by": nil,
	}

	if err := c.client.AtomicUpdateWithRetry(ctx, doc.ID, fields, solr.DefaultRetryConfig()); err != nil {
		c.logger.Warn().Err(err).Str(fieldDocID, doc.ID).Msg("Failed to update retry count")
	}
}

// markError marks a document as having a permanent error.
func (c *Crawler) markError(ctx context.Context, docID, errMsg string) {
	if len(errMsg) > maxErrorMsgLen {
		errMsg = errMsg[:maxErrorMsgLen]
	}

	fields := map[string]interface{}{
		"crawl_status": solr.CrawlStatusError,
		"crawl_error":  errMsg,
		"crawled_at":   time.Now().UTC().Format(time.RFC3339),
	}

	if err := c.client.AtomicUpdateWithRetry(ctx, docID, fields, solr.DefaultRetryConfig()); err != nil {
		c.logger.Warn().Err(err).Str("doc_id", docID).Msg("Failed to mark document as error")
	}
}

// isValidCrawlURL checks if a URL should be crawled.
func isValidCrawlURL(rawURL string) bool {
	// Basic validation
	if len(rawURL) < 8 {
		return false
	}

	// Must be HTTP(S)
	if rawURL[:7] != "http://" && rawURL[:8] != "https://" {
		return false
	}

	// Check all skip patterns
	if matchesSkipPattern(rawURL) {
		return false
	}

	// Check file extensions
	if hasSkipSuffix(rawURL) {
		return false
	}

	return true
}

// matchesSkipPattern checks if URL matches any skip pattern (social share, auth, API, etc).
func matchesSkipPattern(rawURL string) bool {
	skipPatterns := []string{
		// Social share URLs
		"twitter.com/share", "twitter.com/intent/", "x.com/share", "x.com/intent/",
		"facebook.com/sharer", "facebook.com/share.php",
		"pinterest.com/pin/create", "reddit.com/submit",
		"linkedin.com/shareArticle", "linkedin.com/cws/share",
		"telegram.me/share", "t.me/share", "bsky.app/intent/",
		"api.whatsapp.com/send", "wa.me/", "mailto:",
		"vk.com/share.php", "tumblr.com/share", "getpocket.com/save", "share.flipboard.com",
		// Auth/login pages
		"/login", "/signin", "/signup", "/register", "/auth/", "/oauth/", "/cas/login",
		// API endpoints
		"/wp-json/", "/graphql", "/.well-known/",
		// Tracking and ads
		"/track/", "/pixel/", "/beacon/",
		"doubleclick.net", "googlesyndication.com", "googleadservices.com",
		// Print/email versions
		"/print/", "?print=", "&print=", "/email/", "?email=",
		// Non-content URL patterns
		"/ajax/", "/api/", "/_next/static/", "/static/css/", "/static/js/",
		"/wp-content/uploads/", "/wp-includes/",
		"/feed/", "/feed", "/rss", "xmlrpc.php",
		"%7B%7B", "{{", "#",
		"?replytocom=", "?share=", "?action=", "?utm_", "&utm_",
		// Search and category pages (low content value)
		"/search", "/search/", "?q=", "?s=", "/tag/", "/tags/", "/category/",
	}

	for _, pattern := range skipPatterns {
		if containsPattern(rawURL, pattern) {
			return true
		}
	}

	return false
}

// hasSkipSuffix checks if URL path ends with a non-content file extension.
// Handles URLs with query parameters (e.g., /style.css?v=123).
func hasSkipSuffix(rawURL string) bool {
	skipSuffixes := []string{
		// Media
		".pdf", ".zip", ".exe", ".dmg", ".mp3", ".mp4", ".avi", ".mov", ".webm", ".flv",
		".rar", ".tar", ".gz", ".7z", ".iso", ".bin", ".apk", ".deb", ".rpm",
		// Images
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".bmp", ".tiff",
		// Web assets
		".css", ".js", ".woff", ".woff2", ".ttf", ".eot", ".map", ".webmanifest",
		// Data
		".json", ".xml", ".rss", ".atom", ".csv", ".tsv", ".xls", ".xlsx",
		// Documents (non-HTML)
		".doc", ".docx", ".ppt", ".pptx", ".odt", ".ods", ".odp",
	}

	// Extract path without query string and fragment
	path := rawURL
	if idx := strings.Index(rawURL, "?"); idx != -1 {
		path = rawURL[:idx]
	}

	if idx := strings.Index(path, "#"); idx != -1 {
		path = path[:idx]
	}

	for _, suffix := range skipSuffixes {
		if len(path) > len(suffix) && path[len(path)-len(suffix):] == suffix {
			return true
		}
	}

	return false
}

// containsPattern checks if a URL contains a pattern.
func containsPattern(url, pattern string) bool {
	for i := 0; i <= len(url)-len(pattern); i++ {
		if url[i:i+len(pattern)] == pattern {
			return true
		}
	}

	return false
}
