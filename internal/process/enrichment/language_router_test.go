package enrichment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	methodGetTranslation  = "GetTranslation"
	methodTranslate       = "Translate"
	methodSaveTranslation = "SaveTranslation"
	testQueryRouter       = "test query"
	translatedEn          = "translated en"
	translatedEl          = "translated el"
	cachedEn              = "cached en"
	cachedEl              = "cached el"
)

type mockRouterRepo struct {
	mock.Mock
}

var errInvalidType = fmt.Errorf("invalid type")

func (m *mockRouterRepo) GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error) {
	args := m.Called(ctx, channelID, before, limit)
	if args.Get(0) == nil {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("%w", err)
		}

		return nil, nil
	}

	res, ok := args.Get(0).([]string)
	if !ok {
		return nil, errInvalidType
	}

	if err := args.Error(1); err != nil {
		return res, fmt.Errorf("%w", err)
	}

	return res, nil
}

func (m *mockRouterRepo) ClaimNextEnrichment(_ context.Context) (*db.EnrichmentQueueItem, error) {
	return nil, nil //nolint:nilnil
}

func (m *mockRouterRepo) UpdateEnrichmentStatus(_ context.Context, _, _, _ string, _ *time.Time) error {
	return nil
}

func (m *mockRouterRepo) GetEvidenceSource(_ context.Context, _ string) (*db.EvidenceSource, error) {
	return nil, nil //nolint:nilnil
}

func (m *mockRouterRepo) SaveEvidenceSource(_ context.Context, _ *db.EvidenceSource) (string, error) {
	return "", nil
}

func (m *mockRouterRepo) SaveEvidenceClaim(_ context.Context, _ *db.EvidenceClaim) (string, error) {
	return "", nil
}

func (m *mockRouterRepo) SaveItemEvidence(_ context.Context, _ *db.ItemEvidence) error { return nil }

func (m *mockRouterRepo) UpdateItemFactCheckScore(_ context.Context, _ string, _ float32, _, _ string) error {
	return nil
}

func (m *mockRouterRepo) DeleteExpiredEvidenceSources(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockRouterRepo) CleanupExcessEvidencePerItem(_ context.Context, _ int) (int64, error) {
	return 0, nil
}

func (m *mockRouterRepo) DeduplicateEvidenceClaims(_ context.Context) (int64, error) { return 0, nil }

func (m *mockRouterRepo) CleanupExpiredTranslations(_ context.Context) (int64, error) { return 0, nil }

func (m *mockRouterRepo) FindSimilarClaim(_ context.Context, _ string, _ []float32, _ float32) (*db.EvidenceClaim, error) {
	return nil, nil //nolint:nilnil
}

func (m *mockRouterRepo) GetDailyEnrichmentCount(_ context.Context) (int, error)    { return 0, nil }
func (m *mockRouterRepo) GetMonthlyEnrichmentCount(_ context.Context) (int, error)  { return 0, nil }
func (m *mockRouterRepo) GetDailyEnrichmentCost(_ context.Context) (float64, error) { return 0, nil }
func (m *mockRouterRepo) GetMonthlyEnrichmentCost(_ context.Context) (float64, error) {
	return 0, nil
}

func (m *mockRouterRepo) IncrementEnrichmentUsage(_ context.Context, _ string, _ float64) error {
	return nil
}

func (m *mockRouterRepo) IncrementEmbeddingUsage(_ context.Context, _ float64) error { return nil }

func (m *mockRouterRepo) GetLinksForMessage(_ context.Context, _ string) ([]domain.ResolvedLink, error) {
	return nil, nil
}

func (m *mockRouterRepo) GetSetting(_ context.Context, _ string, _ interface{}) error { return nil }

func (m *mockRouterRepo) GetTranslation(ctx context.Context, q, l string) (string, error) {
	args := m.Called(ctx, q, l)
	if err := args.Error(1); err != nil {
		return args.String(0), fmt.Errorf("%w", err)
	}

	return args.String(0), nil
}

func (m *mockRouterRepo) SaveTranslation(ctx context.Context, q, l, t string, ttl time.Duration) error {
	args := m.Called(ctx, q, l, t, ttl)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

func (m *mockRouterRepo) RecoverStuckEnrichmentItems(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

func (m *mockRouterRepo) GetClaimsForSource(_ context.Context, _ string) ([]db.EvidenceClaim, error) {
	return nil, nil
}

type mockTranslationClient struct {
	mock.Mock
}

func (m *mockTranslationClient) Translate(ctx context.Context, text, targetLang string) (string, error) {
	args := m.Called(ctx, text, targetLang)
	if err := args.Error(1); err != nil {
		return args.String(0), fmt.Errorf("%w", err)
	}

	return args.String(0), nil
}

func TestLanguageRouter_GetTargetLanguages(t *testing.T) {
	policy := domain.LanguageRoutingPolicy{
		Default: []string{"en"},
		Channel: map[string][]string{
			"@cyprus_news": {"el"},
		},
		Context: []domain.ContextPolicy{
			{
				Name:      "cyprus",
				Languages: []string{"el", "en"},
				Keywords:  []string{"Cyprus", "Nicosia", "Limassol"},
			},
		},
		Topic: map[string][]string{
			"Local News": {"el"},
		},
	}

	repo := new(mockRouterRepo)
	router := NewLanguageRouter(policy, repo)
	ctx := context.Background()

	t.Run("Channel match", func(t *testing.T) {
		item := &db.EnrichmentQueueItem{
			ChannelUsername: "cyprus_news",
		}
		langs := router.GetTargetLanguages(ctx, item)
		assert.Equal(t, []string{"el"}, langs)
	})

	t.Run("Context match in title", func(t *testing.T) {
		item := &db.EnrichmentQueueItem{
			ChannelTitle: "News from Nicosia",
		}
		langs := router.GetTargetLanguages(ctx, item)
		assert.Equal(t, []string{"el", "en"}, langs)
	})

	t.Run("Context match in summary", func(t *testing.T) {
		item := &db.EnrichmentQueueItem{
			Summary: "Water shortages in Limassol reported.",
		}
		langs := router.GetTargetLanguages(ctx, item)
		assert.Equal(t, []string{"el", "en"}, langs)
	})

	t.Run("Topic match", func(t *testing.T) {
		item := &db.EnrichmentQueueItem{
			Topic: "Local News",
		}
		langs := router.GetTargetLanguages(ctx, item)
		assert.Equal(t, []string{"el"}, langs)
	})

	t.Run("Default", func(t *testing.T) {
		item := &db.EnrichmentQueueItem{
			Topic: "International News",
		}
		langs := router.GetTargetLanguages(ctx, item)
		assert.Equal(t, []string{"en"}, langs)
	})

	t.Run("Confidence priority: title vs summary", func(t *testing.T) {
		// Cyprus in title (el), but international topic (en).
		// Title should win over topic/summary.
		item := &db.EnrichmentQueueItem{
			ChannelTitle: "Cyprus Daily",
			Topic:        "Politics", // Not "Local News"
			Summary:      "Generic news summary.",
		}
		langs := router.GetTargetLanguages(ctx, item)
		assert.Equal(t, []string{"el", "en"}, langs)
	})
}

func TestWorker_ExpandQueriesWithRouting(t *testing.T) {
	policy := domain.LanguageRoutingPolicy{
		Default: []string{"en", "el"},
	}

	repo := new(mockRouterRepo)
	trans := new(mockTranslationClient)
	logger := zerolog.Nop()

	w := &Worker{
		db:                repo,
		translationClient: trans,
		queryExpander:     NewQueryExpander(trans, repo, &logger),
		languageRouter:    NewLanguageRouter(policy, repo),
		cfg:               &config.Config{EnrichmentQueryTranslate: true},
		logger:            &logger,
	}

	ctx := context.Background()
	item := &db.EnrichmentQueueItem{}
	queries := []GeneratedQuery{
		{Query: testQueryRouter, Language: "ru", Strategy: "keyword"},
	}

	t.Run("Translate with cache miss", func(t *testing.T) {
		repo.On(methodGetTranslation, ctx, testQueryRouter, "en").Return("", nil).Once()
		trans.On(methodTranslate, ctx, testQueryRouter, "en").Return(translatedEn, nil).Once()
		repo.On(methodSaveTranslation, ctx, testQueryRouter, "en", translatedEn, mock.Anything).Return(nil).Once()

		repo.On(methodGetTranslation, ctx, testQueryRouter, "el").Return("", nil).Once()
		trans.On(methodTranslate, ctx, testQueryRouter, "el").Return(translatedEl, nil).Once()
		repo.On(methodSaveTranslation, ctx, testQueryRouter, "el", translatedEl, mock.Anything).Return(nil).Once()

		res := w.expandQueriesWithRouting(ctx, item, queries)

		// Only translated queries are included (original "ru" doesn't match targets "en", "el")
		assert.Len(t, res, 2)
		assert.Equal(t, translatedEn, res[0].Query)
		assert.Equal(t, "en", res[0].Language)
		assert.Equal(t, translatedEl, res[1].Query)
		assert.Equal(t, "el", res[1].Language)
		repo.AssertExpectations(t)
		trans.AssertExpectations(t)
	})

	t.Run("Translate with cache hit", func(t *testing.T) {
		repo.On(methodGetTranslation, ctx, testQueryRouter, "en").Return(cachedEn, nil).Once()
		repo.On(methodGetTranslation, ctx, testQueryRouter, "el").Return(cachedEl, nil).Once()

		res := w.expandQueriesWithRouting(ctx, item, queries)

		// Only translated queries are included
		assert.Len(t, res, 2)
		assert.Equal(t, cachedEn, res[0].Query)
		assert.Equal(t, cachedEl, res[1].Query)
		repo.AssertExpectations(t)
	})

	t.Run("Respect query cap", func(t *testing.T) {
		w.cfg.EnrichmentMaxQueriesPerItem = 2
		w.languageRouter.policy.Default = []string{"en", "el", "es"}

		repo.On(methodGetTranslation, ctx, testQueryRouter, "en").Return("en q", nil).Once()
		repo.On(methodGetTranslation, ctx, testQueryRouter, "el").Return("el q", nil).Once()
		// "es" should not be called because of cap (0 original + 2 translations = 2)

		res := w.expandQueriesWithRouting(ctx, item, queries)

		assert.Len(t, res, 2) // 2 translated (cap reached)
		repo.AssertExpectations(t)
	})
}
