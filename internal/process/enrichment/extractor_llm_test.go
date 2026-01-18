package enrichment

import (
	"context"
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

type mockLLMClient struct {
	llm.Client
	response string
	err      error
}

func (m *mockLLMClient) CompleteText(_ context.Context, _, _ string) (string, error) {
	return m.response, m.err
}

const errFmtClaims = "expected %d claims, got %d"

func TestExtractor_ExtractClaimsWithLLM_Robustness(t *testing.T) {
	tests := []struct {
		name        string
		llmResponse string
		expectError bool
		expectCount int
	}{
		{
			name:        "perfect JSON",
			llmResponse: `[{"text": "Claim 1", "entities": []}]`,
			expectCount: 1,
		},
		{
			name: "postamble with brackets and cyrillic",
			llmResponse: `[{"text": "Claim 1", "entities": []}]
Этот текст содержит [скобки].`,
			expectCount: 1,
		},
		{
			name: "preamble with brackets",
			llmResponse: `Вот результат [экстракции]:
[{"text": "Claim 1", "entities": []}]`,
			expectCount: 1,
		},
		{
			name:        "wrapped in markdown",
			llmResponse: "```json\n" + `[{"text": "Claim 1", "entities": []}]` + "\n```",
			expectCount: 1,
		},
		{
			name: "multiple json blocks, takes first valid",
			llmResponse: `[{"text": "Claim 1", "entities": []}]
[{"text": "Claim 2", "entities": []}]`,
			expectCount: 1,
		},
		{
			name:        "invalid JSON",
			llmResponse: `Not a JSON [at all]`,
			expectError: true,
		},
		{
			name:        "empty array",
			llmResponse: `[]`,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockLLMClient{response: tt.llmResponse}
			e := NewExtractor(nil)
			e.SetLLMClient(m, "test-model")

			claims, err := e.extractClaimsWithLLM(context.Background(), "some content")
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(claims) != tt.expectCount {
				t.Errorf(errFmtClaims, tt.expectCount, len(claims))
			}
		})
	}
}
