package pipeline

import (
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

func TestClampFloat32(t *testing.T) {
	tests := []struct {
		name     string
		value    float32
		min      float32
		max      float32
		expected float32
	}{
		{"within range", 0.5, 0, 1, 0.5},
		{"below min", -0.5, 0, 1, 0},
		{"above max", 1.5, 0, 1, 1},
		{"at min", 0, 0, 1, 0},
		{"at max", 1, 0, 1, 1},
		{"negative range", -5, -10, -1, -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampFloat32(tt.value, tt.min, tt.max)
			if got != tt.expected {
				t.Errorf("clampFloat32(%v, %v, %v) = %v, want %v", tt.value, tt.min, tt.max, got, tt.expected)
			}
		})
	}
}

func TestClampScore(t *testing.T) {
	tests := []struct {
		name     string
		value    float32
		expected float32
	}{
		{"within range", 0.5, 0.5},
		{"below zero", -0.5, 0},
		{"above one", 1.5, 1},
		{"at zero", 0, 0},
		{"at one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampScore(tt.value)
			if got != tt.expected {
				t.Errorf("clampScore(%v) = %v, want %v", tt.value, got, tt.expected)
			}
		})
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		{"simple domain", "example.com", "example.com"},
		{"with www", "www.example.com", "example.com"},
		{"uppercase", "WWW.EXAMPLE.COM", "example.com"},
		{"with whitespace", "  example.com  ", "example.com"},
		{"empty", "", ""},
		{"just www", "www.", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDomain(tt.domain)
			if got != tt.expected {
				t.Errorf("normalizeDomain(%q) = %q, want %q", tt.domain, got, tt.expected)
			}
		})
	}
}

func TestExtractDomains(t *testing.T) {
	tests := []struct {
		name     string
		input    llm.MessageInput
		expected []string
	}{
		{
			name:     "no links",
			input:    llm.MessageInput{RawMessage: domain.RawMessage{Text: "no links here"}},
			expected: nil,
		},
		{
			name: "resolved links",
			input: llm.MessageInput{
				ResolvedLinks: []domain.ResolvedLink{
					{Domain: "example.com"},
					{Domain: "test.org"},
				},
			},
			expected: []string{"example.com", "test.org"},
		},
		{
			name: "resolved links with www",
			input: llm.MessageInput{
				ResolvedLinks: []domain.ResolvedLink{
					{Domain: "www.example.com"},
				},
			},
			expected: []string{"example.com"},
		},
		{
			name: "duplicate domains",
			input: llm.MessageInput{
				ResolvedLinks: []domain.ResolvedLink{
					{Domain: "example.com"},
					{Domain: "example.com"},
				},
			},
			expected: []string{"example.com"},
		},
		{
			name: "empty domain skipped",
			input: llm.MessageInput{
				ResolvedLinks: []domain.ResolvedLink{
					{Domain: ""},
					{Domain: "example.com"},
				},
			},
			expected: []string{"example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomains(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("extractDomains() len = %d, want %d", len(got), len(tt.expected))
				return
			}

			for i, d := range got {
				if d != tt.expected[i] {
					t.Errorf("extractDomains()[%d] = %q, want %q", i, d, tt.expected[i])
				}
			}
		})
	}
}

func TestNormalizeBulletText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"simple text", "Hello World", "hello world"},
		{"extra whitespace", "  hello   world  ", "hello world"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"newlines", "hello\nworld", "hello world"},
		{"tabs", "hello\tworld", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBulletText(tt.text)
			if got != tt.expected {
				t.Errorf("normalizeBulletText(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestGenerateBulletHash(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		expectEmpty bool
	}{
		{"normal text", "Hello World", false},
		{"empty text", "", false},
		{"whitespace", "  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateBulletHash(tt.text)
			if (got == "") != tt.expectEmpty {
				t.Errorf("generateBulletHash(%q) empty = %v, want %v", tt.text, got == "", tt.expectEmpty)
			}

			if !tt.expectEmpty && len(got) != 32 {
				t.Errorf("generateBulletHash(%q) len = %d, want 32", tt.text, len(got))
			}
		})
	}

	const testInput = "test"

	// Same input should produce same hash
	hash1 := generateBulletHash(testInput)

	hash2 := generateBulletHash(testInput)
	if hash1 != hash2 {
		t.Errorf("generateBulletHash determinism failed: %q != %q", hash1, hash2)
	}

	// Normalized equivalents should match
	hash3 := generateBulletHash("Hello World")

	hash4 := generateBulletHash("  hello   world  ")
	if hash3 != hash4 {
		t.Errorf("generateBulletHash normalization failed: %q != %q", hash3, hash4)
	}
}

func TestDedupeExtractedBullets(t *testing.T) {
	tests := []struct {
		name     string
		bullets  []llm.ExtractedBullet
		expected int
	}{
		{"empty", []llm.ExtractedBullet{}, 0},
		{"single", []llm.ExtractedBullet{{Text: "one"}}, 1},
		{"no duplicates", []llm.ExtractedBullet{{Text: "one"}, {Text: "two"}}, 2},
		{"with duplicates", []llm.ExtractedBullet{{Text: "one"}, {Text: "one"}}, 1},
		{"normalized duplicates", []llm.ExtractedBullet{{Text: "Hello"}, {Text: "  hello  "}}, 1},
		{"empty text filtered", []llm.ExtractedBullet{{Text: ""}, {Text: "one"}}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeExtractedBullets(tt.bullets)
			if len(got) != tt.expected {
				t.Errorf("dedupeExtractedBullets() len = %d, want %d", len(got), tt.expected)
			}
		})
	}
}

func TestCoalesceTopic(t *testing.T) {
	tests := []struct {
		name     string
		topics   []string
		expected string
	}{
		{"first non-empty", []string{"", "tech", "news"}, "tech"},
		{"all empty", []string{"", "", ""}, ""},
		{"first is set", []string{"tech", "news"}, "tech"},
		{"no args", []string{}, ""},
		{"single empty", []string{""}, ""},
		{"single value", []string{"tech"}, "tech"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coalesceTopic(tt.topics...)
			if got != tt.expected {
				t.Errorf("coalesceTopic(%v) = %q, want %q", tt.topics, got, tt.expected)
			}
		})
	}
}

func TestSummarizeBullets(t *testing.T) {
	tests := []struct {
		name              string
		bullets           []llm.ExtractedBullet
		minImportance     float32
		expectMaxImp      float32
		expectMaxRel      float32
		expectIncludedCnt int
	}{
		{
			name:              "empty",
			bullets:           []llm.ExtractedBullet{},
			minImportance:     0.5,
			expectMaxImp:      0,
			expectMaxRel:      0,
			expectIncludedCnt: 0,
		},
		{
			name: "single above threshold",
			bullets: []llm.ExtractedBullet{
				{ImportanceScore: 0.8, RelevanceScore: 0.7},
			},
			minImportance:     0.5,
			expectMaxImp:      0.8,
			expectMaxRel:      0.7,
			expectIncludedCnt: 1,
		},
		{
			name: "single below threshold",
			bullets: []llm.ExtractedBullet{
				{ImportanceScore: 0.3, RelevanceScore: 0.7},
			},
			minImportance:     0.5,
			expectMaxImp:      0.3,
			expectMaxRel:      0.7,
			expectIncludedCnt: 0,
		},
		{
			name: "mixed",
			bullets: []llm.ExtractedBullet{
				{ImportanceScore: 0.3, RelevanceScore: 0.5},
				{ImportanceScore: 0.8, RelevanceScore: 0.9},
				{ImportanceScore: 0.6, RelevanceScore: 0.4},
			},
			minImportance:     0.5,
			expectMaxImp:      0.8,
			expectMaxRel:      0.9,
			expectIncludedCnt: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeBullets(tt.bullets, tt.minImportance)
			if got.maxImportance != tt.expectMaxImp {
				t.Errorf("summarizeBullets().maxImportance = %v, want %v", got.maxImportance, tt.expectMaxImp)
			}

			if got.maxRelevance != tt.expectMaxRel {
				t.Errorf("summarizeBullets().maxRelevance = %v, want %v", got.maxRelevance, tt.expectMaxRel)
			}

			if got.includedCount != tt.expectIncludedCnt {
				t.Errorf("summarizeBullets().includedCount = %v, want %v", got.includedCount, tt.expectIncludedCnt)
			}
		})
	}
}
