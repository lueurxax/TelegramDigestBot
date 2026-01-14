package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const testTieredModel = "gpt-4o"

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

func (m *mockRepo) CheckStrictDuplicate(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (m *mockRepo) FindSimilarItem(_ context.Context, _ []float32, _ float32) (string, error) {
	return "", nil
}

func (m *mockRepo) LinkMessageToLink(_ context.Context, _, _ string, _ int) error {
	return nil
}

type mockLLM struct {
	llm.Client
}

func (m *mockLLM) GetEmbedding(_ context.Context, text string) ([]float32, error) {
	if text == "Message 1 that is long enough to pass filters" {
		return []float32{1.0, 0.0}, nil
	}

	return []float32{0.0, 1.0}, nil
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
	logger := zerolog.Nop()

	p := New(cfg, repo, llmClient, nil, &logger)

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

func (m *mockLLMWithImportance) GetEmbedding(_ context.Context, _ string) ([]float32, error) {
	return []float32{1.0, 0.0}, nil
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
	p := New(cfg, nil, nil, nil, nil)

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
	t.Run("selects high importance non-smart candidates", func(t *testing.T) {
		candidates := []llm.MessageInput{
			{RawMessage: db.RawMessage{ID: "1"}},
			{RawMessage: db.RawMessage{ID: "2"}},
			{RawMessage: db.RawMessage{ID: "3"}},
		}
		results := []llm.BatchResult{
			{ImportanceScore: 0.5},  // below threshold
			{ImportanceScore: 0.85}, // above threshold
			{ImportanceScore: 0.9},  // above threshold, already smart model
		}
		modelUsed := []string{"gpt-4o-mini", "gpt-4o-mini", testTieredModel}

		indices, selected := selectTieredCandidates(candidates, results, modelUsed, testTieredModel)

		if len(indices) != 1 {
			t.Fatalf("expected 1 selected, got %d", len(indices))
		}

		if indices[0] != 1 {
			t.Errorf("expected index 1, got %d", indices[0])
		}

		if len(selected) != 1 || selected[0].ID != "2" {
			t.Errorf("expected candidate ID '2', got %v", selected)
		}
	})

	t.Run("returns empty when all below threshold", func(t *testing.T) {
		candidates := []llm.MessageInput{{RawMessage: db.RawMessage{ID: "1"}}}
		results := []llm.BatchResult{{ImportanceScore: 0.5}}
		modelUsed := []string{"gpt-4o-mini"}

		indices, selected := selectTieredCandidates(candidates, results, modelUsed, testTieredModel)

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
