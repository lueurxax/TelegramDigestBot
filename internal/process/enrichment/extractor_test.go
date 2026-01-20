package enrichment

import (
	"testing"
)

func TestExtractClaims(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectCount int
		expectClaim string
	}{
		{
			name:        "empty content",
			content:     "",
			expectCount: 0,
		},
		{
			name:        "factual sentence with number",
			content:     "The company reported profits of $50 million in Q3. This represents a 15% increase from last year.",
			expectCount: 2,
		},
		{
			name:        "sentence with indicator",
			content:     "According to the report, sales increased by 20%. The CEO confirmed these figures yesterday.",
			expectCount: 2,
		},
		{
			name:        "non-factual content",
			content:     "Hello world. This is a test.",
			expectCount: 0,
		},
		{
			name:        "respects max claims limit",
			content:     "According to reports, value 1. According to reports, value 2. According to reports, value 3. According to reports, value 4. According to reports, value 5. According to reports, value 6. According to reports, value 7. According to reports, value 8. According to reports, value 9. According to reports, value 10. According to reports, value 11.",
			expectCount: maxExtractedClaims,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := extractClaims(tt.content)
			if len(claims) != tt.expectCount {
				t.Errorf("expected %d claims, got %d", tt.expectCount, len(claims))
			}
		})
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"One sentence.", 1},
		{"First. Second. Third.", 3},
		{"Question? Answer!", 2},
		{"", 0},
		{"No period at end", 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sentences := splitSentences(tt.input)
			if len(sentences) != tt.expected {
				t.Errorf("splitSentences(%q) got %d sentences, expected %d", tt.input, len(sentences), tt.expected)
			}
		})
	}
}

func TestIsFactualSentence(t *testing.T) {
	factual := []string{
		"According to the report, sales increased",
		"The company announced quarterly results",
		"Revenue grew by 15 million dollars",
		"The index rose by 5%",
		"Officials confirmed the data",
	}

	for _, s := range factual {
		if !isFactualSentence(s) {
			t.Errorf("expected %q to be factual", s)
		}
	}

	nonFactual := []string{
		"Hello world",
		"This is interesting",
		"We should consider options",
	}

	for _, s := range nonFactual {
		if isFactualSentence(s) {
			t.Errorf("expected %q to not be factual", s)
		}
	}
}

func TestContainsNumber(t *testing.T) {
	withNumbers := []string{"Price is $100", "15% increase", "Year 2024"}
	for _, s := range withNumbers {
		if !containsNumber(s) {
			t.Errorf("expected %q to contain number", s)
		}
	}

	withoutNumbers := []string{"Hello world", "No numbers here", "ABC"}
	for _, s := range withoutNumbers {
		if containsNumber(s) {
			t.Errorf("expected %q to not contain number", s)
		}
	}
}

func TestExtractEntities(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		expectTypes map[string]bool
		minEntities int
	}{
		{
			name:        "person names",
			text:        "John Smith met with Mary Johnson",
			expectTypes: map[string]bool{entityTypePerson: true},
			minEntities: 2,
		},
		{
			name:        "organizations",
			text:        "Apple Inc and Microsoft Corp announced partnership",
			expectTypes: map[string]bool{entityTypeOrg: true},
			minEntities: 2,
		},
		{
			name:        "locations",
			text:        "The summit was held in Washington and Moscow",
			expectTypes: map[string]bool{entityTypeLoc: true},
			minEntities: 2,
		},
		{
			name:        "money amounts",
			text:        "The deal is worth $50 million",
			expectTypes: map[string]bool{entityTypeMoney: true},
			minEntities: 1,
		},
		{
			name:        "percentages",
			text:        "Revenue increased by 15.5%",
			expectTypes: map[string]bool{entityTypePercent: true},
			minEntities: 1,
		},
		{
			name: "mixed entities",
			text: "CEO John Smith announced Apple Inc revenues in Washington grew 25%",
			expectTypes: map[string]bool{
				entityTypePerson:  true,
				entityTypeOrg:     true,
				entityTypeLoc:     true,
				entityTypePercent: true,
			},
			minEntities: 4,
		},
		{
			name:        "russian entities with accents",
			text:        "Населе́ние Земли́ по состоянию на январь 2026 года выросло.",
			expectTypes: map[string]bool{entityTypePerson: true},
			minEntities: 1,
		},
		{
			name:        "russian acronyms",
			text:        "ЕС и ООН обсудили новый пакет мер.",
			expectTypes: map[string]bool{entityTypeOrg: true},
			minEntities: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities := extractEntities(tt.text)

			if len(entities) < tt.minEntities {
				t.Errorf("expected at least %d entities, got %d: %v", tt.minEntities, len(entities), entities)
			}

			foundTypes := make(map[string]bool)
			for _, e := range entities {
				foundTypes[e.Type] = true
			}

			for typ := range tt.expectTypes {
				if !foundTypes[typ] {
					t.Errorf("expected entity type %s not found in %v", typ, entities)
				}
			}
		})
	}
}

func TestExtractedClaim_EntitiesJSON(t *testing.T) {
	t.Run("empty entities returns nil", func(t *testing.T) {
		claim := ExtractedClaim{Text: "test", Entities: []Entity{}}
		if claim.EntitiesJSON() != nil {
			t.Error("expected nil for empty entities")
		}
	})

	t.Run("with entities returns JSON", func(t *testing.T) {
		claim := ExtractedClaim{
			Text: "test",
			Entities: []Entity{
				{Text: "John", Type: entityTypePerson},
			},
		}

		jsonData := claim.EntitiesJSON()
		if jsonData == nil {
			t.Fatal("expected non-nil JSON")
		}

		if len(jsonData) == 0 {
			t.Error("expected non-empty JSON")
		}
	})
}

func TestCoalesce(t *testing.T) {
	tests := []struct {
		a, b, expected string
	}{
		{"first", "second", "first"},
		{"", "second", "second"},
		{"", "", ""},
		{"first", "", "first"},
	}

	for _, tt := range tests {
		got := coalesce(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("coalesce(%q, %q) = %q, expected %q", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestNewExtractor(t *testing.T) {
	ext := NewExtractor(nil)
	if ext == nil {
		t.Fatal("expected non-nil extractor")
	}

	if ext.httpClient == nil {
		t.Fatal("expected non-nil http client")
	}

	if ext.httpClient.Timeout != fetchTimeout {
		t.Errorf("expected timeout %v, got %v", fetchTimeout, ext.httpClient.Timeout)
	}
}
