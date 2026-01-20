package enrichment

import (
	"testing"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func TestScorer_Score(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name           string
		itemSummary    string
		evidence       *ExtractedEvidence
		expectScore    bool
		expectMatch    bool
		expectContradt bool
		maxScore       float32
	}{
		{
			name:        "nil evidence",
			itemSummary: "Test summary",
			evidence:    nil,
			expectScore: false,
		},
		{
			name:        "empty claims",
			itemSummary: "Test summary",
			evidence: &ExtractedEvidence{
				Claims: []ExtractedClaim{},
			},
			expectScore: false,
		},
		{
			name:        "matching claims",
			itemSummary: "The company announced quarterly earnings increased by 10 percent",
			evidence: &ExtractedEvidence{
				Claims: []ExtractedClaim{
					{
						Text: "The company announced quarterly earnings increased by 10 percent in Q3",
						Entities: []Entity{
							{Text: "company", Type: entityTypeOrg},
						},
					},
				},
			},
			expectScore: true,
			expectMatch: true,
		},
		{
			name:        "non-matching claims",
			itemSummary: "Weather forecast for tomorrow",
			evidence: &ExtractedEvidence{
				Claims: []ExtractedClaim{
					{
						Text:     "Stock market closes at record high",
						Entities: []Entity{},
					},
				},
			},
			expectScore: false,
		},
		{
			name:        "single entity overlap without token match",
			itemSummary: "Жители России наблюдают северное сияние",
			evidence: &ExtractedEvidence{
				Claims: []ExtractedClaim{
					{
						Text: "Russia celebrates a cultural holiday in January",
						Entities: []Entity{
							{Text: "Russia", Type: entityTypeLoc},
						},
					},
				},
			},
			expectScore: false,
			maxScore:    0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(tt.itemSummary, tt.evidence)

			if tt.expectScore && result.AgreementScore <= 0 {
				t.Errorf("expected positive score, got %f", result.AgreementScore)
			}

			if !tt.expectScore && result.AgreementScore > 0.5 {
				t.Errorf("expected low/zero score, got %f", result.AgreementScore)
			}

			if tt.maxScore > 0 && result.AgreementScore > tt.maxScore {
				t.Errorf("expected score <= %f, got %f", tt.maxScore, result.AgreementScore)
			}

			if tt.expectMatch && len(result.MatchedClaims) == 0 {
				t.Error("expected matched claims, got none")
			}
		})
	}
}

func TestScorer_DetermineTier(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name        string
		sourceCount int
		avgScore    float32
		expected    string
	}{
		{
			name:        "high tier",
			sourceCount: 3,
			avgScore:    0.6,
			expected:    db.FactCheckTierHigh,
		},
		{
			name:        "high tier minimum",
			sourceCount: 2,
			avgScore:    0.5,
			expected:    db.FactCheckTierHigh,
		},
		{
			name:        "medium tier",
			sourceCount: 1,
			avgScore:    0.4,
			expected:    db.FactCheckTierMedium,
		},
		{
			name:        "low tier no sources",
			sourceCount: 0,
			avgScore:    0.0,
			expected:    db.FactCheckTierLow,
		},
		{
			name:        "low tier low score",
			sourceCount: 1,
			avgScore:    0.1,
			expected:    db.FactCheckTierLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scorer.DetermineTier(tt.sourceCount, tt.avgScore)
			if got != tt.expected {
				t.Errorf("DetermineTier(%d, %f) = %q, expected %q", tt.sourceCount, tt.avgScore, got, tt.expected)
			}
		})
	}
}

func TestScorer_CalculateOverallScore(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name     string
		scores   []float32
		expected float32
	}{
		{
			name:     "empty scores",
			scores:   []float32{},
			expected: 0,
		},
		{
			name:     "single score",
			scores:   []float32{0.5},
			expected: 0.5,
		},
		{
			name:     "multiple scores",
			scores:   []float32{0.4, 0.6, 0.8},
			expected: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scorer.CalculateOverallScore(tt.scores)

			// Use tolerance for float comparison
			diff := got - tt.expected
			if diff < 0 {
				diff = -diff
			}

			if diff > 0.001 {
				t.Errorf("score: got %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestScorer_MarshalMatchedClaims(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name     string
		claims   []MatchedClaim
		expectNL bool
	}{
		{
			name:     "empty claims",
			claims:   []MatchedClaim{},
			expectNL: true,
		},
		{
			name:     "nil claims",
			claims:   nil,
			expectNL: true,
		},
		{
			name: "with claims",
			claims: []MatchedClaim{
				{ItemClaim: "test", EvidenceClaim: "test2", Score: 0.5},
			},
			expectNL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scorer.MarshalMatchedClaims(tt.claims)

			if tt.expectNL && got != nil {
				t.Errorf("expected nil, got %v", got)
			}

			if !tt.expectNL && got == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		set1     map[string]bool
		set2     map[string]bool
		expected float64
	}{
		{
			name:     "empty sets",
			set1:     map[string]bool{},
			set2:     map[string]bool{},
			expected: 0,
		},
		{
			name:     "identical sets",
			set1:     map[string]bool{"a": true, "b": true},
			set2:     map[string]bool{"a": true, "b": true},
			expected: 1.0,
		},
		{
			name:     "no overlap",
			set1:     map[string]bool{"a": true, "b": true},
			set2:     map[string]bool{"c": true, "d": true},
			expected: 0,
		},
		{
			name:     "partial overlap",
			set1:     map[string]bool{"a": true, "b": true, "c": true},
			set2:     map[string]bool{"b": true, "c": true, "d": true},
			expected: 0.5, // 2 common / 4 total
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccardSimilarity(tt.set1, tt.set2)
			if got != tt.expected {
				t.Errorf("jaccardSimilarity() = %f, expected %f", got, tt.expected)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		expectTokens  []string
		excludeTokens []string
	}{
		{
			name:          "basic tokenization",
			text:          "The quick brown fox jumps",
			expectTokens:  []string{"quick", "brown", "jumps", "fox"},
			excludeTokens: []string{"the"}, // stop word
		},
		{
			name:          "removes stop words",
			text:          "This is a test of the system",
			expectTokens:  []string{"test", "system"},
			excludeTokens: []string{"this", "is", "a", "of", "the"},
		},
		{
			name:         "empty text",
			text:         "",
			expectTokens: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.text)

			for _, token := range tt.expectTokens {
				if !got[token] {
					t.Errorf("expected token %q not found", token)
				}
			}

			for _, token := range tt.excludeTokens {
				if got[token] {
					t.Errorf("unexpected token %q found", token)
				}
			}
		})
	}
}

func TestDetectContradiction(t *testing.T) {
	tests := []struct {
		name        string
		itemText    string
		claims      []ExtractedClaim
		entities    []Entity
		expectContr bool
	}{
		{
			name:     "no claims",
			itemText: "Test statement",
			claims:   []ExtractedClaim{},
			entities: []Entity{},
		},
		{
			name:     "negation in evidence",
			itemText: "Company profits increased this quarter",
			claims: []ExtractedClaim{
				{
					Text:     "Company profits did not increase this quarter",
					Entities: []Entity{{Text: "Company", Type: entityTypeOrg}},
				},
			},
			entities:    []Entity{{Text: "Company", Type: entityTypeOrg}},
			expectContr: true,
		},
		{
			name:     "opposing words",
			itemText: "Stock prices rose sharply",
			claims: []ExtractedClaim{
				{
					Text:     "Stock prices fell sharply",
					Entities: []Entity{{Text: "Stock", Type: entityTypeOrg}},
				},
			},
			entities:    []Entity{{Text: "Stock", Type: entityTypeOrg}},
			expectContr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectContradiction(tt.itemText, tt.claims, tt.entities)
			if got != tt.expectContr {
				t.Errorf("detectContradiction() = %v, expected %v", got, tt.expectContr)
			}
		})
	}
}

func TestContainsOpposingPair(t *testing.T) {
	tests := []struct {
		name     string
		text1    string
		text2    string
		wordA    string
		wordB    string
		expected bool
	}{
		{
			name:     "text1 has A, text2 has B",
			text1:    "prices increased",
			text2:    "prices decreased",
			wordA:    "increased",
			wordB:    "decreased",
			expected: true,
		},
		{
			name:     "both have A",
			text1:    "prices increased",
			text2:    "value increased",
			wordA:    "increased",
			wordB:    "decreased",
			expected: false,
		},
		{
			name:     "neither has either",
			text1:    "prices stable",
			text2:    "value stable",
			wordA:    "increased",
			wordB:    "decreased",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsOpposingPair(tt.text1, tt.text2, tt.wordA, tt.wordB)
			if got != tt.expected {
				t.Errorf("containsOpposingPair() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestIsStopWord(t *testing.T) {
	stopWords := []string{"the", "a", "an", "and", "or", "is", "are", "was", "were"}
	for _, w := range stopWords {
		if !isStopWord(w) {
			t.Errorf("expected %q to be a stop word", w)
		}
	}

	nonStopWords := []string{"company", "profits", "increased", "quarterly"}
	for _, w := range nonStopWords {
		if isStopWord(w) {
			t.Errorf("expected %q to not be a stop word", w)
		}
	}
}
