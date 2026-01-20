package enrichment

import (
	"strings"
	"testing"
)

func TestQueryGenerator_Generate(t *testing.T) {
	gen := NewQueryGenerator()

	tests := []struct {
		name          string
		summary       string
		topic         string
		expectQueries bool
		minQueries    int
		maxQueries    int
	}{
		{
			name:          "empty summary",
			summary:       "",
			expectQueries: false,
		},
		{
			name:          "short summary",
			summary:       "Hi",
			expectQueries: false,
		},
		{
			name:          "basic summary",
			summary:       "Apple Inc announced new iPhone sales increased by 15% in Q3 2024",
			topic:         "Technology",
			expectQueries: true,
			minQueries:    1,
			maxQueries:    maxQueries,
		},
		{
			name:          "summary with entities",
			summary:       "President Biden announced new trade deal with China affecting $50 billion in goods",
			topic:         "Politics",
			expectQueries: true,
			minQueries:    2,
			maxQueries:    maxQueries,
		},
		{
			name:          "summary without clear entities",
			summary:       "Technology companies are developing new artificial intelligence systems",
			topic:         "AI",
			expectQueries: true,
			minQueries:    1,
			maxQueries:    maxQueries,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := gen.Generate(tt.summary, tt.topic, "", nil)

			if tt.expectQueries {
				if len(queries) < tt.minQueries || len(queries) > tt.maxQueries {
					t.Errorf("query count: got %d, want %d-%d", len(queries), tt.minQueries, tt.maxQueries)
				}

				for _, q := range queries {
					if q.Query == "" {
						t.Error("empty query generated")
					}

					if q.Strategy == "" {
						t.Error("empty strategy")
					}
				}
			} else if len(queries) > 0 {
				t.Errorf("query count: got %d, want 0", len(queries))
			}
		})
	}
}

func TestQueryGenerator_NoDuplicates(t *testing.T) {
	gen := NewQueryGenerator()
	summary := "Apple Inc announced that Apple profits increased in the Apple store segment"
	topic := "Business"

	queries := gen.Generate(summary, topic, "", nil)
	seen := make(map[string]bool)

	for _, q := range queries {
		lower := strings.ToLower(q.Query)

		if seen[lower] {
			t.Errorf("duplicate query found: %q", q.Query)
		}

		seen[lower] = true
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes mentions",
			input:    "Hello @username this is a test",
			expected: "Hello this is a test",
		},
		{
			name:     "keeps hashtag words",
			input:    "Breaking #news about #technology",
			expected: "Breaking news about technology",
		},
		{
			name:     "removes URLs",
			input:    "Check out https://example.com for more",
			expected: "Check out for more",
		},
		{
			name:     "normalizes whitespace",
			input:    "Multiple   spaces    here",
			expected: "Multiple spaces here",
		},
		{
			name:     "trims",
			input:    "  trimmed  ",
			expected: "trimmed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanText(tt.input)
			if got != tt.expected {
				t.Errorf("cleanText(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractQueryEntities(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		minCount int
	}{
		{
			name:     "person names",
			text:     "John Smith and Mary Johnson met yesterday",
			minCount: 2,
		},
		{
			name:     "organizations",
			text:     "Apple Inc and Microsoft Corp announced partnership",
			minCount: 2,
		},
		{
			name:     "quoted phrases",
			text:     `The report titled "Global Climate Crisis" was released`,
			minCount: 1,
		},
		{
			name:     "acronyms",
			text:     "The FBI and CIA are investigating",
			minCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities := extractQueryEntities(tt.text)
			if len(entities) < tt.minCount {
				t.Errorf("entity count: got %d, want >= %d (%v)", len(entities), tt.minCount, entities)
			}
		})
	}
}

func TestExtractLocations(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "country names",
			text:     "Tensions between Russia and Ukraine continue",
			expected: []string{"Russia", "Ukraine"},
		},
		{
			name:     "city names",
			text:     "Leaders met in Washington and Moscow",
			expected: []string{"Washington", "Moscow"},
		},
		{
			name:     "mixed",
			text:     "The EU announced sanctions on Russia",
			expected: []string{"EU", "Russia"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations := extractLocations(tt.text)

			for _, exp := range tt.expected {
				if !containsIgnoreCase(locations, exp) {
					t.Errorf("location %q not found in %v", exp, locations)
				}
			}
		})
	}
}

func containsIgnoreCase(slice []string, target string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, target) {
			return true
		}
	}

	return false
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		expectWords []string
	}{
		{
			name:        "frequent words",
			text:        "The company reported company profits and company growth",
			expectWords: []string{"company"},
		},
		{
			name:        "filters stop words",
			text:        "This is the test of the system with many articles",
			expectWords: []string{"test", "system"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := extractKeywords(tt.text)

			for _, exp := range tt.expectWords {
				if !containsString(keywords, exp) {
					t.Errorf("keyword %q not found in %v", exp, keywords)
				}
			}
		})
	}
}

func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}

	return false
}

func TestTruncateQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short query unchanged",
			input:    "short query",
			expected: "short query",
		},
		{
			name:     "too short returns empty",
			input:    "hi",
			expected: "",
		},
		{
			name:     "trims whitespace",
			input:    "  query with spaces  ",
			expected: "query with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateQuery(tt.input)
			if got != tt.expected {
				t.Errorf("TruncateQuery(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsEmoji(t *testing.T) {
	emojiRunes := []rune{'😀', '🎉', '❤', '🔥'}

	for _, r := range emojiRunes {
		if !isEmoji(r) {
			t.Errorf("expected %c to be emoji", r)
		}
	}

	nonEmojiRunes := []rune{'A', 'z', '1', '@'}

	for _, r := range nonEmojiRunes {
		if isEmoji(r) {
			t.Errorf("expected %c to not be emoji", r)
		}
	}
}

func TestRemoveEmojis(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello 😀 World", "Hello  World"},
		{"No emojis here", "No emojis here"},
		{"🔥🔥🔥", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := removeEmojis(tt.input)
			if got != tt.expected {
				t.Errorf("removeEmojis(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsCommonAcronym(t *testing.T) {
	common := []string{"AM", "PM", "TV", "OK", "CEO"}

	for _, a := range common {
		if !isCommonAcronym(a) {
			t.Errorf("expected %q to be common acronym", a)
		}
	}

	uncommon := []string{"FBI", "CIA", "NASA", "ACME"}

	for _, a := range uncommon {
		if isCommonAcronym(a) {
			t.Errorf("expected %q to not be common acronym", a)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "English text",
			text:     "Apple Inc announced new iPhone sales increased by 15%",
			expected: langEnglish,
		},
		{
			name:     "Russian text",
			text:     "Президент объявил о новых мерах поддержки экономики",
			expected: langRussian,
		},
		{
			name:     "Ukrainian text",
			text:     "Президент України підписав закон про освіту та безпеку",
			expected: langUkrainian,
		},
		{
			name:     "Greek text",
			text:     "Η κυβέρνηση ανακοίνωσε νέα μέτρα για την οικονομία",
			expected: langGreek,
		},
		{
			name:     "Mixed English-Russian",
			text:     "Apple объявила о новых продуктах",
			expected: langRussian, // Cyrillic dominates
		},
		{
			name:     "Latin non-English",
			text:     "Guten Tag aus Berlin und willkommen",
			expected: langUnknown,
		},
		{
			name:     "Chinese text",
			text:     "苹果公司宣布新款iPhone销量增长",
			expected: langUnknown,
		},
		{
			name:     "Empty text",
			text:     "",
			expected: langUnknown,
		},
		{
			name:     "Numbers only",
			text:     "12345 67890",
			expected: langUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.text)
			if got != tt.expected {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestIsEnglish(t *testing.T) {
	tests := []struct {
		language string
		expected bool
	}{
		{langEnglish, true},
		{langRussian, false},
		{langUkrainian, false},
		{langGreek, false},
		{langUnknown, false},
		{"de", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			got := isEnglish(tt.language)
			if got != tt.expected {
				t.Errorf("isEnglish(%q) = %v, want %v", tt.language, got, tt.expected)
			}
		})
	}
}

func TestGenerateIncludesLanguage(t *testing.T) {
	gen := NewQueryGenerator()

	testCases := []struct {
		name         string
		summary      string
		topic        string
		expectedLang string
	}{
		{
			name:         "English summary has English language",
			summary:      "Apple Inc announced new iPhone sales increased by 15% in Q3",
			topic:        "Tech",
			expectedLang: langEnglish,
		},
		{
			name:         "Russian summary has Russian language",
			summary:      "Президент России объявил о новых мерах поддержки экономики страны",
			topic:        "Политика",
			expectedLang: langRussian,
		},
		{
			name:         "Ukrainian summary has Ukrainian language",
			summary:      "Президент України підписав закон про освіту",
			topic:        "Політика",
			expectedLang: langUkrainian,
		},
		{
			name:         "Greek summary has Greek language",
			summary:      "Η κυβέρνηση ανακοίνωσε νέα μέτρα για την οικονομία",
			topic:        "Επικαιρότητα",
			expectedLang: langGreek,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertLanguageInQueries(t, gen, tc.summary, tc.topic, tc.expectedLang)
		})
	}
}

func assertLanguageInQueries(t *testing.T, gen *QueryGenerator, summary, topic, expectedLang string) {
	t.Helper()

	queries := gen.Generate(summary, topic, "", nil)
	if len(queries) == 0 {
		t.Fatalf("no queries generated for %s summary", expectedLang)
	}

	for _, q := range queries {
		if q.Language != expectedLang {
			t.Errorf("query %q: got language %q, want %q", q.Query, q.Language, expectedLang)
		}
	}
}
