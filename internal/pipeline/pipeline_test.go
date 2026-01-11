package pipeline

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/llm"
)

type mockRepo struct {
	settings            map[string]interface{}
	unprocessedMessages []db.RawMessage
	filters             []db.Filter
	savedItems          []*db.Item
	markedProcessed     []string
}

func (m *mockRepo) GetSetting(ctx context.Context, key string, target interface{}) error {
	val, ok := m.settings[key]
	if !ok {
		return nil
	}
	// For simplicity in tests, assume types match
	data, _ := json.Marshal(val)
	return json.Unmarshal(data, target)
}

func (m *mockRepo) GetUnprocessedMessages(ctx context.Context, limit int) ([]db.RawMessage, error) {
	if len(m.unprocessedMessages) > limit {
		return m.unprocessedMessages[:limit], nil
	}
	return m.unprocessedMessages, nil
}

func (m *mockRepo) GetBacklogCount(ctx context.Context) (int, error) {
	return len(m.unprocessedMessages), nil
}

func (m *mockRepo) GetActiveFilters(ctx context.Context) ([]db.Filter, error) {
	return m.filters, nil
}

func (m *mockRepo) MarkAsProcessed(ctx context.Context, id string) error {
	m.markedProcessed = append(m.markedProcessed, id)
	return nil
}

func (m *mockRepo) GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error) {
	return nil, nil
}

func (m *mockRepo) GetChannelStats(ctx context.Context) (map[string]db.ChannelStats, error) {
	return map[string]db.ChannelStats{}, nil
}

func (m *mockRepo) SaveItem(ctx context.Context, item *db.Item) error {
	m.savedItems = append(m.savedItems, item)
	item.ID = "new-id"
	return nil
}

func (m *mockRepo) SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error {
	return nil
}

func (m *mockRepo) SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error {
	return nil
}

func (m *mockRepo) CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error) {
	return false, nil
}

func (m *mockRepo) FindSimilarItem(ctx context.Context, embedding []float32, threshold float32) (string, error) {
	return "", nil
}

func (m *mockRepo) LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error {
	return nil
}

type mockLLM struct {
	llm.Client
}

func (m *mockLLM) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	if text == "Message 1 that is long enough to pass filters" {
		return []float32{1.0, 0.0}, nil
	}
	return []float32{0.0, 1.0}, nil
}

func (m *mockLLM) ProcessBatch(ctx context.Context, messages []llm.MessageInput, targetLanguage string, model string, tone string) ([]llm.BatchResult, error) {
	res := make([]llm.BatchResult, len(messages))
	for i := range messages {
		res[i] = llm.BatchResult{
			Index:           i,
			RelevanceScore:  0.9,
			ImportanceScore: 0.8,
			Topic:           "Test",
			Summary:         "Test summary",
			Language:        "en",
			SourceChannel:   messages[i].ChannelTitle,
		}
	}
	return res, nil
}

func TestPipeline_processNextBatch(t *testing.T) {
	cfg := &config.Config{
		WorkerBatchSize:    10,
		RelevanceThreshold: 0.5,
	}
	repo := &mockRepo{
		settings: make(map[string]interface{}),
		unprocessedMessages: []db.RawMessage{
			{ID: "1", Text: "Message 1 that is long enough to pass filters", CanonicalHash: "hash1"},
			{ID: "2", Text: "Message 2 that is also long enough", CanonicalHash: "hash2"},
		},
	}
	llmClient := &mockLLM{}
	logger := zerolog.Nop()

	p := New(cfg, repo, llmClient, nil, &logger)

	err := p.processNextBatch(context.Background(), "test-corr-id")
	if err != nil {
		t.Fatalf("processNextBatch failed: %v", err)
	}

	if len(repo.savedItems) != 2 {
		t.Errorf("expected 2 saved items, got %d", len(repo.savedItems))
	}

	if len(repo.markedProcessed) != 2 {
		t.Errorf("expected 2 marked as processed, got %d", len(repo.markedProcessed))
	}
}
