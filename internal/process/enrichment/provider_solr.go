package enrichment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
)

const (
	solrHealthCheckTimeout = 5 * time.Second
)

// SolrProvider implements the Provider and LanguageSearchProvider interfaces
// for searching documents indexed in SolrCloud.
type SolrProvider struct {
	client     *solr.Client
	maxResults int
	enabled    bool
}

// SolrConfig holds configuration for the Solr provider.
type SolrConfig struct {
	Enabled    bool
	BaseURL    string
	Timeout    time.Duration
	MaxResults int
}

// NewSolrProvider creates a new Solr search provider.
func NewSolrProvider(cfg SolrConfig) *SolrProvider {
	client := solr.New(solr.Config{
		Enabled:    cfg.Enabled,
		BaseURL:    cfg.BaseURL,
		Timeout:    cfg.Timeout,
		MaxResults: cfg.MaxResults,
	})

	return &SolrProvider{
		client:     client,
		maxResults: cfg.MaxResults,
		enabled:    cfg.Enabled,
	}
}

// Name returns the provider name.
func (p *SolrProvider) Name() ProviderName {
	return ProviderSolr
}

// Priority returns the provider priority (same as YaCy - highest self-hosted).
func (p *SolrProvider) Priority() int {
	return PriorityHighSelfHosted
}

// IsAvailable checks if Solr is reachable.
func (p *SolrProvider) IsAvailable(ctx context.Context) bool {
	if !p.enabled {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, solrHealthCheckTimeout)
	defer cancel()

	return p.client.Ping(ctx) == nil
}

// Search executes a search query without language filtering.
func (p *SolrProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	return p.SearchWithLanguage(ctx, query, "", maxResults)
}

// SearchWithLanguage executes a search query with optional language-specific field boosting.
func (p *SolrProvider) SearchWithLanguage(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if p.maxResults > 0 && maxResults > p.maxResults {
		maxResults = p.maxResults
	}

	opts := p.buildSearchOptions(query, language, maxResults)

	resp, err := p.client.Search(ctx, query, opts...)
	if err != nil {
		return nil, fmt.Errorf("solr search: %w", err)
	}

	results := p.convertToSearchResults(resp.Response.Docs)

	return p.filterResultsByLanguage(results, language), nil
}

// buildSearchOptions constructs Solr search options with language filtering and boosting.
func (p *SolrProvider) buildSearchOptions(_, language string, maxResults int) []solr.SearchOption {
	const optsCapacity = 6

	opts := make([]solr.SearchOption, 0, optsCapacity)
	opts = append(opts,
		solr.WithRows(maxResults),
		// Only return documents that have been successfully crawled
		solr.WithFilterQuery("crawl_status:done"),
		solr.WithFields("id,url,title,content,description,domain,language,published_at,source"),
		solr.WithSort("score desc, published_at desc"),
	)

	// Add language filter at Solr level if language is specified and not "unknown"
	// This is more efficient than post-filtering in Go and avoids cross-language noise
	normalizedLang := normalizeLanguageCode(language)
	if normalizedLang != "" && normalizedLang != langUnknown {
		opts = append(opts, solr.WithFilterQuery("language:"+normalizedLang))
	}

	// Use edismax with language-specific field boosting
	qf := p.buildQueryFields(language)
	opts = append(opts, solr.WithEdismax(qf))

	return opts
}

// normalizeLanguageCode normalizes language names to ISO codes.
func normalizeLanguageCode(language string) string {
	switch strings.ToLower(language) {
	case langEnglish, langNameEnglish:
		return langEnglish
	case langRussian, langNameRussian:
		return langRussian
	case langGreek, langNameGreek:
		return langGreek
	case langGerman, langNameGerman:
		return langGerman
	case langFrench, langNameFrench:
		return langFrench
	case langUnknown, "":
		return langUnknown
	default:
		return strings.ToLower(language)
	}
}

// buildQueryFields returns query fields with language-specific boosting.
func (p *SolrProvider) buildQueryFields(language string) string {
	// Base query fields
	qf := "title^3 content^1 description^2"

	// Add language-specific field boosting if language is specified
	switch strings.ToLower(language) {
	case langEnglish, langNameEnglish:
		qf = "title_en^4 content_en^1.5 title^3 content^1 description^2"
	case langRussian, langNameRussian:
		qf = "title_ru^4 content_ru^1.5 title^3 content^1 description^2"
	case langGreek, langNameGreek:
		qf = "title_el^4 content_el^1.5 title^3 content^1 description^2"
	case langGerman, langNameGerman:
		qf = "title_de^4 content_de^1.5 title^3 content^1 description^2"
	case langFrench, langNameFrench:
		qf = "title_fr^4 content_fr^1.5 title^3 content^1 description^2"
	}

	return qf
}

// convertToSearchResults converts Solr documents to SearchResults.
func (p *SolrProvider) convertToSearchResults(docs []solr.Document) []SearchResult {
	results := make([]SearchResult, 0, len(docs))

	for _, doc := range docs {
		result := SearchResult{
			URL:         doc.URL,
			Title:       doc.Title,
			Description: doc.Description,
			Domain:      doc.Domain,
			Language:    doc.Language,
			PublishedAt: doc.PublishedAt,
		}

		// Use content as description if description is empty
		if result.Description == "" && doc.Content != "" {
			result.Description = truncateSolrContent(doc.Content, maxDescriptionLength)
		}

		results = append(results, result)
	}

	return results
}

// filterResultsByLanguage filters results by detected language.
func (p *SolrProvider) filterResultsByLanguage(results []SearchResult, language string) []SearchResult {
	if language == "" || isUnknownLanguage(language) {
		return results
	}

	filtered := make([]SearchResult, 0, len(results))

	for _, res := range results {
		// First check the stored language
		if res.Language != "" && languageMatches(language, res.Language) {
			filtered = append(filtered, res)
			continue
		}

		// Fall back to detection from content
		detected := links.DetectLanguage(res.Title + " " + res.Description)
		if detected == "" {
			continue
		}

		if detected == language {
			filtered = append(filtered, res)
		}
	}

	return filtered
}

// truncateSolrContent truncates content to the specified length.
func truncateSolrContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}

	return content[:maxLen] + "..."
}
