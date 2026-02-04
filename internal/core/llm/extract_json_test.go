package llm

import "testing"

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "pure_object",
			input: `{"key":"value"}`,
			want:  `{"key":"value"}`,
		},
		{
			name:  "pure_array",
			input: `[{"a":1}]`,
			want:  `[{"a":1}]`,
		},
		{
			name:  "array_with_preamble",
			input: `Here is the result: [{"a":1}]`,
			want:  `[{"a":1}]`,
		},
		{
			name:  "object_with_preamble",
			input: `Here: {"key":"value"} done.`,
			want:  `{"key":"value"}`,
		},
		{
			name:  "array_preferred_over_object",
			input: `Text [{"a":1}] and {"b":2}`,
			want:  `[{"a":1}]`,
		},
		{
			name:  "nested_brackets_in_strings",
			input: `{"arr":"[1,2,3]","key":"val"}`,
			want:  `{"arr":"[1,2,3]","key":"val"}`,
		},
		{
			name:  "no_json",
			input: "just some text",
			want:  "just some text",
		},
		{
			name:  "invalid_json_brackets",
			input: `text { not json } more`,
			want:  "text { not json } more",
		},
		{
			name:  "markdown_wrapped_array",
			input: "```json\n[{\"text\":\"claim\"}]\n```",
			want:  `[{"text":"claim"}]`,
		},
		{
			name:  "garbage_before_valid_array",
			input: `cater FIRE [{"text":"real claim"}]`,
			want:  `[{"text":"real claim"}]`,
		},
		{
			name:  "empty_array",
			input: `Result: []`,
			want:  `[]`,
		},
		{
			name:  "empty_object",
			input: `Result: {}`,
			want:  `{}`,
		},
		{
			name:  "nested_arrays",
			input: `[{"items":[1,2,3]},{"items":[4,5]}]`,
			want:  `[{"items":[1,2,3]},{"items":[4,5]}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
