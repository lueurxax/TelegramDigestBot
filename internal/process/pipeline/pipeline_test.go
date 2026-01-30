package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/process/filters"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

var errLLM = errors.New("llm error")

const (
	tech = "Tech"

	image = "image"

	testID = "test-id"
)

type mockRepo struct {
	settings            map[string]interface{}
	unprocessedMessages []db.RawMessage
	filters             []db.Filter
	savedItems          []*db.Item
	markedProcessed     []string
}

func (m *mockRepo) GetSetting(_ context.Context, key string, target interface{}) error {
	val, ok := m.settings[key]

	if !ok {
		return nil
	}
	// For simplicity in tests, assume types match

	data, _ := json.Marshal(val) //nolint:errchkjson // test helper, marshaling test data

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal setting: %w", err)
	}

	return nil
}

func (m *mockRepo) GetUnprocessedMessages(_ context.Context, limit int) ([]db.RawMessage, error) {
	if len(m.unprocessedMessages) > limit {
		return m.unprocessedMessages[:limit], nil
	}

	return m.unprocessedMessages, nil
}

func (m *mockRepo) GetBacklogCount(_ context.Context) (int, error) {
	return len(m.unprocessedMessages), nil
}

func (m *mockRepo) GetActiveFilters(_ context.Context) ([]db.Filter, error) {
	return m.filters, nil
}

func (m *mockRepo) MarkAsProcessed(_ context.Context, id string) error {
	m.markedProcessed = append(m.markedProcessed, id)

	return nil
}

func (m *mockRepo) ReleaseClaimedMessage(_ context.Context, _ string) error {
	return nil
}

func (m *mockRepo) RecoverStuckPipelineMessages(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

func (m *mockRepo) GetRecentMessagesForChannel(_ context.Context, _ string, _ time.Time, _ int) ([]string, error) {
	return nil, nil
}

func (m *mockRepo) GetChannelStats(_ context.Context) (map[string]db.ChannelStats, error) {
	return map[string]db.ChannelStats{}, nil
}

func (m *mockRepo) SaveItem(_ context.Context, item *db.Item) error {
	m.savedItems = append(m.savedItems, item)
	item.ID = "new-id"

	return nil
}

func (m *mockRepo) SaveItemError(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (m *mockRepo) SaveRelevanceGateLog(_ context.Context, _, _ string, _ *float32, _, _, _ string) error {
	return nil
}

func (m *mockRepo) SaveRawMessageDropLog(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockRepo) SaveEmbedding(_ context.Context, _ string, _ []float32) error {
	return nil
}

func (m *mockRepo) EnqueueFactCheck(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockRepo) CountPendingFactChecks(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockRepo) EnqueueEnrichment(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockRepo) CountPendingEnrichments(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockRepo) CheckStrictDuplicate(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (m *mockRepo) FindSimilarItem(_ context.Context, _ []float32, _ float32, _ time.Time) (string, error) {
	return "", nil
}

func (m *mockRepo) FindSimilarItemForChannel(_ context.Context, _ []float32, _ string, _ float32, _ time.Time) (string, error) {
	return "", nil
}

func (m *mockRepo) GetSummaryCache(_ context.Context, _, _ string) (*db.SummaryCacheEntry, error) {
	return nil, db.ErrSummaryCacheNotFound
}

func (m *mockRepo) UpsertSummaryCache(_ context.Context, _ *db.SummaryCacheEntry) error {
	return nil
}

func (m *mockRepo) LinkMessageToLink(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (m *mockRepo) InsertBullet(_ context.Context, _ *db.Bullet) error {
	return nil
}

func (m *mockRepo) UpdateBulletEmbedding(_ context.Context, _ string, _ []float32) error {
	return nil
}

func (m *mockRepo) UpdateBulletStatus(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockRepo) GetPendingBulletsForDedup(_ context.Context) ([]db.PendingBulletForDedup, error) {
	return nil, nil
}

func (m *mockRepo) MarkDuplicateBullets(_ context.Context, _ []string) error {
	return nil
}

func (m *mockRepo) DeleteSetting(_ context.Context, key string) error {
	delete(m.settings, key)

	return nil
}

func (m *mockRepo) GetRawMessagesForLinkBackfill(_ context.Context, _ time.Time, _ int) ([]db.RawMessage, error) {
	return nil, nil
}

type mockEmbeddingClient struct {
	embeddings.Client
}

func (m *mockEmbeddingClient) GetEmbedding(_ context.Context, text string) ([]float32, error) {
	if text == "Message 1 that is long enough to pass filters" {
		return []float32{1.0, 0.0}, nil
	}

	return []float32{0.0, 1.0}, nil
}

type mockLLM struct {
	llm.Client
}

func (m *mockLLM) ProcessBatch(_ context.Context, messages []llm.MessageInput, _, _, _ string) ([]llm.BatchResult, error) {
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

func (m *mockLLM) TranslateText(_ context.Context, text string, _ string, _ string) (string, error) {
	return text, nil
}

func (m *mockLLM) RelevanceGate(_ context.Context, _, _, _ string) (llm.RelevanceGateResult, error) {
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
	embeddingClient := &mockEmbeddingClient{}

	logger := zerolog.Nop()

	p := New(cfg, repo, llmClient, embeddingClient, nil, nil, &logger)

	err := p.processNextBatch(context.Background(), "test-corr-id") //nolint:goconst // test literal
	if err != nil {
		t.Fatalf("processNextBatch failed: %v", err) //nolint:goconst // test literal
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
			embeddingClient := &mockEmbeddingClient{}

			logger := zerolog.Nop()

			p := New(cfg, repo, llmClient, embeddingClient, nil, nil, &logger)

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

func (m *mockLLMWithImportance) ProcessBatch(_ context.Context, messages []llm.MessageInput, _, _, _ string) ([]llm.BatchResult, error) {
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

func (m *mockLLMWithImportance) TranslateText(_ context.Context, text string, _ string, _ string) (string, error) {
	return text, nil
}

func TestHasUniqueInfo(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		want    bool
	}{
		{
			name:    "contains proper name",
			summary: "John announced new features",
			want:    true,
		},
		{
			name:    "contains number",
			summary: "Revenue grew by 25 percent",
			want:    true,
		},
		{
			name:    "contains date reference",
			summary: "Event scheduled for Monday",
			want:    true,
		},
		{
			name:    "contains month",
			summary: "Launch planned for January",
			want:    true,
		},
		{
			name:    "generic statement no unique info",
			summary: "something happened somewhere",
			want:    false,
		},
		{
			name:    "empty string",
			summary: "",
			want:    false,
		},
		{
			name:    "with HTML tags",
			summary: "<b>John</b> announced something",
			want:    true,
		},
		{
			name:    "today reference",
			summary: "happening today in the market",
			want:    true,
		},
	}

	cfg := &config.Config{}

	p := New(cfg, nil, nil, nil, nil, nil, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.hasUniqueInfo(tt.summary); got != tt.want {
				t.Errorf("hasUniqueInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase",
			input: "en",
			want:  "en",
		},
		{
			name:  "uppercase",
			input: "EN",
			want:  "en",
		},
		{
			name:  "mixed case",
			input: "RuSsIaN",
			want:  "russian",
		},
		{
			name:  "with leading space",
			input: " ru",
			want:  "ru",
		},
		{
			name:  "with trailing space",
			input: "uk ",
			want:  "uk",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLanguage(tt.input); got != tt.want {
				t.Errorf("normalizeLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainsUkrainianLetters(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "pure English",
			text: "Hello world",
			want: false,
		},
		{
			name: "Russian text without Ukrainian letters",
			text: "Привет мир",
			want: false,
		},
		{
			name: "contains Ukrainian Є",
			text: "Європа",
			want: true,
		},
		{
			name: "contains Ukrainian І",
			text: "Київ",
			want: true,
		},
		{
			name: "contains Ukrainian Ї",
			text: "Україна",
			want: true,
		},
		{
			name: "contains Ukrainian Ґ",
			text: "Ґанок",
			want: true,
		},
		{
			name: "empty string",
			text: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsUkrainianLetters(tt.text); got != tt.want {
				t.Errorf("containsUkrainianLetters() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummaryNeedsTranslation(t *testing.T) {
	tests := []struct {
		name         string
		summary      string
		detectedLang string
		targetLang   string
		want         bool
	}{
		{
			name:         "empty summary",
			summary:      "",
			detectedLang: "en",
			targetLang:   "ru",
			want:         false,
		},
		{
			name:         "empty target",
			summary:      "Hello world",
			detectedLang: "en",
			targetLang:   "",
			want:         false,
		},
		{
			name:         "same language",
			summary:      "Hello world",
			detectedLang: "en",
			targetLang:   "en",
			want:         false,
		},
		{
			name:         "different languages",
			summary:      "Hello world",
			detectedLang: "en",
			targetLang:   "ru",
			want:         true,
		},
		{
			name:         "Ukrainian to Russian",
			summary:      "Київ столиця",
			detectedLang: "",
			targetLang:   "ru",
			want:         true,
		},
		{
			name:         "whitespace only summary",
			summary:      "   ",
			detectedLang: "en",
			targetLang:   "ru",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summaryNeedsTranslation(tt.summary, tt.detectedLang, tt.targetLang); got != tt.want {
				t.Errorf("summaryNeedsTranslation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectTieredCandidates(t *testing.T) {
	t.Run("selects high importance candidates above threshold", func(t *testing.T) {
		candidates := []llm.MessageInput{
			{RawMessage: db.RawMessage{ID: "1"}},
			{RawMessage: db.RawMessage{ID: "2"}},
			{RawMessage: db.RawMessage{ID: "3"}},
		}

		results := []llm.BatchResult{
			{ImportanceScore: 0.5},  // below threshold (0.8)
			{ImportanceScore: 0.85}, // above threshold
			{ImportanceScore: 0.9},  // above threshold
		}

		indices, selected := selectTieredCandidates(candidates, results, nil)

		if len(indices) != 2 {
			t.Fatalf("expected 2 selected, got %d", len(indices))
		}

		if indices[0] != 1 || indices[1] != 2 {
			t.Errorf("expected indices [1, 2], got %v", indices)
		}

		if len(selected) != 2 || selected[0].ID != "2" || selected[1].ID != "3" {
			t.Errorf("expected candidate IDs ['2', '3'], got %v", selected)
		}
	})

	t.Run("returns empty when all below threshold", func(t *testing.T) {
		candidates := []llm.MessageInput{{RawMessage: db.RawMessage{ID: "1"}}}

		results := []llm.BatchResult{{ImportanceScore: 0.5}}

		indices, selected := selectTieredCandidates(candidates, results, nil)

		if len(indices) != 0 || len(selected) != 0 {
			t.Errorf("expected empty results, got %d indices", len(indices))
		}
	})
}

func TestApplyTieredResults(t *testing.T) {
	results := []llm.BatchResult{
		{ImportanceScore: 0.5, Summary: "Original 1"},
		{ImportanceScore: 0.85, Summary: "Original 2"},
		{ImportanceScore: 0.9, Summary: "Original 3"},
	}

	tieredResults := []llm.BatchResult{
		{ImportanceScore: 0.95, Summary: "Tiered 2"},
	}

	tieredIndices := []int{1}

	applyTieredResults(results, tieredResults, tieredIndices)

	if results[0].Summary != "Original 1" {
		t.Errorf("result[0] should be unchanged, got %q", results[0].Summary)
	}

	if results[1].Summary != "Tiered 2" {
		t.Errorf("result[1] should be updated, got %q", results[1].Summary)
	}

	if results[2].Summary != "Original 3" {
		t.Errorf("result[2] should be unchanged, got %q", results[2].Summary)
	}
}

func TestEvaluateRelevanceGateHeuristic(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		wantDecision   string
		wantConfidence float32
		wantReason     string
	}{
		{
			name:           "empty text",
			text:           "",
			wantDecision:   DecisionIrrelevant,
			wantConfidence: ConfidenceEmpty,
			wantReason:     ReasonEmpty,
		},
		{
			name:           "whitespace only",
			text:           "   \t\n  ",
			wantDecision:   DecisionIrrelevant,
			wantConfidence: ConfidenceEmpty,
			wantReason:     ReasonEmpty,
		},
		{
			name:           "link only",
			text:           "https://example.com/news",
			wantDecision:   DecisionIrrelevant,
			wantConfidence: ConfidenceLinkOnly,
			wantReason:     ReasonLinkOnly,
		},
		{
			name:           "telegram link only",
			text:           "t.me/channel/123",
			wantDecision:   DecisionIrrelevant,
			wantConfidence: ConfidenceLinkOnly,
			wantReason:     ReasonLinkOnly,
		},
		{
			name:           "only symbols no alphanumeric",
			text:           "!!! ??? ---",
			wantDecision:   DecisionIrrelevant,
			wantConfidence: ConfidenceNoText,
			wantReason:     ReasonNoText,
		},
		{
			name:           "valid text",
			text:           "Breaking news about technology",
			wantDecision:   DecisionRelevant,
			wantConfidence: ConfidencePassed,
			wantReason:     ReasonPassed,
		},
		{
			name:           "text with link",
			text:           "Check out this news https://example.com",
			wantDecision:   DecisionRelevant,
			wantConfidence: ConfidencePassed,
			wantReason:     ReasonPassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateRelevanceGateHeuristic(tt.text)

			if got.decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q", got.decision, tt.wantDecision)
			}

			if got.confidence != tt.wantConfidence {
				t.Errorf("confidence = %v, want %v", got.confidence, tt.wantConfidence)
			}

			if got.reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", got.reason, tt.wantReason)
			}
		})
	}
}

func TestHasAlphaNum(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "has letters",
			s:    "abc",
			want: true,
		},
		{
			name: "has numbers",
			s:    "123",
			want: true,
		},
		{
			name: "mixed alphanumeric",
			s:    "abc123",
			want: true,
		},
		{
			name: "only symbols",
			s:    "!@#$%",
			want: false,
		},
		{
			name: "empty string",
			s:    "",
			want: false,
		},
		{
			name: "only spaces",
			s:    "   ",
			want: false,
		},
		{
			name: "cyrillic letters",
			s:    "привет",
			want: true,
		},
		{
			name: "unicode numbers",
			s:    "٤٥٦", // Arabic-Indic digits
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAlphaNum(tt.s); got != tt.want {
				t.Errorf("hasAlphaNum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupIndicesByModel(t *testing.T) {
	tests := []struct {
		name           string
		candidates     []llm.MessageInput
		llmModel       string
		expectedGroups map[string][]int
	}{
		{
			name: "all same model without vision routing",
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ID: "1"}},
				{RawMessage: db.RawMessage{ID: "2"}},
				{RawMessage: db.RawMessage{ID: "3"}},
			},
			llmModel:       "gpt-4o-mini",
			expectedGroups: map[string][]int{"": {0, 1, 2}},
		},
		{
			name: "media does not change model",
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ID: "1"}},
				{RawMessage: db.RawMessage{ID: "2", MediaData: []byte(image)}},
				{RawMessage: db.RawMessage{ID: "3"}},
			},
			llmModel:       "gpt-4o-mini",
			expectedGroups: map[string][]int{"": {0, 1, 2}},
		},
		{
			name: "empty model uses default",
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ID: "1"}},
			},
			llmModel:       "",
			expectedGroups: map[string][]int{"": {0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{LLMModel: tt.llmModel}

			p := New(cfg, nil, nil, nil, nil, nil, nil)

			result := p.groupIndicesByModel(tt.candidates, nil)

			if len(result) != len(tt.expectedGroups) {
				t.Errorf("expected %d groups, got %d", len(tt.expectedGroups), len(result))
			}

			for model, expectedIndices := range tt.expectedGroups {
				actualIndices, ok := result[model]

				if !ok {
					t.Errorf("expected group for model %q not found", model)
					continue
				}

				if len(actualIndices) != len(expectedIndices) {
					t.Errorf("model %q: expected %d indices, got %d", model, len(expectedIndices), len(actualIndices))
					continue
				}

				for i, idx := range expectedIndices {
					if actualIndices[i] != idx {
						t.Errorf("model %q: index %d = %d, want %d", model, i, actualIndices[i], idx)
					}
				}
			}
		})
	}
}

func TestDetermineStatus(t *testing.T) {
	tests := []struct {
		name                    string
		relevanceScore          float32
		relevanceThreshold      float32
		channelRelevance        float32
		autoRelevanceEnabled    bool
		relevanceThresholdDelta float32
		expectedStatus          string
	}{
		{
			name:               "above threshold returns ready",
			relevanceScore:     0.8,
			relevanceThreshold: 0.5,
			expectedStatus:     StatusReady,
		},
		{
			name:               "below threshold returns rejected",
			relevanceScore:     0.3,
			relevanceThreshold: 0.5,
			expectedStatus:     StatusRejected,
		},
		{
			name:               "equal to threshold returns ready",
			relevanceScore:     0.5,
			relevanceThreshold: 0.5,
			expectedStatus:     StatusReady,
		},
		{
			name:               "channel specific threshold overrides default",
			relevanceScore:     0.6,
			relevanceThreshold: 0.5,
			channelRelevance:   0.7,
			expectedStatus:     StatusRejected,
		},
		{
			name:                    "auto relevance adds delta",
			relevanceScore:          0.65,
			relevanceThreshold:      0.5,
			autoRelevanceEnabled:    true,
			relevanceThresholdDelta: 0.2,
			expectedStatus:          StatusRejected, // 0.5 + 0.2 = 0.7 > 0.65
		},
		{
			name:                    "threshold clamped to 0",
			relevanceScore:          0.1,
			relevanceThreshold:      0.5,
			autoRelevanceEnabled:    true,
			relevanceThresholdDelta: -0.6, // would make it -0.1
			expectedStatus:          StatusReady,
		},
		{
			name:                    "threshold clamped to 1",
			relevanceScore:          0.95,
			relevanceThreshold:      0.8,
			autoRelevanceEnabled:    true,
			relevanceThresholdDelta: 0.5, // would make it 1.3
			expectedStatus:          StatusRejected,
		},
	}

	cfg := &config.Config{}

	p := New(cfg, nil, nil, nil, nil, nil, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &pipelineSettings{
				relevanceThreshold: tt.relevanceThreshold,
			}

			c := llm.MessageInput{
				RawMessage: db.RawMessage{
					RelevanceThreshold:      tt.channelRelevance,
					AutoRelevanceEnabled:    tt.autoRelevanceEnabled,
					RelevanceThresholdDelta: tt.relevanceThresholdDelta,
				},
			}

			status := p.determineStatus(c, tt.relevanceScore, s)

			if status != tt.expectedStatus {
				t.Errorf("determineStatus() = %q, want %q", status, tt.expectedStatus)
			}
		})
	}
}

func TestCalculateImportance(t *testing.T) {
	tests := []struct {
		name             string
		importanceWeight float32
		llmImportance    float32
		summary          string
		expectedMin      float32
		expectedMax      float32
	}{
		{
			name:             "normal weight with unique info",
			importanceWeight: 1.0,
			llmImportance:    0.8,
			summary:          "John announced new features on Monday",
			expectedMin:      0.79,
			expectedMax:      0.81,
		},
		{
			name:             "weight below minimum defaults to 1.0",
			importanceWeight: 0.05, // below MinChannelWeight (0.1)
			llmImportance:    0.7,
			summary:          "Company reported 25% growth",
			expectedMin:      0.69,
			expectedMax:      0.71,
		},
		{
			name:             "weight above maximum clamped to 2.0",
			importanceWeight: 3.0,
			llmImportance:    0.4,
			summary:          "Breaking news from January",
			expectedMin:      0.79,
			expectedMax:      0.81, // 0.4 * 2.0 = 0.8
		},
		{
			name:             "result capped at 1.0",
			importanceWeight: 2.0,
			llmImportance:    0.9,
			summary:          "Major update on Tuesday",
			expectedMin:      0.99,
			expectedMax:      1.01, // 0.9 * 2.0 = 1.8, capped to 1.0
		},
		{
			name:             "penalty for no unique info",
			importanceWeight: 1.0,
			llmImportance:    0.5,
			summary:          "something happened somewhere",
			expectedMin:      0.29,
			expectedMax:      0.31, // 0.5 - 0.2 = 0.3
		},
		{
			name:             "penalty does not go below zero",
			importanceWeight: 1.0,
			llmImportance:    0.1,
			summary:          "generic stuff",
			expectedMin:      0.0,
			expectedMax:      0.01, // 0.1 - 0.2 = -0.1, clamped to 0
		},
	}

	cfg := &config.Config{}

	logger := zerolog.Nop()

	p := New(cfg, nil, nil, nil, nil, nil, &logger)

	s := &pipelineSettings{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := llm.MessageInput{
				RawMessage: db.RawMessage{
					ID:               testID,
					ImportanceWeight: tt.importanceWeight,
				},
			}

			res := llm.BatchResult{
				ImportanceScore: tt.llmImportance,
				Summary:         tt.summary,
			}

			got := p.calculateImportance(logger, c, res, s)

			if got < tt.expectedMin || got > tt.expectedMax {
				t.Errorf("calculateImportance() = %v, want between %v and %v", got, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestNormalizeResults(t *testing.T) {
	tests := []struct {
		name               string
		normalizeScores    bool
		channelStats       map[string]db.ChannelStats
		candidates         []llm.MessageInput
		results            []llm.BatchResult
		expectedRelevance  []float32
		expectedImportance []float32
	}{
		{
			name:            "normalization disabled",
			normalizeScores: false,
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ChannelID: "ch1"}},
			},
			results: []llm.BatchResult{
				{RelevanceScore: 0.8, ImportanceScore: 0.6, Summary: "test"},
			},
			expectedRelevance:  []float32{0.8},
			expectedImportance: []float32{0.6},
		},
		{
			name:            "normalization enabled with stats",
			normalizeScores: true,
			channelStats: map[string]db.ChannelStats{
				"ch1": {AvgRelevance: 0.5, StddevRelevance: 0.2, AvgImportance: 0.4, StddevImportance: 0.2},
			},
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ChannelID: "ch1"}},
			},
			results: []llm.BatchResult{
				{RelevanceScore: 0.7, ImportanceScore: 0.6, Summary: "test"},
			},
			expectedRelevance:  []float32{1.0}, // (0.7 - 0.5) / 0.2 = 1.0
			expectedImportance: []float32{1.0}, // (0.6 - 0.4) / 0.2 = 1.0
		},
		{
			name:            "no stats for channel",
			normalizeScores: true,
			channelStats: map[string]db.ChannelStats{
				"ch2": {AvgRelevance: 0.5, StddevRelevance: 0.2, AvgImportance: 0.4, StddevImportance: 0.2},
			},
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ChannelID: "ch1"}},
			},
			results: []llm.BatchResult{
				{RelevanceScore: 0.8, ImportanceScore: 0.6, Summary: "test"},
			},
			expectedRelevance:  []float32{0.8}, // unchanged
			expectedImportance: []float32{0.6}, // unchanged
		},
		{
			name:            "stddev below minimum skips normalization",
			normalizeScores: true,
			channelStats: map[string]db.ChannelStats{
				"ch1": {AvgRelevance: 0.5, StddevRelevance: 0.005, AvgImportance: 0.4, StddevImportance: 0.005},
			},
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ChannelID: "ch1"}},
			},
			results: []llm.BatchResult{
				{RelevanceScore: 0.8, ImportanceScore: 0.6, Summary: "test"},
			},
			expectedRelevance:  []float32{0.8}, // unchanged due to low stddev
			expectedImportance: []float32{0.6}, // unchanged due to low stddev
		},
		{
			name:            "empty summary skipped",
			normalizeScores: true,
			channelStats: map[string]db.ChannelStats{
				"ch1": {AvgRelevance: 0.5, StddevRelevance: 0.2, AvgImportance: 0.4, StddevImportance: 0.2},
			},
			candidates: []llm.MessageInput{
				{RawMessage: db.RawMessage{ChannelID: "ch1"}},
			},
			results: []llm.BatchResult{
				{RelevanceScore: 0.8, ImportanceScore: 0.6, Summary: ""},
			},
			expectedRelevance:  []float32{0.8}, // unchanged
			expectedImportance: []float32{0.6}, // unchanged
		},
	}

	cfg := &config.Config{}

	p := New(cfg, nil, nil, nil, nil, nil, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &pipelineSettings{
				normalizeScores: tt.normalizeScores,
				channelStats:    tt.channelStats,
			}

			p.normalizeResults(tt.candidates, tt.results, s)

			for i := range tt.results {
				if tt.results[i].RelevanceScore < tt.expectedRelevance[i]-0.01 ||
					tt.results[i].RelevanceScore > tt.expectedRelevance[i]+0.01 {
					t.Errorf("result[%d].RelevanceScore = %v, want %v", i, tt.results[i].RelevanceScore, tt.expectedRelevance[i])
				}

				if tt.results[i].ImportanceScore < tt.expectedImportance[i]-0.01 ||
					tt.results[i].ImportanceScore > tt.expectedImportance[i]+0.01 {
					t.Errorf("result[%d].ImportanceScore = %v, want %v", i, tt.results[i].ImportanceScore, tt.expectedImportance[i])
				}
			}
		})
	}
}

func TestEvaluateRelevanceGate(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		gateMode     string
		gateEnabled  bool
		wantDecision string
	}{
		{
			name:         "heuristic mode with empty text",
			text:         "",
			gateMode:     "heuristic",
			gateEnabled:  true,
			wantDecision: DecisionIrrelevant,
		},
		{
			name:         "heuristic mode with valid text",
			text:         "Breaking news about technology",
			gateMode:     "heuristic",
			gateEnabled:  true,
			wantDecision: DecisionRelevant,
		},
		{
			name:         "empty mode defaults to heuristic",
			text:         "Valid message content",
			gateMode:     "",
			gateEnabled:  true,
			wantDecision: DecisionRelevant,
		},
		{
			name:         "hybrid mode with irrelevant heuristic",
			text:         "",
			gateMode:     "hybrid",
			gateEnabled:  true,
			wantDecision: DecisionIrrelevant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}

			repo := &mockRepo{settings: make(map[string]interface{})}

			llmClient := &mockLLM{}

			logger := zerolog.Nop()

			p := New(cfg, repo, llmClient, nil, nil, nil, &logger)

			s := &pipelineSettings{
				relevanceGateEnabled: tt.gateEnabled,
				relevanceGateMode:    tt.gateMode,
			}

			decision := p.evaluateRelevanceGate(context.Background(), logger, tt.text, s)

			if decision.decision != tt.wantDecision {
				t.Errorf("evaluateRelevanceGate() decision = %q, want %q", decision.decision, tt.wantDecision)
			}
		})
	}
}

func TestEvaluateGateLLM(t *testing.T) {
	tests := []struct {
		name                 string
		relevanceGateModel   string
		cfgModel             string
		mockRelevanceGateErr error
		mockDecision         string
		mockConfidence       float32
		expectOK             bool
		expectedModel        string
	}{
		{
			name:               "uses relevance gate model when set",
			relevanceGateModel: "gpt-4",
			cfgModel:           "gpt-4o-mini",
			mockDecision:       DecisionRelevant,
			mockConfidence:     0.8,
			expectOK:           true,
			expectedModel:      "gpt-4",
		},
		{
			name:               "falls back to config model",
			relevanceGateModel: "",
			cfgModel:           "gpt-4o",
			mockDecision:       DecisionRelevant,
			mockConfidence:     0.9,
			expectOK:           true,
			expectedModel:      "gpt-4o",
		},
		{
			name:               "returns false when no model available",
			relevanceGateModel: "",
			cfgModel:           "",
			expectOK:           false,
		},
		{
			name:                 "returns false on LLM error",
			relevanceGateModel:   "gpt-4",
			mockRelevanceGateErr: errLLM,
			expectOK:             false,
		},
		{
			name:               "clamps confidence above 1",
			relevanceGateModel: "gpt-4",
			mockDecision:       DecisionRelevant,
			mockConfidence:     1.5,
			expectOK:           true,
		},
		{
			name:               "clamps confidence below 0",
			relevanceGateModel: "gpt-4",
			mockDecision:       DecisionRelevant,
			mockConfidence:     -0.5,
			expectOK:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{LLMModel: tt.cfgModel}

			repo := &mockRepo{settings: make(map[string]interface{})}

			llmClient := &mockLLMForGate{
				decision:   tt.mockDecision,
				confidence: tt.mockConfidence,
				err:        tt.mockRelevanceGateErr,
			}

			logger := zerolog.Nop()

			p := New(cfg, repo, llmClient, nil, nil, nil, &logger)

			s := &pipelineSettings{
				relevanceGateModel: tt.relevanceGateModel,
			}

			decision, ok := p.evaluateGateLLM(context.Background(), logger, "test text", s)

			if ok != tt.expectOK {
				t.Errorf("evaluateGateLLM() ok = %v, want %v", ok, tt.expectOK)
			}

			if tt.expectOK && tt.expectedModel != "" && decision.model != tt.expectedModel {
				t.Errorf("evaluateGateLLM() model = %q, want %q", decision.model, tt.expectedModel)
			}
		})
	}
}

type mockLLMForGate struct {
	llm.Client
	decision   string
	confidence float32
	err        error
}

func (m *mockLLMForGate) RelevanceGate(_ context.Context, _, _, _ string) (llm.RelevanceGateResult, error) {
	if m.err != nil {
		return llm.RelevanceGateResult{}, m.err
	}

	return llm.RelevanceGateResult{
		Decision:   m.decision,
		Confidence: m.confidence,
		Reason:     "mock reason",
	}, nil
}

func TestLoadGatePrompt(t *testing.T) {
	tests := []struct {
		name            string
		activeVersion   string
		promptOverride  string
		expectedVersion string
		expectedPrompt  string
	}{
		{
			name:            "default prompt when no settings",
			expectedVersion: "v1",
			expectedPrompt:  defaultGatePrompt,
		},
		{
			name:            "uses active version",
			activeVersion:   "v2",
			expectedVersion: "v2",
			expectedPrompt:  defaultGatePrompt,
		},
		{
			name:            "uses prompt override",
			activeVersion:   "v2",
			promptOverride:  "Custom prompt",
			expectedVersion: "v2",
			expectedPrompt:  "Custom prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := make(map[string]interface{})

			if tt.activeVersion != "" {
				settings[gatePromptActiveKey] = tt.activeVersion
			}

			if tt.promptOverride != "" {
				settings[gatePromptVersionPrefix+tt.activeVersion] = tt.promptOverride
			}

			cfg := &config.Config{}

			repo := &mockRepo{settings: settings}

			logger := zerolog.Nop()

			p := New(cfg, repo, nil, nil, nil, nil, &logger)

			prompt, version := p.loadGatePrompt(context.Background(), logger)

			if version != tt.expectedVersion {
				t.Errorf("loadGatePrompt() version = %q, want %q", version, tt.expectedVersion)
			}

			if prompt != tt.expectedPrompt {
				t.Errorf("loadGatePrompt() prompt = %q, want %q", prompt, tt.expectedPrompt)
			}
		})
	}
}

func TestCreateItem(t *testing.T) {
	tests := []struct {
		name           string
		relevanceScore float32
		threshold      float32
		expectedStatus string
	}{
		{
			name:           "above threshold returns ready",
			relevanceScore: 0.8,
			threshold:      0.5,
			expectedStatus: StatusReady,
		},
		{
			name:           "below threshold returns rejected",
			relevanceScore: 0.3,
			threshold:      0.5,
			expectedStatus: StatusRejected,
		},
	}

	cfg := &config.Config{}

	logger := zerolog.Nop()

	p := New(cfg, nil, nil, nil, nil, nil, &logger)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := llm.MessageInput{
				RawMessage: db.RawMessage{
					ID:               testID,
					ImportanceWeight: 1.0,
				},
			}

			res := llm.BatchResult{
				RelevanceScore:  tt.relevanceScore,
				ImportanceScore: 0.8,
				Topic:           tech,
				Summary:         "Test summary with John",
				Language:        "en",
			}

			s := &pipelineSettings{
				relevanceThreshold: tt.threshold,
			}

			item := p.createItem(logger, c, res, s)

			if item.Status != tt.expectedStatus {
				t.Errorf("createItem() status = %q, want %q", item.Status, tt.expectedStatus)
			}

			if item.RawMessageID != testID {
				t.Errorf("createItem() RawMessageID = %q, want %q", item.RawMessageID, testID)
			}

			if item.Topic != tech {
				t.Errorf("createItem() Topic = %q, want %q", item.Topic, tech)
			}
		})
	}
}

func TestGetDurationSetting(t *testing.T) {
	tests := []struct {
		name         string
		settingValue string
		defaultVal   time.Duration
		expected     time.Duration
	}{
		{
			name:         "valid duration string",
			settingValue: "30s",
			defaultVal:   10 * time.Second,
			expected:     30 * time.Second,
		},
		{
			name:         "invalid duration uses default",
			settingValue: "invalid",
			defaultVal:   15 * time.Second,
			expected:     15 * time.Second,
		},
		{
			name:         "empty uses default",
			settingValue: "",
			defaultVal:   20 * time.Second,
			expected:     20 * time.Second,
		},
		{
			name:         "minutes duration",
			settingValue: "5m",
			defaultVal:   1 * time.Minute,
			expected:     5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := make(map[string]interface{})

			if tt.settingValue != "" {
				settings["test_duration"] = tt.settingValue
			}

			cfg := &config.Config{}

			repo := &mockRepo{settings: settings}

			logger := zerolog.Nop()

			p := New(cfg, repo, nil, nil, nil, nil, &logger)

			result := p.getDurationSetting(context.Background(), "test_duration", tt.defaultVal, logger)

			if result != tt.expected {
				t.Errorf("getDurationSetting() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSkipMessage(t *testing.T) {
	tests := []struct {
		name                  string
		message               db.RawMessage
		skipForwards          bool
		linkEnrichmentEnabled bool
		seenHashes            map[string]string
		expectSkip            bool
	}{
		{
			name:       "duplicate hash in batch",
			message:    db.RawMessage{ID: "2", CanonicalHash: "hash1", Text: "Long enough text for filter"},
			seenHashes: map[string]string{"hash1": "1"},
			expectSkip: true,
		},
		{
			name:         "forwarded message when skip enabled",
			message:      db.RawMessage{ID: "1", CanonicalHash: "hash1", Text: "Long enough text for filter", IsForward: true},
			skipForwards: true,
			seenHashes:   make(map[string]string),
			expectSkip:   true,
		},
		{
			name:         "forwarded message when skip disabled",
			message:      db.RawMessage{ID: "1", CanonicalHash: "hash1", Text: "Long enough text for filter", IsForward: true},
			skipForwards: false,
			seenHashes:   make(map[string]string),
			expectSkip:   false,
		},
		{
			name:       "normal message passes",
			message:    db.RawMessage{ID: "1", CanonicalHash: "hash1", Text: "Long enough text for filter"},
			seenHashes: make(map[string]string),
			expectSkip: false,
		},
		{
			name:                  "short message with link passes when enrichment enabled",
			message:               db.RawMessage{ID: "1", Text: "https://t.me/1"},
			linkEnrichmentEnabled: true,
			seenHashes:            make(map[string]string),
			expectSkip:            false,
		},
		{
			name:                  "short message with link skipped when enrichment disabled",
			message:               db.RawMessage{ID: "1", Text: "https://t.me/1"},
			linkEnrichmentEnabled: false,
			seenHashes:            make(map[string]string),
			expectSkip:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}

			repo := &mockRepo{settings: make(map[string]interface{})}

			logger := zerolog.Nop()

			p := New(cfg, repo, nil, nil, nil, nil, &logger)

			s := &pipelineSettings{
				skipForwards:          tt.skipForwards,
				linkEnrichmentEnabled: tt.linkEnrichmentEnabled,
				minLengthDefault:      20,
			}

			f := filters.New(nil, false, 20, nil, "mixed")

			skip := p.skipMessageBasic(context.Background(), logger, &tt.message, s, tt.seenHashes, f)

			if skip != tt.expectSkip {
				t.Errorf("skipMessage() = %v, want %v", skip, tt.expectSkip)
			}
		})
	}
}
