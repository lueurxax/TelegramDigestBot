package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

type BatchResult struct {
	Index           int       `json:"index"`
	RelevanceScore  float32   `json:"relevance_score"`
	ImportanceScore float32   `json:"importance_score"`
	Topic           string    `json:"topic"`
	Summary         string    `json:"summary"`
	Language        string    `json:"language"`
	SourceChannel   string    `json:"source_channel"` // Echo back the source channel name for verification
	Embedding       []float32 `json:"-"`
}

type MessageInput struct {
	domain.RawMessage
	Context       []string
	ResolvedLinks []domain.ResolvedLink
}

// EvidenceSource represents evidence from external sources for context injection.
type EvidenceSource struct {
	URL             string
	Domain          string
	Title           string
	Description     string // Added for "Background" context
	AgreementScore  float32
	IsContradiction bool
}

// ItemEvidence maps item IDs to their associated evidence sources.
type ItemEvidence map[string][]EvidenceSource

type Client interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
	ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage string, model string, tone string) ([]BatchResult, error)
	TranslateText(ctx context.Context, text string, targetLanguage string, model string) (string, error)
	GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage string, model string, tone string) (string, error)
	GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage string, model string, tone string) (string, error)
	SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage string, model string, tone string) (string, error)
	SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage string, model string, tone string) (string, error)
	GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage string, model string) (string, error)
	RelevanceGate(ctx context.Context, text string, model string, prompt string) (RelevanceGateResult, error)
	CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error)
	GenerateDigestCover(ctx context.Context, topics []string, narrative string) ([]byte, error)
}

type RelevanceGateResult struct {
	Decision   string  `json:"decision"`
	Confidence float32 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type PromptStore interface {
	GetSetting(ctx context.Context, key string, target interface{}) error
}

type mockClient struct {
	cfg *config.Config
}

func New(cfg *config.Config, store PromptStore, logger *zerolog.Logger) Client {
	if cfg.LLMAPIKey == "" || cfg.LLMAPIKey == "mock" {
		return &mockClient{cfg: cfg}
	}

	return NewOpenAI(cfg, store, logger)
}

func (c *mockClient) GetEmbedding(_ context.Context, _ string) ([]float32, error) {
	// Mock embedding (dimensions as in schema)
	emb := make([]float32, mockEmbeddingDimensions)
	// Fill with some deterministic values based on text for mock consistency
	for i := 0; i < len(emb); i++ {
		emb[i] = 0.1
	}

	return emb, nil
}

func (c *mockClient) ProcessBatch(_ context.Context, messages []MessageInput, targetLanguage string, model string, tone string) ([]BatchResult, error) {
	// In a real implementation, this would call OpenAI or another LLM provider.
	// Construct prompt (simplified)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Summarize and score these Telegram messages in JSON format (target language: %s, model: %s, tone: %s):\n", targetLanguage, model, tone))

	for i, m := range messages {
		sb.WriteString(fmt.Sprintf("%d. [Context: %v] %s\n", i, m.Context, m.Text))
	}

	// Mocking LLM response
	results := make([]BatchResult, len(messages))
	for i := range messages {
		results[i] = BatchResult{
			Index:           i,
			RelevanceScore:  mockRelevanceScore,
			ImportanceScore: mockImportanceScore,
			Topic:           DefaultTopic,
			Summary:         "This is a summary of the message.",
			Language:        "en",
			SourceChannel:   messages[i].ChannelTitle,
		}
	}

	return results, nil
}

func (c *mockClient) TranslateText(_ context.Context, text string, _ string, _ string) (string, error) {
	return text, nil
}

func (c *mockClient) GenerateNarrative(_ context.Context, items []domain.Item, _ string, _ string, tone string) (string, error) {
	return "This is a mock cohesive narrative of the latest news based on " + fmt.Sprint(len(items)) + " items in " + tone + " tone.", nil
}

func (c *mockClient) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, _ ItemEvidence, targetLanguage string, model string, tone string) (string, error) {
	return c.GenerateNarrative(ctx, items, targetLanguage, model, tone)
}

func (c *mockClient) SummarizeCluster(_ context.Context, items []domain.Item, _ string, _ string, tone string) (string, error) {
	return "This is a mock consolidated summary of " + fmt.Sprint(len(items)) + " related items in " + tone + " tone.", nil
}

func (c *mockClient) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, _ ItemEvidence, targetLanguage string, model string, tone string) (string, error) {
	return c.SummarizeCluster(ctx, items, targetLanguage, model, tone)
}

func (c *mockClient) GenerateClusterTopic(_ context.Context, items []domain.Item, _ string, _ string) (string, error) {
	if len(items) > 0 {
		return items[0].Topic, nil
	}

	return DefaultTopic, nil
}

func (c *mockClient) RelevanceGate(_ context.Context, _ string, _ string, _ string) (RelevanceGateResult, error) {
	return RelevanceGateResult{
		Decision:   "relevant",
		Confidence: mockConfidenceScore,
		Reason:     "mock",
	}, nil
}

func (c *mockClient) CompressSummariesForCover(_ context.Context, summaries []string) ([]string, error) {
	// Mock returns summaries as-is
	return summaries, nil
}

func (c *mockClient) GenerateDigestCover(_ context.Context, _ []string, _ string) ([]byte, error) {
	// Mock returns nil - no image generated
	return nil, nil
}
