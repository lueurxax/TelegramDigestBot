package linkseeder

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

// Skip reasons for metrics.
const (
	SkipReasonDisabled        = "disabled"
	SkipReasonInvalidScheme   = "invalid_scheme"
	SkipReasonTelegramDomain  = "telegram_domain"
	SkipReasonDeniedExtension = "denied_extension"
	SkipReasonDeniedDomain    = "denied_domain"
	SkipReasonNotAllowed      = "not_in_allowlist"
	SkipReasonQueueFull       = "queue_full"
	SkipReasonDuplicate       = "duplicate"
	SkipReasonMaxLinks        = "max_links_exceeded"
)

// Log field names.
const (
	logFieldURL     = "url"
	logFieldPeerID  = "peer_id"
	logFieldMsgID   = "msg_id"
	logFieldChannel = "channel"
)

// Telegram-internal domains to filter out.
var telegramDomains = map[string]struct{}{
	"t.me":         {},
	"telegram.me":  {},
	"telesco.pe":   {},
	"telegram.org": {},
}

// SeedSource identifies the source of seeded links.
const SeedSourceTelegram = "telegram"

// Seeder seeds external links from Telegram messages into the crawler queue.
type Seeder struct {
	client             *solr.Client
	logger             *zerolog.Logger
	maxLinksPerMessage int
	maxQueuePending    int
	extensionDenylist  map[string]struct{}
	domainAllowlist    map[string]struct{}
	domainDenylist     map[string]struct{}
}

// Config holds the seeder configuration.
type Config struct {
	MaxLinksPerMessage int
	MaxQueuePending    int
	ExtensionDenylist  []string
	DomainAllowlist    []string
	DomainDenylist     []string
}

// NewFromConfig creates a Seeder from application config.
func NewFromConfig(cfg *config.Config, client *solr.Client, logger *zerolog.Logger) *Seeder {
	extDenylist := parseCommaSeparated(cfg.LinkSeedExtDenylist)
	domainAllow := parseCommaSeparated(cfg.DomainAllowlist)
	domainDeny := parseCommaSeparated(cfg.DomainDenylist)

	return New(Config{
		MaxLinksPerMessage: cfg.MaxLinksPerMessage,
		MaxQueuePending:    cfg.CrawlerQueueMaxPending,
		ExtensionDenylist:  extDenylist,
		DomainAllowlist:    domainAllow,
		DomainDenylist:     domainDeny,
	}, client, logger)
}

// New creates a new Seeder with the given configuration.
func New(cfg Config, client *solr.Client, logger *zerolog.Logger) *Seeder {
	extDenylist := buildExtensionDenylist(cfg.ExtensionDenylist)
	domainAllow := buildDomainSet(cfg.DomainAllowlist)
	domainDeny := buildDomainSet(cfg.DomainDenylist)

	return &Seeder{
		client:             client,
		logger:             logger,
		maxLinksPerMessage: cfg.MaxLinksPerMessage,
		maxQueuePending:    cfg.MaxQueuePending,
		extensionDenylist:  extDenylist,
		domainAllowlist:    domainAllow,
		domainDenylist:     domainDeny,
	}
}

func buildExtensionDenylist(extensions []string) map[string]struct{} {
	result := make(map[string]struct{}, len(extensions))

	for _, ext := range extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}

		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}

		result[ext] = struct{}{}
	}

	return result
}

func buildDomainSet(domains []string) map[string]struct{} {
	result := make(map[string]struct{}, len(domains))

	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			result[d] = struct{}{}
		}
	}

	return result
}

// SeedInput contains information about a Telegram message for link seeding.
type SeedInput struct {
	PeerID    int64 // Telegram peer ID of the channel
	MessageID int64 // Telegram message ID
	Channel   string
	URLs      []string
}

// SeedResult contains the results of a seeding operation.
type SeedResult struct {
	Extracted int
	Enqueued  int
	Skipped   map[string]int
	Errors    int
}

// SeedLinks extracts and enqueues links from a Telegram message.
// This is a non-blocking, opportunistic operation - errors are logged but not returned.
func (s *Seeder) SeedLinks(ctx context.Context, input SeedInput) SeedResult {
	result := SeedResult{
		Skipped: make(map[string]int),
	}

	if s.shouldSkipSeeding(&result, input) {
		return result
	}

	result.Extracted = len(input.URLs)
	observability.LinkSeedExtracted.Add(float64(len(input.URLs)))

	if s.isQueueFull(ctx, &result, input) {
		return result
	}

	seedRef := fmt.Sprintf("tg://peer/%d/msg/%d", input.PeerID, input.MessageID)
	s.processURLs(ctx, input, seedRef, &result)

	return result
}

func (s *Seeder) shouldSkipSeeding(result *SeedResult, input SeedInput) bool {
	if !s.client.Enabled() {
		s.logger.Debug().
			Str(logFieldChannel, input.Channel).
			Msg("Link seeding skipped: Solr client disabled")

		result.Skipped[SkipReasonDisabled] = len(input.URLs)

		return true
	}

	return false
}

func (s *Seeder) isQueueFull(ctx context.Context, result *SeedResult, input SeedInput) bool {
	pendingCount, err := s.getQueuePendingCount(ctx)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str(logFieldChannel, input.Channel).
			Msg("Failed to check queue pending count")

		return false
	}

	observability.CrawlerQueuePending.Set(float64(pendingCount))

	if s.maxQueuePending <= 0 {
		return false
	}

	if pendingCount >= s.maxQueuePending {
		s.logger.Debug().
			Int("pending", pendingCount).
			Int("max", s.maxQueuePending).
			Str(logFieldChannel, input.Channel).
			Msg("Link seeding skipped: queue full")

		result.Skipped[SkipReasonQueueFull] = len(input.URLs)
		observability.LinkSeedSkipped.WithLabelValues(SkipReasonQueueFull).Add(float64(len(input.URLs)))

		return true
	}

	return false
}

func (s *Seeder) processURLs(ctx context.Context, input SeedInput, seedRef string, result *SeedResult) {
	enqueued := 0

	for i, rawURL := range input.URLs {
		if s.maxLinksPerMessage > 0 && i >= s.maxLinksPerMessage {
			remaining := len(input.URLs) - i
			result.Skipped[SkipReasonMaxLinks] += remaining
			observability.LinkSeedSkipped.WithLabelValues(SkipReasonMaxLinks).Add(float64(remaining))

			break
		}

		if s.processURL(ctx, rawURL, seedRef, input, result) {
			enqueued++
		}
	}

	result.Enqueued = enqueued
}

func (s *Seeder) processURL(ctx context.Context, rawURL, seedRef string, input SeedInput, result *SeedResult) bool {
	skipReason := s.filterURL(rawURL)
	if skipReason != "" {
		result.Skipped[skipReason]++
		observability.LinkSeedSkipped.WithLabelValues(skipReason).Inc()
		s.logger.Debug().
			Str(logFieldURL, rawURL).
			Int64(logFieldPeerID, input.PeerID).
			Int64(logFieldMsgID, input.MessageID).
			Str(logFieldChannel, input.Channel).
			Str("reason", skipReason).
			Msg("Link skipped")

		return false
	}

	err := s.enqueueURL(ctx, rawURL, seedRef)
	if err != nil {
		return s.handleEnqueueError(err, rawURL, input, result)
	}

	observability.LinkSeedEnqueued.Inc()
	s.logger.Debug().
		Str(logFieldURL, rawURL).
		Int64(logFieldPeerID, input.PeerID).
		Int64(logFieldMsgID, input.MessageID).
		Str(logFieldChannel, input.Channel).
		Msg("URL enqueued for crawling")

	return true
}

func (s *Seeder) handleEnqueueError(err error, rawURL string, input SeedInput, result *SeedResult) bool {
	if errors.Is(err, errDuplicate) {
		result.Skipped[SkipReasonDuplicate]++
		observability.LinkSeedSkipped.WithLabelValues(SkipReasonDuplicate).Inc()

		return false
	}

	result.Errors++

	observability.LinkSeedErrors.Inc()
	s.logger.Warn().
		Err(err).
		Str(logFieldURL, rawURL).
		Int64(logFieldPeerID, input.PeerID).
		Int64(logFieldMsgID, input.MessageID).
		Str(logFieldChannel, input.Channel).
		Msg("Failed to enqueue URL")

	return false
}

var errDuplicate = errors.New("url already exists in queue")

// filterURL checks if a URL should be filtered out.
// Returns empty string if URL is valid, otherwise returns the skip reason.
func (s *Seeder) filterURL(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return SkipReasonInvalidScheme
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return SkipReasonInvalidScheme
	}

	if reason := s.checkScheme(parsed); reason != "" {
		return reason
	}

	host := strings.ToLower(parsed.Hostname())

	if reason := s.checkTelegramDomain(host); reason != "" {
		return reason
	}

	if reason := s.checkExtension(parsed); reason != "" {
		return reason
	}

	if reason := s.checkDomainLists(host); reason != "" {
		return reason
	}

	return ""
}

func (s *Seeder) checkScheme(parsed *url.URL) string {
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return SkipReasonInvalidScheme
	}

	return ""
}

func (s *Seeder) checkTelegramDomain(host string) string {
	if _, isTG := telegramDomains[host]; isTG {
		return SkipReasonTelegramDomain
	}

	return ""
}

func (s *Seeder) checkExtension(parsed *url.URL) string {
	if len(s.extensionDenylist) == 0 {
		return ""
	}

	ext := strings.ToLower(path.Ext(parsed.Path))
	if _, denied := s.extensionDenylist[ext]; denied {
		return SkipReasonDeniedExtension
	}

	return ""
}

func (s *Seeder) checkDomainLists(host string) string {
	if len(s.domainDenylist) > 0 {
		if s.matchesDomain(host, s.domainDenylist) {
			return SkipReasonDeniedDomain
		}
	}

	if len(s.domainAllowlist) > 0 {
		if !s.matchesDomain(host, s.domainAllowlist) {
			return SkipReasonNotAllowed
		}
	}

	return ""
}

// matchesDomain checks if a host matches any domain in the list.
// Supports suffix matching (e.g., "example.com" matches "sub.example.com").
func (s *Seeder) matchesDomain(host string, domains map[string]struct{}) bool {
	if _, ok := domains[host]; ok {
		return true
	}

	for domain := range domains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}

	return false
}

// enqueueURL adds a URL to the crawler queue with seed metadata.
func (s *Seeder) enqueueURL(ctx context.Context, rawURL, seedRef string) error {
	canonicalURL := solr.CanonicalizeURL(rawURL)
	docID := solr.WebDocID(canonicalURL)

	existing, err := s.client.Get(ctx, docID)
	if err != nil && !errors.Is(err, solr.ErrNotFound) {
		return fmt.Errorf("check existing: %w", err)
	}

	if existing != nil {
		return errDuplicate
	}

	// Parse canonical URL for normalized domain (lowercase, no port)
	parsed, err := url.Parse(canonicalURL)
	if err != nil {
		return fmt.Errorf("parse canonical url: %w", err)
	}

	doc := solr.NewIndexDocument(docID).
		SetField("source", solr.SourceWeb).
		SetField(logFieldURL, rawURL).
		SetField("url_canonical", canonicalURL).
		SetField("domain", parsed.Hostname()).
		SetField("crawl_status", solr.CrawlStatusPending).
		SetField("crawl_depth", 0).
		SetField("crawl_seed_source", SeedSourceTelegram).
		SetField("crawl_seed_ref", seedRef).
		SetField("indexed_at", time.Now().UTC().Format(time.RFC3339))

	if err := s.client.Index(ctx, doc); err != nil {
		return fmt.Errorf("index document: %w", err)
	}

	return nil
}

// getQueuePendingCount returns the number of pending URLs in the queue.
func (s *Seeder) getQueuePendingCount(ctx context.Context) (int, error) {
	resp, err := s.client.Search(ctx, "*:*",
		solr.WithFilterQuery("source:web"),
		solr.WithFilterQuery("crawl_status:pending"),
		solr.WithRows(0),
	)
	if err != nil {
		return 0, fmt.Errorf("count pending: %w", err)
	}

	return resp.Response.NumFound, nil
}

// parseCommaSeparated splits a comma-separated string into a slice.
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result
}
