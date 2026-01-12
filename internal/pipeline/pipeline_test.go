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

func (m *mockRepo) SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error {
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

func (m *mockLLM) RelevanceGate(ctx context.Context, text string, model string, prompt string) (llm.RelevanceGateResult, error) {
	return llm.RelevanceGateResult{
		Decision:   DecisionRelevant,
		Confidence: 0.5,
		Reason:     "mock",
	}, nil
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

func TestPipeline_ImportanceWeightApplication(t *testing.T) {
	tests := []struct {
		name             string
		importanceWeight float32
		llmImportance    float32
		expectedMin      float32
		expectedMax      float32
	}{
		{
			name:             "default weight 1.0",
			importanceWeight: 1.0,
			llmImportance:    0.8,
			expectedMin:      0.79,
			expectedMax:      0.81,
		},
		{
			name:             "high weight 1.5",
			importanceWeight: 1.5,
			llmImportance:    0.6,
			expectedMin:      0.89,
			expectedMax:      0.91, // 0.6 * 1.5 = 0.9
		},
		{
			name:             "low weight 0.5",
			importanceWeight: 0.5,
			llmImportance:    0.8,
			expectedMin:      0.39,
			expectedMax:      0.41, // 0.8 * 0.5 = 0.4
		},
		{
			name:             "capped at 1.0",
			importanceWeight: 2.0,
			llmImportance:    0.8,
			expectedMin:      0.99,
			expectedMax:      1.01, // 0.8 * 2.0 = 1.6, capped to 1.0
		},
		{
			name:             "zero weight defaults to 1.0",
			importanceWeight: 0.0,
			llmImportance:    0.8,
			expectedMin:      0.79,
			expectedMax:      0.81, // defaults to 1.0
		},
		{
			name:             "negative weight defaults to 1.0",
			importanceWeight: -0.5,
			llmImportance:    0.8,
			expectedMin:      0.79,
			expectedMax:      0.81, // defaults to 1.0
		},
		{
			name:             "weight above 2.0 clamped to 2.0",
			importanceWeight: 3.0,
			llmImportance:    0.4,
			expectedMin:      0.79,
			expectedMax:      0.81, // 0.4 * 2.0 = 0.8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				WorkerBatchSize:    10,
				RelevanceThreshold: 0.5,
			}
			repo := &mockRepo{
				settings: make(map[string]interface{}),
				unprocessedMessages: []db.RawMessage{
					{
						ID:               "1",
						Text:             "Message with enough text to pass filters",
						CanonicalHash:    "hash1",
						ImportanceWeight: tt.importanceWeight,
					},
				},
			}
			llmClient := &mockLLMWithImportance{importance: tt.llmImportance}
			logger := zerolog.Nop()

			p := New(cfg, repo, llmClient, nil, &logger)

			err := p.processNextBatch(context.Background(), "test-corr-id")
			if err != nil {
				t.Fatalf("processNextBatch failed: %v", err)
			}

			if len(repo.savedItems) != 1 {
				t.Fatalf("expected 1 saved item, got %d", len(repo.savedItems))
			}

			got := repo.savedItems[0].ImportanceScore
			if got < tt.expectedMin || got > tt.expectedMax {
				t.Errorf("ImportanceScore = %v, want between %v and %v", got, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

// mockLLMWithImportance allows configuring the importance score returned
type mockLLMWithImportance struct {
	llm.Client
	importance float32
}

func (m *mockLLMWithImportance) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	return []float32{1.0, 0.0}, nil
}

func (m *mockLLMWithImportance) ProcessBatch(ctx context.Context, messages []llm.MessageInput, targetLanguage string, model string, tone string) ([]llm.BatchResult, error) {
	res := make([]llm.BatchResult, len(messages))
	for i := range messages {
		res[i] = llm.BatchResult{
			Index:           i,
			RelevanceScore:  0.9,
			ImportanceScore: m.importance,
			Topic:           "Test",
			Summary:         "Test summary with unique info here",
			Language:        "en",
			SourceChannel:   messages[i].ChannelTitle,
		}
	}

	return res, nil
}
