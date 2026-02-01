package enrichment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links/linkextract"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	errFmtScoreTooLow      = "score too low: got %f, want at least %f"
	errFmtScoreTooHigh     = "score too high: got %f, want at most %f"
	errExpectedMatchClaims = "expected matched claims, got none"
)

// loadTestFixture loads HTML content from testdata directory.
func loadTestFixture(t *testing.T, filename string) []byte {
	t.Helper()

	path := filepath.Join("testdata", filename)

	data, err := os.ReadFile(path) //nolint:gosec // test fixture loading is safe
	if err != nil {
		t.Fatalf("failed to load fixture %s: %v", filename, err)
	}

	return data
}

// TestLinkExtraction_Integration tests that links are correctly extracted from message text.
func TestLinkExtraction_Integration(t *testing.T) {
	tests := []struct {
		name          string
		messageText   string
		expectedURLs  []string
		expectedTypes []linkextract.LinkType
	}{
		{
			name:          "russian news with web link",
			messageText:   "Важные новости! http://russiancyprus.news/news/society/stop-the-mockery-over-larnaca-marina-says-protestors/",
			expectedURLs:  []string{"http://russiancyprus.news/news/society/stop-the-mockery-over-larnaca-marina-says-protestors"},
			expectedTypes: []linkextract.LinkType{linkextract.LinkTypeWeb},
		},
		{
			name:          "telegram post link",
			messageText:   "See https://t.me/russiancyprusnews/11617 for details",
			expectedURLs:  []string{"https://t.me/russiancyprusnews/11617"},
			expectedTypes: []linkextract.LinkType{linkextract.LinkTypeTelegram},
		},
		{
			name:          "multiple links mixed types",
			messageText:   "News: https://cyprus-mail.com/article and https://t.me/channel/123",
			expectedURLs:  []string{"https://cyprus-mail.com/article", "https://t.me/channel/123"},
			expectedTypes: []linkextract.LinkType{linkextract.LinkTypeWeb, linkextract.LinkTypeTelegram},
		},
		{
			name:          "blocked social media link",
			messageText:   "Follow us https://twitter.com/cyprusnews",
			expectedURLs:  []string{"https://twitter.com/cyprusnews"},
			expectedTypes: []linkextract.LinkType{linkextract.LinkTypeBlocked},
		},
		{
			name:          "no links",
			messageText:   "Just plain text without any URLs",
			expectedURLs:  nil,
			expectedTypes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extracted := linkextract.ExtractLinks(tt.messageText)

			if len(extracted) != len(tt.expectedURLs) {
				t.Errorf("link count: got %d, want %d", len(extracted), len(tt.expectedURLs))
				return
			}

			for i, link := range extracted {
				if link.URL != tt.expectedURLs[i] {
					t.Errorf("link[%d] URL: got %q, want %q", i, link.URL, tt.expectedURLs[i])
				}

				if link.Type != tt.expectedTypes[i] {
					t.Errorf("link[%d] type: got %q, want %q", i, link.Type, tt.expectedTypes[i])
				}
			}
		})
	}
}

// TestContentExtraction_Integration tests HTML content extraction from web pages.
func TestContentExtraction_Integration(t *testing.T) {
	tests := []struct {
		name            string
		fixtureFile     string
		rawURL          string
		expectTitle     string
		expectContent   []string // substrings that should appear in content
		expectMinLength int
	}{
		{
			name:            "russian cyprus news article",
			fixtureFile:     "cyprus_news_ru.html",
			rawURL:          "http://russiancyprus.news/news/society/stop-the-mockery/",
			expectTitle:     "Stop the mockery over Larnaca marina",
			expectContent:   []string{"Larnaca marina", "200 people", "Maria Georgiou"},
			expectMinLength: 200,
		},
		{
			name:            "cyprus mail original article",
			fixtureFile:     "cyprus_mail_original.html",
			rawURL:          "https://cyprus-mail.com/2026/02/01/stop-the-mockery-over-larnaca-marina-says-protestors",
			expectTitle:     "Stop the mockery over Larnaca marina",
			expectContent:   []string{"200 people protested", "Larnaca marina", "Maria Georgiou", "2018"},
			expectMinLength: 300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			htmlBytes := loadTestFixture(t, tt.fixtureFile)

			content, err := links.ExtractWebContent(htmlBytes, tt.rawURL, maxContentLength)
			if err != nil {
				t.Fatalf("extraction error: %v", err)
			}

			if content == nil {
				t.Fatal("content is nil")
			}

			if !strings.Contains(content.Title, tt.expectTitle) {
				t.Errorf("title: got %q, want to contain %q", content.Title, tt.expectTitle)
			}

			if len(content.Content) < tt.expectMinLength {
				t.Errorf("content length: got %d, want at least %d", len(content.Content), tt.expectMinLength)
			}

			for _, substr := range tt.expectContent {
				if !strings.Contains(content.Content, substr) {
					t.Errorf("content missing expected substring: %q", substr)
				}
			}
		})
	}
}

// verifyQueriesNotEmpty checks that all queries have non-empty Query and Strategy fields.
func verifyQueriesNotEmpty(t *testing.T, queries []GeneratedQuery) {
	t.Helper()

	for i, q := range queries {
		if q.Query == "" {
			t.Errorf("query[%d] is empty", i)
		}

		if q.Strategy == "" {
			t.Errorf("query[%d] has no strategy", i)
		}
	}
}

// verifyQueriesContainKeywords checks that at least one query contains each expected keyword.
func verifyQueriesContainKeywords(t *testing.T, queries []GeneratedQuery, keywords []string) {
	t.Helper()

	for _, keyword := range keywords {
		found := false

		for _, q := range queries {
			if strings.Contains(strings.ToLower(q.Query), strings.ToLower(keyword)) {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("no query contains keyword %q, queries: %v", keyword, queries)
		}
	}
}

// TestQueryGeneration_Integration tests search query generation from message summaries.
func TestQueryGeneration_Integration(t *testing.T) {
	gen := NewQueryGenerator()

	tests := []struct {
		name           string
		summary        string
		text           string
		topic          string
		channelTitle   string
		links          []domain.ResolvedLink
		expectQueries  bool
		minQueries     int
		expectKeywords []string // at least one query should contain these
	}{
		{
			name:           "larnaca marina protest summary",
			summary:        "Protestors demand an end to delays on Larnaca marina development project",
			text:           "About 200 people protested outside Larnaca marina demanding action",
			topic:          "News",
			channelTitle:   "Russian Cyprus News",
			expectQueries:  true,
			minQueries:     1,
			expectKeywords: []string{"marina"}, // stable keyword from summary
		},
		{
			name:           "summary with entities and numbers",
			summary:        "President Biden announces $50 billion trade deal with China",
			text:           "",
			topic:          "Politics",
			channelTitle:   "World News",
			expectQueries:  true,
			minQueries:     2,
			expectKeywords: []string{"Biden", "China"}, // main entities from summary
		},
		{
			name:    "summary with resolved link context",
			summary: "Local news about marina protest",
			text:    "",
			topic:   "Local",
			links: []domain.ResolvedLink{
				{
					Title:   "Stop the mockery over Larnaca marina, says protestors",
					Content: "About 200 people protested outside the Larnaca marina on Saturday",
					Domain:  "cyprus-mail.com",
				},
			},
			expectQueries:  true,
			minQueries:     1,
			expectKeywords: []string{"Larnaca", "marina"},
		},
		{
			name:          "russian text generates queries",
			summary:       "Протестующие требуют прекратить насмешки над маринай в Ларнаке",
			text:          "Около 200 человек вышли на протест у марины Ларнаки в субботу",
			topic:         "Новости",
			channelTitle:  "Russian Cyprus News",
			expectQueries: true,
			minQueries:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := gen.Generate(tt.summary, tt.text, tt.topic, tt.channelTitle, tt.links)

			if tt.expectQueries {
				if len(queries) < tt.minQueries {
					t.Errorf("query count: got %d, want at least %d", len(queries), tt.minQueries)
				}

				verifyQueriesNotEmpty(t, queries)
				verifyQueriesContainKeywords(t, queries, tt.expectKeywords)
			} else if len(queries) > 0 {
				t.Errorf("expected no queries, got %d", len(queries))
			}
		})
	}
}

// TestEvidenceScoring_Integration tests the scoring of evidence against item summaries.
func TestEvidenceScoring_Integration(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name           string
		itemSummary    string
		evidence       *ExtractedEvidence
		expectMinScore float32
		expectMaxScore float32
		expectMatches  bool
		expectTier     string
		sourceCount    int
	}{
		{
			name:        "matching english content",
			itemSummary: "About 200 people protested outside Larnaca marina demanding an end to development delays",
			evidence: &ExtractedEvidence{
				Source: &db.EvidenceSource{
					URL:    "https://cyprus-mail.com/article",
					Domain: "cyprus-mail.com",
					Title:  "Larnaca marina protest",
				},
				Claims: []ExtractedClaim{
					{
						Text: "About 200 people protested outside the Larnaca marina on Saturday demanding authorities end delays on the marina development project",
						Entities: []Entity{
							{Text: "Larnaca", Type: entityTypeLoc},
							{Text: "200", Type: "NUMBER"},
						},
					},
					{
						Text: "Protestors held banners reading Stop the mockery as they gathered near the waterfront",
						Entities: []Entity{
							{Text: "Larnaca", Type: entityTypeLoc},
						},
					},
				},
			},
			expectMinScore: 0.3,
			expectMaxScore: 1.0,
			expectMatches:  true,
			sourceCount:    2,
		},
		{
			name:        "partial match with shared entities",
			itemSummary: "Maria Georgiou organizes protest at Larnaca marina",
			evidence: &ExtractedEvidence{
				Source: &db.EvidenceSource{
					URL:    "https://example.com/article",
					Domain: "example.com",
				},
				Claims: []ExtractedClaim{
					{
						Text: "Maria Georgiou, spokesperson for the protest organizers, told Cyprus Mail about the demonstration",
						Entities: []Entity{
							{Text: "Maria Georgiou", Type: entityTypePerson},
							{Text: "Cyprus Mail", Type: entityTypeOrg},
						},
					},
				},
			},
			expectMinScore: 0.15,
			expectMatches:  true,
			sourceCount:    1,
		},
		{
			name:        "no match - different topics",
			itemSummary: "Weather forecast for Nicosia shows rain expected tomorrow",
			evidence: &ExtractedEvidence{
				Source: &db.EvidenceSource{
					URL:    "https://example.com/sports",
					Domain: "example.com",
				},
				Claims: []ExtractedClaim{
					{
						Text: "The football match between Limassol and Paphos ended in a draw",
						Entities: []Entity{
							{Text: "Limassol", Type: entityTypeLoc},
							{Text: "Paphos", Type: entityTypeLoc},
						},
					},
				},
			},
			expectMaxScore: 0.1,
			expectMatches:  false,
			sourceCount:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(tt.itemSummary, tt.evidence)
			verifyScoringResult(t, result, tt.expectMinScore, tt.expectMaxScore, tt.expectMatches)

			// Test tier determination
			tier := scorer.DetermineTier(tt.sourceCount, result.AgreementScore)
			if tt.expectTier != "" && tier != tt.expectTier {
				t.Errorf("tier: got %q, want %q", tier, tt.expectTier)
			}
		})
	}
}

// verifyScoringResult checks that the scoring result meets expectations.
func verifyScoringResult(t *testing.T, result ScoringResult, minScore, maxScore float32, expectMatches bool) {
	t.Helper()

	if minScore > 0 && result.AgreementScore < minScore {
		t.Errorf(errFmtScoreTooLow, result.AgreementScore, minScore)
	}

	if maxScore > 0 && result.AgreementScore > maxScore {
		t.Errorf(errFmtScoreTooHigh, result.AgreementScore, maxScore)
	}

	if expectMatches && len(result.MatchedClaims) == 0 {
		t.Error(errExpectedMatchClaims)
	}

	if !expectMatches && len(result.MatchedClaims) > 0 {
		t.Errorf("expected no matches, got %d", len(result.MatchedClaims))
	}
}

// TestCrossLanguageScoring_Integration tests scoring between Russian and English content.
func TestCrossLanguageScoring_Integration(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name           string
		itemSummary    string
		evidenceClaim  string
		entities       []Entity
		expectMinScore float32
		expectMaxScore float32
	}{
		{
			name:          "russian summary vs english evidence with shared entities",
			itemSummary:   "Протестующие требуют прекратить насмешки над маринай в Ларнаке",
			evidenceClaim: "About 200 people protested outside the Larnaca marina on Saturday demanding authorities end delays",
			entities: []Entity{
				{Text: "Larnaca", Type: entityTypeLoc},
				{Text: "200", Type: "NUMBER"},
			},
			// Entity overlap should provide some score even without token match
			expectMinScore: 0.0,  // May be 0 due to different languages
			expectMaxScore: 0.25, // Shouldn't be too high without translation
		},
		{
			name:          "transliterated entities should match",
			itemSummary:   "Maria Georgiou организовала протест в Ларнаке",
			evidenceClaim: "Maria Georgiou, spokesperson for the protest organizers, addressed the crowd",
			entities: []Entity{
				{Text: "Maria Georgiou", Type: entityTypePerson},
			},
			expectMinScore: 0.1, // Person name entity should match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := &ExtractedEvidence{
				Claims: []ExtractedClaim{
					{
						Text:     tt.evidenceClaim,
						Entities: tt.entities,
					},
				},
			}

			result := scorer.Score(tt.itemSummary, evidence)

			if result.AgreementScore < tt.expectMinScore {
				t.Errorf(errFmtScoreTooLow, result.AgreementScore, tt.expectMinScore)
			}

			if tt.expectMaxScore > 0 && result.AgreementScore > tt.expectMaxScore {
				t.Errorf(errFmtScoreTooHigh, result.AgreementScore, tt.expectMaxScore)
			}
		})
	}
}

// TestFullPipeline_Integration tests the complete flow from link extraction to evidence scoring.
func TestFullPipeline_Integration(t *testing.T) {
	// This test simulates the full pipeline flow without external dependencies
	tests := []struct {
		name            string
		messageText     string
		linkFixture     string
		originalFixture string
		expectLinkCount int
		expectQueries   int
		expectScore     float32
	}{
		{
			name:            "cyprus news with corroborating source",
			messageText:     "Важные новости! http://russiancyprus.news/news/society/stop-the-mockery/",
			linkFixture:     "cyprus_news_ru.html",
			originalFixture: "cyprus_mail_original.html",
			expectLinkCount: 1,
			expectQueries:   2,
			expectScore:     0.3,
		},
	}

	gen := NewQueryGenerator()
	scorer := NewScorer()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: Extract links from message
			extracted := linkextract.ExtractLinks(tt.messageText)

			if len(extracted) != tt.expectLinkCount {
				t.Fatalf("link extraction: got %d links, want %d", len(extracted), tt.expectLinkCount)
			}

			// Step 2: Extract content from link (simulated with fixture)
			linkHTML := loadTestFixture(t, tt.linkFixture)

			linkContent, err := links.ExtractWebContent(linkHTML, extracted[0].URL, maxContentLength)
			if err != nil {
				t.Fatalf("content extraction failed: %v", err)
			}

			if linkContent.Title == "" {
				t.Error("extracted content has no title")
			}

			// Step 3: Generate search queries
			resolvedLinks := []domain.ResolvedLink{
				{
					URL:     extracted[0].URL,
					Domain:  extracted[0].Domain,
					Title:   linkContent.Title,
					Content: linkContent.Content,
				},
			}

			queries := gen.Generate(linkContent.Title, linkContent.Content, "News", "Russian Cyprus News", resolvedLinks)

			if len(queries) < tt.expectQueries {
				t.Errorf("query generation: got %d queries, want at least %d", len(queries), tt.expectQueries)
			}

			// Step 4: Extract evidence from original source (simulated with fixture)
			originalHTML := loadTestFixture(t, tt.originalFixture)

			originalContent, err := links.ExtractWebContent(originalHTML, "https://cyprus-mail.com/article", maxContentLength)
			if err != nil {
				t.Fatalf("original content extraction failed: %v", err)
			}

			// Step 5: Create evidence with claims
			evidence := &ExtractedEvidence{
				Source: &db.EvidenceSource{
					URL:    "https://cyprus-mail.com/article",
					Domain: "cyprus-mail.com",
					Title:  originalContent.Title,
				},
				Claims: []ExtractedClaim{
					{
						Text: originalContent.Content[:min(500, len(originalContent.Content))],
						Entities: []Entity{
							{Text: "Larnaca", Type: entityTypeLoc},
							{Text: "Maria Georgiou", Type: entityTypePerson},
							{Text: "200", Type: "NUMBER"},
						},
					},
				},
			}

			// Step 6: Score the evidence
			result := scorer.Score(linkContent.Content, evidence)

			if result.AgreementScore < tt.expectScore {
				t.Errorf("scoring: got %f, want at least %f", result.AgreementScore, tt.expectScore)
			}

			// Verify the pipeline produced meaningful results
			t.Logf("Pipeline results:")
			t.Logf("  Links extracted: %d", len(extracted))
			t.Logf("  Content title: %s", linkContent.Title)
			t.Logf("  Queries generated: %d", len(queries))
			t.Logf("  Agreement score: %f", result.AgreementScore)
			t.Logf("  Matched claims: %v", result.MatchedClaims)
		})
	}
}
