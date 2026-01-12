package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
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
	db.RawMessage
	ChannelTitle        string
	ChannelContext      string
	ChannelDescription  string
	ChannelCategory     string
	ChannelTone         string
	ChannelUpdateFreq   string
	RelevanceThreshold  float32
	ImportanceThreshold float32
	ImportanceWeight    float32
	Context             []string
	ResolvedLinks       []db.ResolvedLink
}

type Client interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
	ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage string, model string, tone string) ([]BatchResult, error)
	GenerateNarrative(ctx context.Context, items []db.Item, targetLanguage string, model string, tone string) (string, error)
	SummarizeCluster(ctx context.Context, items []db.Item, targetLanguage string, model string, tone string) (string, error)
	GenerateClusterTopic(ctx context.Context, items []db.Item, targetLanguage string, model string) (string, error)
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

func (c *mockClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Mock embedding (dimensions as in schema)
	emb := make([]float32, mockEmbeddingDimensions)
	// Fill with some deterministic values based on text for mock consistency
	for i := 0; i < len(emb); i++ {
		emb[i] = 0.1
	}

	return emb, nil
}

func (c *mockClient) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage string, model string, tone string) ([]BatchResult, error) {
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

func (c *mockClient) GenerateNarrative(ctx context.Context, items []db.Item, targetLanguage string, model string, tone string) (string, error) {
	return "This is a mock cohesive narrative of the latest news based on " + fmt.Sprint(len(items)) + " items in " + tone + " tone.", nil
}

func (c *mockClient) SummarizeCluster(ctx context.Context, items []db.Item, targetLanguage string, model string, tone string) (string, error) {
	return "This is a mock consolidated summary of " + fmt.Sprint(len(items)) + " related items in " + tone + " tone.", nil
}

func (c *mockClient) GenerateClusterTopic(ctx context.Context, items []db.Item, targetLanguage string, model string) (string, error) {
	if len(items) > 0 {
		return items[0].Topic, nil
	}

	return DefaultTopic, nil
}
