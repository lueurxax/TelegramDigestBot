package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
)

// Queue constants.
const (
	allDocsQuery    = "*:*"
	filterPending   = "crawl_status:pending"
	filterSourceWeb = "source:web"
	fieldDocID      = "doc_id"
	schemeHTTP      = "http"
	schemeHTTPS     = "https"
	claimMultiplier = 2
	wrapErrFmt      = "%w: %s"
)

// Queue errors.
var errUnsupportedScheme = errors.New("unsupported URL scheme")

// claimURLs claims URLs from the queue for processing.
// It searches for pending URLs OR stale claims (crawler died mid-processing).
// Uses optimistic locking via Solr's _version_ field to prevent races.
func (c *Crawler) claimURLs(ctx context.Context, count int) ([]*solr.Document, error) {
	now := time.Now().UTC()
	staleThreshold := now.Add(-c.cfg.CrawlClaimTTL)

	// Build filter: pending URLs OR stale processing claims
	// Stale claims are URLs that were claimed but not completed within TTL
	staleFilter := fmt.Sprintf(
		"crawl_status:(pending OR (processing AND crawl_claimed_at:[* TO %s]))",
		staleThreshold.Format(time.RFC3339),
	)

	resp, err := c.client.Search(ctx, allDocsQuery,
		solr.WithFilterQuery(staleFilter),
		solr.WithFilterQuery(filterSourceWeb),
		solr.WithRows(count*claimMultiplier),     // Fetch more to account for claim failures
		solr.WithSort("crawl_depth asc, id asc"), // Breadth-first crawling
		solr.WithFields("id,url,domain,crawl_depth,crawl_status,crawl_retries,_version_"),
	)
	if err != nil {
		return nil, fmt.Errorf("search pending URLs: %w", err)
	}

	if len(resp.Response.Docs) == 0 {
		return nil, nil
	}

	var claimed []*solr.Document

	for _, doc := range resp.Response.Docs {
		if len(claimed) >= count {
			break
		}

		// Try to claim with optimistic locking
		// Uses _version_ field - if another crawler modified the doc, version changed and this fails with 409
		err := c.client.ConditionalUpdate(ctx, doc.ID, doc.Version, map[string]interface{}{
			"crawl_status":     solr.CrawlStatusProcessing,
			"crawl_claimed_at": now.Format(time.RFC3339),
			"crawl_claimed_by": c.podName,
		})
		if err != nil {
			if errors.Is(err, solr.ErrVersionConflict) {
				// Another worker claimed it, try next
				c.logger.Debug().Str(fieldDocID, doc.ID).Msg("Claim conflict, skipping")
				continue
			}

			c.logger.Warn().Err(err).Str(fieldDocID, doc.ID).Msg("Failed to claim URL")

			continue
		}

		docCopy := doc
		claimed = append(claimed, &docCopy)
	}

	return claimed, nil
}

// enqueueURL adds a URL to the crawl queue if it doesn't already exist.
func (c *Crawler) enqueueURL(ctx context.Context, rawURL string, depth int) error {
	// Validate and normalize URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS {
		return fmt.Errorf(wrapErrFmt, errUnsupportedScheme, parsed.Scheme)
	}

	canonicalURL := solr.CanonicalizeURL(rawURL)
	docID := solr.WebDocID(canonicalURL)

	// Check if already exists
	existing, err := c.client.Get(ctx, docID)
	if err != nil && !errors.Is(err, solr.ErrNotFound) {
		return fmt.Errorf("check existing: %w", err)
	}

	if existing != nil {
		// URL already in queue or processed
		return nil
	}

	// Add to queue (crawled_at is set when actually crawled, not when enqueued)
	doc := solr.NewIndexDocument(docID).
		SetField("source", solr.SourceWeb).
		SetField(fieldURL, rawURL).
		SetField("url_canonical", canonicalURL).
		SetField(logKeyDomain, parsed.Host).
		SetField("crawl_status", solr.CrawlStatusPending).
		SetField("crawl_depth", depth).
		SetField("indexed_at", time.Now().UTC().Format(time.RFC3339))

	if err := c.client.Index(ctx, doc); err != nil {
		return fmt.Errorf("index document: %w", err)
	}

	return nil
}

// GetQueueStats returns statistics about the crawl queue.
func (c *Crawler) GetQueueStats(ctx context.Context) (map[string]int, error) {
	resp, err := c.client.Search(ctx, allDocsQuery,
		solr.WithFilterQuery(filterSourceWeb),
		solr.WithRows(0),
	)
	if err != nil {
		return nil, fmt.Errorf("search queue stats: %w", err)
	}

	stats := map[string]int{
		"total": resp.Response.NumFound,
	}

	// Get counts by status
	for _, status := range []string{solr.CrawlStatusPending, solr.CrawlStatusProcessing, solr.CrawlStatusDone, solr.CrawlStatusError} {
		statusResp, err := c.client.Search(ctx, allDocsQuery,
			solr.WithFilterQuery(filterSourceWeb),
			solr.WithFilterQuery(fmt.Sprintf("crawl_status:%s", status)),
			solr.WithRows(0),
		)
		if err != nil {
			continue
		}

		stats[status] = statusResp.Response.NumFound
	}

	return stats, nil
}
