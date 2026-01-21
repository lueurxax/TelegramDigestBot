package enrichment

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanTranslation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no prefix",
			input:    "this is a test",
			expected: "this is a test",
		},
		{
			name:     "translation prefix",
			input:    "Translation: this is a test",
			expected: "this is a test",
		},
		{
			name:     "translated prefix",
			input:    "Translated: this is a test",
			expected: "this is a test",
		},
		{
			name:     "query prefix",
			input:    "Query: this is a test",
			expected: "this is a test",
		},
		{
			name:     "translated query prefix",
			input:    "Translated Query: this is a test",
			expected: "this is a test",
		},
		{
			name:     "russian translation prefix",
			input:    "Перевод: это тест",
			expected: "это тест",
		},
		{
			name:     "russian query prefix",
			input:    "Запрос: это тест",
			expected: "это тест",
		},
		{
			name:     "with quotes",
			input:    `"this is a test"`,
			expected: "this is a test",
		},
		{
			name:     "prefix with quotes",
			input:    `Translation: "this is a test"`,
			expected: "this is a test",
		},
		{
			name:     "nested prefix",
			input:    "Translation: Query: this is a test",
			expected: "this is a test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanTranslation(tt.input))
		})
	}
}
