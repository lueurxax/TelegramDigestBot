package llm

import "testing"

func TestOpenRouterResolveModel(t *testing.T) {
	p := &openRouterProvider{}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty defaults to default model",
			input: "",
			want:  ModelMistralSmall,
		},
		{
			name:  "already qualified path used as-is",
			input: "mistralai/mistral-small-2603",
			want:  "mistralai/mistral-small-2603",
		},
		{
			name:  "other qualified path used as-is",
			input: "meta-llama/llama-3.1-8b-instruct",
			want:  "meta-llama/llama-3.1-8b-instruct",
		},
		{
			name:  "mistral keyword maps to default mistral",
			input: "mistral-7b",
			want:  ModelMistralSmall,
		},
		{
			name:  "bare gpt model falls back to default",
			input: "gpt-4o-mini",
			want:  defaultOpenRouterModel,
		},
		{
			name:  "unknown model falls back to default",
			input: "some-unknown-model",
			want:  defaultOpenRouterModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.resolveModel(tt.input)
			if got != tt.want {
				t.Errorf("resolveModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
