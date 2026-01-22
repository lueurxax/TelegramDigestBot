package enrichment

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// TranslationCache provides caching for translated queries.
type TranslationCache interface {
	GetTranslation(ctx context.Context, query, targetLang string) (string, error)
	SaveTranslation(ctx context.Context, query, targetLang, translatedText string, ttl time.Duration) error
}

// QueryExpander expands queries by translating them to target languages.
type QueryExpander struct {
	translationClient TranslationClient
	cache             TranslationCache
	logger            *zerolog.Logger
	cacheTTL          time.Duration
}

// NewQueryExpander creates a new query expander.
func NewQueryExpander(translationClient TranslationClient, cache TranslationCache, logger *zerolog.Logger) *QueryExpander {
	return &QueryExpander{
		translationClient: translationClient,
		cache:             cache,
		logger:            logger,
		cacheTTL:          defaultTranslationCacheTTL,
	}
}

// ExpandQueries translates queries to target languages and returns the expanded set.
// Only queries matching target languages are included (original or translated).
func (e *QueryExpander) ExpandQueries(ctx context.Context, queries []GeneratedQuery, targetLangs []string, maxQueries int) []GeneratedQuery {
	if e.translationClient == nil {
		return queries
	}

	result := e.copyOriginalQueries(queries, targetLangs, maxQueries)
	result = e.appendTranslatedQueries(ctx, result, queries, targetLangs, maxQueries)

	return result
}

// HasTranslation returns true if the expander can translate queries.
func (e *QueryExpander) HasTranslation() bool {
	return e.translationClient != nil
}

func (e *QueryExpander) copyOriginalQueries(queries []GeneratedQuery, targetLangs []string, maxQueries int) []GeneratedQuery {
	result := make([]GeneratedQuery, 0, maxQueries)

	for _, q := range queries {
		if len(result) >= maxQueries {
			break
		}

		// Only include original queries that match a target language
		if e.queryMatchesTargetLanguage(q, targetLangs) {
			result = append(result, q)
		}
	}

	return result
}

func (e *QueryExpander) queryMatchesTargetLanguage(q GeneratedQuery, targetLangs []string) bool {
	if len(targetLangs) == 0 {
		return true
	}

	for _, lang := range targetLangs {
		if q.Language == lang {
			return true
		}
	}

	return false
}

func (e *QueryExpander) appendTranslatedQueries(ctx context.Context, result, queries []GeneratedQuery, targetLangs []string, maxQueries int) []GeneratedQuery {
	for _, targetLang := range targetLangs {
		if len(result) >= maxQueries {
			break
		}

		result = e.translateQueriesForLanguage(ctx, result, queries, targetLang, maxQueries)
	}

	return result
}

func (e *QueryExpander) translateQueriesForLanguage(ctx context.Context, result, queries []GeneratedQuery, targetLang string, maxQueries int) []GeneratedQuery {
	for _, originalQ := range queries {
		if len(result) >= maxQueries {
			break
		}

		if originalQ.Language == targetLang {
			continue
		}

		if translated := e.tryTranslateQuery(ctx, originalQ, targetLang); translated != nil {
			result = append(result, *translated)
		}
	}

	return result
}

func (e *QueryExpander) tryTranslateQuery(ctx context.Context, originalQ GeneratedQuery, targetLang string) *GeneratedQuery {
	translated, err := e.translateWithCache(ctx, originalQ.Query, targetLang)
	if err != nil {
		if e.logger != nil {
			e.logger.Debug().
				Err(err).
				Str(logKeyQuery, originalQ.Query).
				Str(logKeyLanguage, targetLang).
				Msg("failed to translate query")
		}

		return nil
	}

	if translated == "" || translated == originalQ.Query {
		return nil
	}

	return &GeneratedQuery{
		Query:    translated,
		Strategy: originalQ.Strategy + "_translated",
		Language: targetLang,
	}
}

func (e *QueryExpander) translateWithCache(ctx context.Context, text, targetLang string) (string, error) {
	if e.cache != nil {
		if cached, err := e.cache.GetTranslation(ctx, text, targetLang); err == nil && cached != "" {
			return cached, nil
		}
	}

	translated, err := e.translationClient.Translate(ctx, text, targetLang)
	if err != nil {
		return "", fmt.Errorf(fmtErrTranslateTo, targetLang, err)
	}

	if e.cache != nil {
		if err := e.cache.SaveTranslation(ctx, text, targetLang, translated, e.cacheTTL); err != nil && e.logger != nil {
			e.logger.Warn().Err(err).Msg("failed to save translation to cache")
		}
	}

	return translated, nil
}
