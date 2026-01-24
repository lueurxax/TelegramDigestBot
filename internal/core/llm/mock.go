package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

// mockProvider implements the Provider interface for testing purposes.
type mockProvider struct {
	cfg *config.Config
}

// NewMockProvider creates a new mock LLM provider.
func NewMockProvider(cfg *config.Config) *mockProvider {
	return &mockProvider{cfg: cfg}
}

// Name returns the provider identifier.
func (p *mockProvider) Name() ProviderName {
	return ProviderMock
}

// IsAvailable returns true as mock is always available.
func (p *mockProvider) IsAvailable() bool {
	return true
}

// Priority returns the provider priority.
func (p *mockProvider) Priority() int {
	return PriorityMock
}

// SupportsImageGeneration returns false for mock provider.
func (p *mockProvider) SupportsImageGeneration() bool {
	return false
}

// ProcessBatch implements Provider interface.
func (p *mockProvider) ProcessBatch(_ context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
	// Construct prompt (simplified) for logging purposes
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

// TranslateText implements Provider interface.
func (p *mockProvider) TranslateText(_ context.Context, text, _, _ string) (string, error) {
	return text, nil
}

// CompleteText implements Provider interface.
func (p *mockProvider) CompleteText(_ context.Context, prompt, _ string) (string, error) {
	if strings.Contains(prompt, "JSON array") {
		return `[{"text": "Mock claim", "entities": [{"text": "Mock entity", "type": "ORG"}]}]`, nil
	}

	return "Mock response", nil
}

// GenerateNarrative implements Provider interface.
func (p *mockProvider) GenerateNarrative(_ context.Context, items []domain.Item, _, _, tone string) (string, error) {
	return "This is a mock cohesive narrative of the latest news based on " + fmt.Sprint(len(items)) + " items in " + tone + " tone.", nil
}

// GenerateNarrativeWithEvidence implements Provider interface.
func (p *mockProvider) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, _ ItemEvidence, targetLanguage, model, tone string) (string, error) {
	return p.GenerateNarrative(ctx, items, targetLanguage, model, tone)
}

// SummarizeCluster implements Provider interface.
func (p *mockProvider) SummarizeCluster(_ context.Context, items []domain.Item, _, _, tone string) (string, error) {
	return "This is a mock consolidated summary of " + fmt.Sprint(len(items)) + " related items in " + tone + " tone.", nil
}

// SummarizeClusterWithEvidence implements Provider interface.
func (p *mockProvider) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, _ ItemEvidence, targetLanguage, model, tone string) (string, error) {
	return p.SummarizeCluster(ctx, items, targetLanguage, model, tone)
}

// GenerateClusterTopic implements Provider interface.
func (p *mockProvider) GenerateClusterTopic(_ context.Context, items []domain.Item, _, _ string) (string, error) {
	if len(items) > 0 {
		return items[0].Topic, nil
	}

	return DefaultTopic, nil
}

// RelevanceGate implements Provider interface.
func (p *mockProvider) RelevanceGate(_ context.Context, _, _, _ string) (RelevanceGateResult, error) {
	return RelevanceGateResult{
		Decision:   "relevant",
		Confidence: mockConfidenceScore,
		Reason:     "mock",
	}, nil
}

// CompressSummariesForCover implements Provider interface.
func (p *mockProvider) CompressSummariesForCover(_ context.Context, summaries []string) ([]string, error) {
	return summaries, nil
}

// GenerateDigestCover returns nil for mock provider.
func (p *mockProvider) GenerateDigestCover(_ context.Context, _ []string, _ string) ([]byte, error) {
	return nil, nil
}

// Ensure mockProvider implements Provider interface.
var _ Provider = (*mockProvider)(nil)
