package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/settings"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	testResolveLinks = "ResolveLinks"
	testGetSetting   = "GetSetting"
)

type mockLinkResolver struct {
	mock.Mock
}

func (m *mockLinkResolver) ResolveLinks(ctx context.Context, text string, maxLinks int, webTTL, tgTTL time.Duration) ([]domain.ResolvedLink, error) {
	args := m.Called(ctx, text, maxLinks, webTTL, tgTTL)
	res, _ := args.Get(0).([]domain.ResolvedLink)

	if args.Error(1) != nil {
		return res, fmt.Errorf("%w", args.Error(1))
	}

	return res, nil
}

func TestResolveBackfillMessages(t *testing.T) {
	resolver := new(mockLinkResolver)
	repo := new(mockRepo)
	logger := zerolog.Nop()
	p := &Pipeline{
		database:     repo,
		linkResolver: resolver,
		logger:       &logger,
	}

	ctx := context.Background()
	messages := []db.RawMessage{
		{
			ID:           "msg1",
			Text:         "check this link",
			EntitiesJSON: []byte(`[{"URL":"https://example.com"}]`),
		},
		{
			ID:        "msg2",
			Text:      "",
			MediaJSON: []byte(`{"Webpage":{"URL":"https://another.com"}}`),
		},
		{
			ID:   "msg3",
			Text: "no links here",
		},
	}

	maxLinks := 5
	webTTL := time.Hour
	tgTTL := 24 * time.Hour

	// msg1 has text and entity link
	resolver.On(testResolveLinks, ctx, "check this link https://example.com", maxLinks, webTTL, tgTTL).Return([]domain.ResolvedLink{
		{ID: "link1", URL: "https://example.com"},
	}, nil)

	// msg2 has only media link
	resolver.On(testResolveLinks, ctx, "https://another.com", maxLinks, webTTL, tgTTL).Return([]domain.ResolvedLink{
		{ID: "link2", URL: "https://another.com"},
	}, nil)

	// msg3 has no links, should be processed and resolver called with just text
	resolver.On(testResolveLinks, ctx, "no links here", maxLinks, webTTL, tgTTL).Return([]domain.ResolvedLink{}, nil)

	stats := p.resolveBackfillMessages(ctx, logger, messages, maxLinks, webTTL, tgTTL)

	assert.Equal(t, 0, stats.skipped)
	assert.Equal(t, 2, stats.resolved) // link1 and link2
	assert.Equal(t, 2, stats.linked)   // both linked
	assert.Equal(t, 0, stats.failures)

	resolver.AssertExpectations(t)
}

type fullMockRepo struct {
	mock.Mock
}

func (m *fullMockRepo) GetSetting(ctx context.Context, key string, target interface{}) error {
	args := m.Called(ctx, key, target)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) DeleteSetting(ctx context.Context, key string) error {
	args := m.Called(ctx, key)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) GetUnprocessedMessages(ctx context.Context, limit int) ([]db.RawMessage, error) {
	args := m.Called(ctx, limit)
	res, _ := args.Get(0).([]db.RawMessage)

	if args.Error(1) != nil {
		return res, fmt.Errorf("%w", args.Error(1))
	}

	return res, nil
}

func (m *fullMockRepo) GetRawMessagesForLinkBackfill(ctx context.Context, since time.Time, limit int) ([]db.RawMessage, error) {
	args := m.Called(ctx, since, limit)
	res, _ := args.Get(0).([]db.RawMessage)

	if args.Error(1) != nil {
		return res, fmt.Errorf("%w", args.Error(1))
	}

	return res, nil
}

func (m *fullMockRepo) GetBacklogCount(ctx context.Context) (int, error) {
	args := m.Called(ctx)

	if args.Error(1) != nil {
		return args.Int(0), fmt.Errorf("%w", args.Error(1))
	}

	return args.Int(0), nil
}

func (m *fullMockRepo) GetActiveFilters(ctx context.Context) ([]db.Filter, error) {
	args := m.Called(ctx)
	res, _ := args.Get(0).([]db.Filter)

	if args.Error(1) != nil {
		return res, fmt.Errorf("%w", args.Error(1))
	}

	return res, nil
}

func (m *fullMockRepo) MarkAsProcessed(ctx context.Context, id string) error {
	args := m.Called(ctx, id)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error) {
	args := m.Called(ctx, channelID, before, limit)
	res, _ := args.Get(0).([]string)

	if args.Error(1) != nil {
		return res, fmt.Errorf("%w", args.Error(1))
	}

	return res, nil
}

func (m *fullMockRepo) GetChannelStats(ctx context.Context) (map[string]db.ChannelStats, error) {
	args := m.Called(ctx)
	res, _ := args.Get(0).(map[string]db.ChannelStats)

	if args.Error(1) != nil {
		return res, fmt.Errorf("%w", args.Error(1))
	}

	return res, nil
}

func (m *fullMockRepo) SaveItem(ctx context.Context, item *db.Item) error {
	args := m.Called(ctx, item)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error {
	args := m.Called(ctx, rawMsgID, errJSON)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error {
	args := m.Called(ctx, rawMsgID, decision, confidence, reason, model, gateVersion)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) SaveRawMessageDropLog(ctx context.Context, rawMsgID, reason, detail string) error {
	args := m.Called(ctx, rawMsgID, reason, detail)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error {
	args := m.Called(ctx, itemID, embedding)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) EnqueueFactCheck(ctx context.Context, itemID, claim, normalizedClaim string) error {
	args := m.Called(ctx, itemID, claim, normalizedClaim)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) CountPendingFactChecks(ctx context.Context) (int, error) {
	args := m.Called(ctx)

	if args.Error(1) != nil {
		return args.Int(0), fmt.Errorf("%w", args.Error(1))
	}

	return args.Int(0), nil
}

func (m *fullMockRepo) EnqueueEnrichment(ctx context.Context, itemID, summary string) error {
	args := m.Called(ctx, itemID, summary)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func (m *fullMockRepo) CountPendingEnrichments(ctx context.Context) (int, error) {
	args := m.Called(ctx)

	if args.Error(1) != nil {
		return args.Int(0), fmt.Errorf("%w", args.Error(1))
	}

	return args.Int(0), nil
}

func (m *fullMockRepo) CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error) {
	args := m.Called(ctx, hash, id)

	if args.Error(1) != nil {
		return args.Bool(0), fmt.Errorf("%w", args.Error(1))
	}

	return args.Bool(0), nil
}

func (m *fullMockRepo) FindSimilarItem(ctx context.Context, embedding []float32, threshold float32) (string, error) {
	args := m.Called(ctx, embedding, threshold)

	if args.Error(1) != nil {
		return args.String(0), fmt.Errorf("%w", args.Error(1))
	}

	return args.String(0), nil
}

func (m *fullMockRepo) LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error {
	args := m.Called(ctx, rawMsgID, linkCacheID, position)

	if args.Error(0) != nil {
		return fmt.Errorf("%w", args.Error(0))
	}

	return nil
}

func TestProcessBackfill(t *testing.T) {
	repo := new(fullMockRepo)
	resolver := new(mockLinkResolver)
	logger := zerolog.Nop()
	cfg := &config.Config{
		MaxLinksPerMessage:   3,
		LinkCacheTTL:         time.Hour,
		TelegramLinkCacheTTL: 2 * time.Hour,
	}
	p := New(cfg, repo, nil, resolver, &logger)

	ctx := context.Background()
	req := settings.LinkBackfillRequest{
		Hours: 24,
		Limit: 10,
	}

	repo.On("GetRawMessagesForLinkBackfill", ctx, mock.AnythingOfType("time.Time"), 10).Return([]db.RawMessage{
		{ID: "m1", Text: "link https://t.me/c/1/1"},
	}, nil)

	repo.On(testGetSetting, ctx, SettingMaxLinksPerMessage, mock.Anything).Return(nil)
	repo.On(testGetSetting, ctx, SettingLinkCacheTTL, mock.Anything).Return(nil)
	repo.On(testGetSetting, ctx, SettingTgLinkCacheTTL, mock.Anything).Return(nil)

	resolver.On(testResolveLinks, ctx, "link https://t.me/c/1/1", 3, cfg.LinkCacheTTL, cfg.TelegramLinkCacheTTL).Return([]domain.ResolvedLink{
		{ID: "l1", URL: "https://t.me/c/1/1"},
	}, nil)

	repo.On("LinkMessageToLink", ctx, "m1", "l1", 0).Return(nil)
	repo.On("DeleteSetting", ctx, settings.SettingLinkBackfillRequest).Return(nil)

	err := p.processBackfill(ctx, logger, req)
	require.NoError(t, err)

	repo.AssertExpectations(t)
	resolver.AssertExpectations(t)
}
