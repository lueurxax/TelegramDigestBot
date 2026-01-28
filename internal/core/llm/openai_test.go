package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

var (
	errKeyNotFound  = errors.New("key not found")
	errTypeMismatch = errors.New("type mismatch")
	errStoreError   = errors.New("store error")
)

const (
	testErrTruncate                   = "truncate(%q, %d) = %q, want %q"
	testErrExpectedProviderCount      = "expected 1 provider, got %d"
	testModelGPT4                     = "gpt-4"
	testAPIKeyMock                    = "mock"
	testErrGenerateClusterTopic       = "GenerateClusterTopic() error = %v"
	testTopicTechnology               = "Technology"
	testErrGenerateClusterTopicResult = "GenerateClusterTopic() = %q, want %q"
	testDecisionRelevant              = "relevant"
	testReasonMock                    = "mock"
	testPromptProcess                 = "Process this:"
	testErrBuildMessagePartsAtLeast2  = "buildMessageParts() returned %d parts, want at least 2"
	testErrAlignBatchResults          = "alignBatchResults() error = %v"
	testSummaryFirst                  = "First"
	testSummarySecond                 = "Second"
	testErrAlignedSummary             = "aligned[0].Summary = %q, want %q"
	testErrAlignBatchResultsCountTwo  = "alignBatchResults() returned %d results, want 2"
	testErrAlignBySourceChannel       = "alignBySourceChannel() error = %v"
	testErrTranslateText              = "TranslateText() error = %v"
	testErrGenerateNarrative          = "GenerateNarrative() error = %v"
	testErrSummarizeCluster           = "SummarizeCluster() error = %v"
	testErrCompressSummaries          = "CompressSummariesForCover() error = %v"
	testPromptKey                     = "test"
	testPromptActiveKey               = "prompt:" + testPromptKey + ":active"
	testPromptV1Key                   = "prompt:" + testPromptKey + ":v1"
	testPromptV2Key                   = "prompt:" + testPromptKey + ":v2"
	testPromptV2                      = "v2"
	testPromptV3                      = "v3"
	testErrLoadPrompt                 = "loadPrompt() prompt = %q, want %q"
	testErrLoadPromptVersion          = "loadPrompt() version = %q, want %q"
	testPromptCustomV2                = "custom prompt v2"
	testPromptOverride                = "overridden default prompt"
	testWhitespaceOnly                = "   "
	testHello                         = "Hello"
	testErrTranslateHello             = "TranslateText() = %q, want 'Hello'"
	expectedToneProfessional          = "professional"
	expectedToneCasual                = "casual"
	expectedToneBrief                 = "brief"
	errKeyNotFoundFmt                 = "%w: %s"
	expectedDefaultTopic              = "General"
	expectedLinkTypeTelegram          = "telegram"
	expectedLinkTypeWeb               = "web"
)

// testLogger returns a no-op logger for tests
func testLogger() *zerolog.Logger {
	logger := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	return &logger
}

func TestBuildCoverPrompt(t *testing.T) {
	tests := []struct {
		name         string
		topics       []string
		narrative    string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:      "empty topics and narrative",
			topics:    nil,
			narrative: "",
			wantContains: []string{
				"abstract editorial illustration for a news digest",
				"modern conceptual art",
				"Absolutely no text",
			},
			wantMissing: []string{"current events", "news digest covering"},
		},
		{
			name:      "with topics only",
			topics:    []string{testTopicTechnology, "Finance"},
			narrative: "",
			wantContains: []string{
				"news digest covering: Technology, Finance",
				"Absolutely no text",
			},
			wantMissing: []string{"current events"},
		},
		{
			name:      "with narrative only",
			topics:    nil,
			narrative: "Breaking news about tech startups",
			wantContains: []string{
				"representing these current events: Breaking news about tech startups",
				"Absolutely no text",
			},
			wantMissing: []string{"news digest covering"},
		},
		{
			name:      "with both topics and narrative",
			topics:    []string{"Politics", "World News"},
			narrative: "Important summit meeting",
			wantContains: []string{
				// When narrative is present, it takes precedence over topics
				"representing these current events: Important summit meeting",
				"Absolutely no text",
			},
			wantMissing: []string{"Politics", "World News"},
		},
		{
			name:      "long narrative is truncated",
			topics:    nil,
			narrative: strings.Repeat("This is a very long narrative. ", 20),
			wantContains: []string{
				"current events",
				"...", // Truncation indicator
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCoverPrompt(tt.topics, tt.narrative)

			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildCoverPrompt() = %q, want to contain %q", got, s)
				}
			}

			for _, s := range tt.wantMissing {
				if strings.Contains(got, s) {
					t.Errorf("buildCoverPrompt() = %q, should not contain %q", got, s)
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel..."},         // Truncated with ellipsis
		{"hello world", 5, "hello..."}, // Truncated with ellipsis
		{"", 10, ""},
		{"test", 0, "..."}, // Edge case: max 0 adds ellipsis
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)

			if got != tt.want {
				t.Errorf(testErrTruncate, tt.input, tt.max, got, tt.want)
			}
		})
	}
}

func TestBuildCompressSummariesPrompt(t *testing.T) {
	tests := []struct {
		name         string
		summaries    []string
		wantContains []string
	}{
		{
			name:      "single summary",
			summaries: []string{"Trump announces new tariffs on imports"},
			wantContains: []string{
				"Compress each of these news summaries",
				"1. Trump announces new tariffs on imports",
			},
		},
		{
			name:      "multiple summaries",
			summaries: []string{"Tech company merger announced", "Stock market reaches new high", "Climate summit concludes"},
			wantContains: []string{
				"Compress each of these news summaries",
				"1. Tech company merger announced",
				"2. Stock market reaches new high",
				"3. Climate summit concludes",
			},
		},
		{
			name:      "empty summaries",
			summaries: []string{},
			wantContains: []string{
				"Compress each of these news summaries",
			},
		},
		{
			name:      "summary with HTML tags",
			summaries: []string{"<b>Breaking:</b> Major event occurred"},
			wantContains: []string{
				"1. <b>Breaking:</b> Major event occurred",
			},
		},
		{
			name:      "non-English summary",
			summaries: []string{"Новый закон принят парламентом"},
			wantContains: []string{
				"1. Новый закон принят парламентом",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCompressSummariesPrompt(tt.summaries)

			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildCompressSummariesPrompt() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}

func TestGetToneInstruction(t *testing.T) {
	tests := []struct {
		tone string
		want string
	}{
		{ToneProfessional, "Write in a formal, journalistic tone."},
		{ToneCasual, "Write in a conversational, accessible tone."},
		{ToneBrief, "Be extremely concise, telegram-style."},
		{"Professional", "Write in a formal, journalistic tone."}, // case insensitive
		{"CASUAL", "Write in a conversational, accessible tone."}, // case insensitive
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.tone, func(t *testing.T) {
			got := getToneInstruction(tt.tone)

			if got != tt.want {
				t.Errorf("getToneInstruction(%q) = %q, want %q", tt.tone, got, tt.want)
			}
		})
	}
}

func TestApplyPromptTokens(t *testing.T) {
	tests := []struct {
		name            string
		prompt          string
		langInstruction string
		count           int
		wantContains    []string
		wantMissing     []string
	}{
		{
			name:            "replaces count placeholder",
			prompt:          "Process {{MESSAGE_COUNT}} messages",
			langInstruction: "",
			count:           5,
			wantContains:    []string{"Process 5 messages"},
			wantMissing:     []string{"{{MESSAGE_COUNT}}"},
		},
		{
			name:            "replaces lang placeholder",
			prompt:          "Summarize{{LANG_INSTRUCTION}}",
			langInstruction: " in English",
			count:           1,
			wantContains:    []string{"Summarize in English"},
			wantMissing:     []string{"{{LANG_INSTRUCTION}}"},
		},
		{
			name:            "replaces both placeholders",
			prompt:          "Process {{MESSAGE_COUNT}} messages{{LANG_INSTRUCTION}}",
			langInstruction: " in Russian",
			count:           10,
			wantContains:    []string{"Process 10 messages", "in Russian"},
		},
		{
			name:            "appends lang instruction when no placeholder",
			prompt:          "Simple prompt without placeholder",
			langInstruction: " Write in German.",
			count:           3,
			wantContains:    []string{"Simple prompt", "Write in German."},
		},
		{
			name:            "no lang instruction and no placeholder",
			prompt:          "Simple prompt",
			langInstruction: "",
			count:           1,
			wantContains:    []string{"Simple prompt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyPromptTokens(tt.prompt, tt.langInstruction, tt.count)

			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("applyPromptTokens() = %q, want to contain %q", got, s)
				}
			}

			for _, s := range tt.wantMissing {
				if strings.Contains(got, s) {
					t.Errorf("applyPromptTokens() = %q, should not contain %q", got, s)
				}
			}
		})
	}
}

func TestPromptActiveKey(t *testing.T) {
	tests := []struct {
		baseKey string
		want    string
	}{
		{"summarize", "prompt:summarize:active"},
		{"narrative", "prompt:narrative:active"},
		{"", "prompt::active"},
	}

	for _, tt := range tests {
		t.Run(tt.baseKey, func(t *testing.T) {
			if got := promptActiveKey(tt.baseKey); got != tt.want {
				t.Errorf("promptActiveKey(%q) = %q, want %q", tt.baseKey, got, tt.want)
			}
		})
	}
}

func TestPromptVersionKey(t *testing.T) {
	tests := []struct {
		baseKey string
		version string
		want    string
	}{
		{"summarize", "v1", "prompt:summarize:v1"},
		{"narrative", "v2", "prompt:narrative:v2"},
		{"", "", "prompt::"},
	}

	for _, tt := range tests {
		t.Run(tt.baseKey+"_"+tt.version, func(t *testing.T) {
			if got := promptVersionKey(tt.baseKey, tt.version); got != tt.want {
				t.Errorf("promptVersionKey(%q, %q) = %q, want %q", tt.baseKey, tt.version, got, tt.want)
			}
		})
	}
}

func TestTryParseWrapper(t *testing.T) {
	c := &openaiClient{}

	tests := []struct {
		name    string
		content string
		wantLen int
	}{
		{
			name:    "valid wrapper with results",
			content: `{"results": [{"index": 0, "summary": "Test"}]}`,
			wantLen: 1,
		},
		{
			name:    "valid wrapper with multiple results",
			content: fmt.Sprintf(`{"results": [{"index": 0, "summary": "%s"}, {"index": 1, "summary": "%s"}]}`, testSummaryFirst, testSummarySecond),
			wantLen: 2,
		},
		{
			name:    "empty results array",
			content: `{"results": []}`,
			wantLen: 0,
		},
		{
			name:    "invalid JSON",
			content: `{invalid}`,
			wantLen: 0,
		},
		{
			name:    "no results key",
			content: `{"data": [{"index": 0}]}`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.tryParseWrapper(tt.content)
			if len(got) != tt.wantLen {
				t.Errorf("tryParseWrapper() returned %d results, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestTryParseArray(t *testing.T) {
	c := &openaiClient{}

	tests := []struct {
		name    string
		content string
		wantLen int
	}{
		{
			name:    "valid array",
			content: `[{"index": 0, "summary": "Test"}]`,
			wantLen: 1,
		},
		{
			name:    "multiple items",
			content: `[{"index": 0}, {"index": 1}, {"index": 2}]`,
			wantLen: 3,
		},
		{
			name:    "empty array",
			content: `[]`,
			wantLen: 0,
		},
		{
			name:    "invalid JSON",
			content: `[invalid]`,
			wantLen: 0,
		},
		{
			name:    "object instead of array",
			content: `{"index": 0}`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.tryParseArray(tt.content)
			if len(got) != tt.wantLen {
				t.Errorf("tryParseArray() returned %d results, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestTryFindArrayInJSON(t *testing.T) {
	c := &openaiClient{}

	tests := []struct {
		name    string
		content string
		wantLen int
	}{
		{
			name:    "finds array in object",
			content: `{"data": [{"index": 0, "summary": "Test"}]}`,
			wantLen: 1,
		},
		{
			name:    "finds array with different key",
			content: `{"items": [{"index": 0}, {"index": 1}]}`,
			wantLen: 2,
		},
		{
			name:    "empty array returns nothing",
			content: `{"data": []}`,
			wantLen: 0,
		},
		{
			name:    "invalid JSON",
			content: `{invalid}`,
			wantLen: 0,
		},
		{
			name:    "no array values",
			content: `{"key": "value", "num": 123}`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.tryFindArrayInJSON(tt.content)
			if len(got) != tt.wantLen {
				t.Errorf("tryFindArrayInJSON() returned %d results, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestPopulateResultsByIndex(t *testing.T) {
	c := &openaiClient{}

	tests := []struct {
		name         string
		results      []BatchResult
		messageCount int
		wantLen      int
		wantAllZero  bool
	}{
		{
			name: "normal indexing",
			results: []BatchResult{
				{Index: 0, Summary: testSummaryFirst},
				{Index: 1, Summary: testSummarySecond},
			},
			messageCount: 2,
			wantLen:      2,
			wantAllZero:  false,
		},
		{
			name: "all zero indices",
			results: []BatchResult{
				{Index: 0, Summary: testSummaryFirst},
				{Index: 0, Summary: testSummarySecond},
			},
			messageCount: 2,
			wantLen:      2,
			wantAllZero:  true,
		},
		{
			name: "out of range indices ignored",
			results: []BatchResult{
				{Index: 0, Summary: "Valid"},
				{Index: 10, Summary: "Invalid"},
			},
			messageCount: 2,
			wantLen:      2,
			wantAllZero:  false,
		},
		{
			name: "negative indices ignored",
			results: []BatchResult{
				{Index: 0, Summary: "Valid"},
				{Index: -1, Summary: "Invalid"},
			},
			messageCount: 2,
			wantLen:      2,
			wantAllZero:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, allZero := c.populateResultsByIndex(tt.results, tt.messageCount)

			if len(got) != tt.wantLen {
				t.Errorf("populateResultsByIndex() returned %d results, want %d", len(got), tt.wantLen)
			}

			if allZero != tt.wantAllZero {
				t.Errorf("populateResultsByIndex() allZero = %v, want %v", allZero, tt.wantAllZero)
			}
		})
	}
}

func TestBuildResolvedLinksText(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	tests := []struct {
		name         string
		links        []domain.ResolvedLink
		wantContains []string
	}{
		{
			name: "telegram link",
			links: []domain.ResolvedLink{
				{
					LinkType:     LinkTypeTelegram,
					ChannelTitle: "TestChannel",
					Content:      "Some content here",
					Views:        1000,
				},
			},
			wantContains: []string{"[Telegram]", "TestChannel", "Some content", "1000 views"},
		},
		{
			name: "web link",
			links: []domain.ResolvedLink{
				{
					LinkType: "web",
					Domain:   "example.com",
					Title:    "Article Title",
					Content:  "Article content",
				},
			},
			wantContains: []string{"[Web]", "example.com", "Article Title", "Article content"},
		},
		{
			name: "telegram link without views",
			links: []domain.ResolvedLink{
				{
					LinkType:     LinkTypeTelegram,
					ChannelTitle: "Channel",
					Content:      "Content",
					Views:        0,
				},
			},
			wantContains: []string{"[Telegram]", "Channel"},
		},
		{
			name:         "empty links",
			links:        []domain.ResolvedLink{},
			wantContains: []string{"[Referenced Content:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.buildResolvedLinksText(tt.links)
			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildResolvedLinksText() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}

func TestBuildMessageTextPart(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	tests := []struct {
		name         string
		index        int
		input        MessageInput
		wantContains []string
	}{
		{
			name:  "basic message",
			index: 0,
			input: MessageInput{
				RawMessage: domain.RawMessage{Text: "Hello world"},
			},
			wantContains: []string{"[0]", ">>> MESSAGE TO SUMMARIZE <<<", "Hello world"},
		},
		{
			name:  "with channel title",
			index: 1,
			input: MessageInput{
				RawMessage: domain.RawMessage{
					Text:         "Content",
					ChannelTitle: "TestChannel",
				},
			},
			wantContains: []string{"[1]", "(Source Channel: TestChannel)", "Content"},
		},
		{
			name:  "with all metadata",
			index: 2,
			input: MessageInput{
				RawMessage: domain.RawMessage{
					Text:               "Message",
					ChannelTitle:       "Channel",
					ChannelContext:     "Context info",
					ChannelDescription: "Description",
					ChannelCategory:    testTopicTechnology,
					ChannelTone:        "Professional",
					ChannelUpdateFreq:  "Daily",
				},
			},
			wantContains: []string{
				"[2]",
				"(Source Channel: Channel)",
				"(Channel Context: Context info)",
				"(Channel Description: Description)",
				"(Channel Category: " + testTopicTechnology + ")",
				"(Channel Tone: Professional)",
				"(Channel Frequency: Daily)",
			},
		},
		{
			name:  "with context array",
			index: 0,
			input: MessageInput{
				RawMessage: domain.RawMessage{Text: "Message"},
				Context:    []string{"Previous message 1", "Previous message 2"},
			},
			wantContains: []string{"[BACKGROUND CONTEXT", "Previous message 1", "Previous message 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.buildMessageTextPart(tt.index, tt.input)
			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildMessageTextPart() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}

// Tests for mock client (increases coverage for llm.go)

func TestNew_MockClient(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	if client == nil {
		t.Fatal("New() returned nil for empty API key")
	}

	// Should return a Registry containing mock provider
	registry, ok := client.(*Registry)
	if !ok {
		t.Error("expected *Registry for empty API key")
	}

	// Verify it has exactly one provider (mock)
	if registry.ProviderCount() != 1 {
		t.Errorf(testErrExpectedProviderCount, registry.ProviderCount())
	}
}

func TestNew_MockClientExplicit(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: testAPIKeyMock}
	client := New(context.Background(), cfg, nil, nil, nil)

	if client == nil {
		t.Fatal("New() returned nil for mock API key")
	}

	// Should return a Registry containing mock provider
	registry, ok := client.(*Registry)
	if !ok {
		t.Error("expected *Registry for mock API key")
	}

	// Verify it has exactly one provider (mock)
	if registry.ProviderCount() != 1 {
		t.Errorf(testErrExpectedProviderCount, registry.ProviderCount())
	}
}

func TestMockClient_ProcessBatch(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	messages := []MessageInput{
		{RawMessage: domain.RawMessage{Text: "Message 1", ChannelTitle: "Channel1"}},
		{RawMessage: domain.RawMessage{Text: "Message 2", ChannelTitle: "Channel2"}},
	}

	results, err := client.ProcessBatch(context.Background(), messages, "en", testModelGPT4, ToneProfessional)
	if err != nil {
		t.Fatalf("ProcessBatch() error = %v", err)
	}

	if len(results) != len(messages) {
		t.Errorf("ProcessBatch() returned %d results, want %d", len(results), len(messages))
	}

	for i, r := range results {
		if r.Index != i {
			t.Errorf("result[%d].Index = %d, want %d", i, r.Index, i)
		}

		if r.SourceChannel != messages[i].ChannelTitle {
			t.Errorf("result[%d].SourceChannel = %s, want %s", i, r.SourceChannel, messages[i].ChannelTitle)
		}

		if r.Summary == "" {
			t.Errorf("result[%d].Summary is empty", i)
		}
	}
}

func TestMockClient_TranslateText(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	text := "Hello world"

	result, err := client.TranslateText(context.Background(), text, "ru", testModelGPT4)
	if err != nil {
		t.Fatalf(testErrTranslateText, err)
	}

	if result != text {
		t.Errorf("TranslateText() = %q, want %q (mock returns same text)", result, text)
	}
}

func TestMockClient_GenerateNarrative(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	items := []domain.Item{
		{ID: "1", Summary: "Summary 1"},
		{ID: "2", Summary: "Summary 2"},
	}

	result, err := client.GenerateNarrative(context.Background(), items, "en", testModelGPT4, ToneProfessional)
	if err != nil {
		t.Fatalf(testErrGenerateNarrative, err)
	}

	if result == "" {
		t.Error("GenerateNarrative() returned empty string")
	}

	if !strings.Contains(result, "2 items") {
		t.Errorf("GenerateNarrative() = %q, should contain item count", result)
	}

	if !strings.Contains(result, ToneProfessional) {
		t.Errorf("GenerateNarrative() = %q, should contain tone", result)
	}
}

func TestMockClient_SummarizeCluster(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	items := []domain.Item{
		{ID: "1", Summary: "Summary 1"},
		{ID: "2", Summary: "Summary 2"},
		{ID: "3", Summary: "Summary 3"},
	}

	result, err := client.SummarizeCluster(context.Background(), items, "en", testModelGPT4, ToneCasual)
	if err != nil {
		t.Fatalf(testErrSummarizeCluster, err)
	}

	if result == "" {
		t.Error("SummarizeCluster() returned empty string")
	}

	if !strings.Contains(result, "3") {
		t.Errorf("SummarizeCluster() = %q, should contain item count", result)
	}
}

func TestMockClient_GenerateClusterTopic(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	t.Run("with items", func(t *testing.T) {
		items := []domain.Item{
			{ID: "1", Topic: testTopicTechnology},
			{ID: "2", Topic: "Finance"},
		}

		result, err := client.GenerateClusterTopic(context.Background(), items, "en", testModelGPT4)
		if err != nil {
			t.Fatalf(testErrGenerateClusterTopic, err)
		}

		// Mock returns first item's topic
		if result != testTopicTechnology {
			t.Errorf(testErrGenerateClusterTopicResult, result, testTopicTechnology)
		}
	})

	t.Run("empty items", func(t *testing.T) {
		result, err := client.GenerateClusterTopic(context.Background(), []domain.Item{}, "en", testModelGPT4)
		if err != nil {
			t.Fatalf(testErrGenerateClusterTopic, err)
		}

		if result != DefaultTopic {
			t.Errorf(testErrGenerateClusterTopicResult, result, DefaultTopic)
		}
	})
}

func TestMockClient_RelevanceGate(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	result, err := client.RelevanceGate(context.Background(), "some text", testModelGPT4, "custom prompt")
	if err != nil {
		t.Fatalf("RelevanceGate() error = %v", err)
	}

	if result.Decision != testDecisionRelevant {
		t.Errorf("RelevanceGate().Decision = %q, want %q", result.Decision, testDecisionRelevant)
	}

	if result.Reason != testReasonMock {
		t.Errorf("RelevanceGate().Reason = %q, want %q", result.Reason, testReasonMock)
	}
}

func TestMockClient_CompressSummariesForCover(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	summaries := []string{"Summary one", "Summary two", "Summary three"}

	result, err := client.CompressSummariesForCover(context.Background(), summaries)
	if err != nil {
		t.Fatalf(testErrCompressSummaries, err)
	}

	if len(result) != len(summaries) {
		t.Errorf("CompressSummariesForCover() returned %d, want %d", len(result), len(summaries))
	}

	// Mock returns same summaries
	for i := range summaries {
		if result[i] != summaries[i] {
			t.Errorf("CompressSummariesForCover()[%d] = %q, want %q", i, result[i], summaries[i])
		}
	}
}

func TestMockClient_GenerateDigestCover(t *testing.T) {
	cfg := &config.Config{LLMAPIKey: ""}
	client := New(context.Background(), cfg, nil, nil, nil)

	result, err := client.GenerateDigestCover(context.Background(), []string{"Tech", "News"}, "Some narrative")
	// Mock provider doesn't support image generation, so Registry returns ErrNoImageProvider
	if err == nil {
		t.Fatalf("GenerateDigestCover() expected error for mock provider")
	}

	if result != nil {
		t.Errorf("GenerateDigestCover() = %v, want nil", result)
	}
}

// Tests for openaiClient helper methods

func TestBuildLangInstruction(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	tests := []struct {
		name         string
		targetLang   string
		tone         string
		wantContains []string
		wantEmpty    bool
	}{
		{
			name:       "empty language and tone",
			targetLang: "",
			tone:       "",
			wantEmpty:  true,
		},
		{
			name:         "language only",
			targetLang:   "Russian",
			tone:         "",
			wantContains: []string{"Russian", "IMPORTANT", "Write all outputs"},
		},
		{
			name:         "tone only",
			targetLang:   "",
			tone:         ToneProfessional,
			wantContains: []string{"formal", "journalistic"},
		},
		{
			name:         "both language and tone",
			targetLang:   "Spanish",
			tone:         ToneCasual,
			wantContains: []string{"Spanish", "conversational"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.buildLangInstruction(tt.targetLang, tt.tone)

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("buildLangInstruction() = %q, want empty", got)
				}

				return
			}

			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildLangInstruction() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name       string
		cfgModel   string
		inputModel string
		want       string
	}{
		{
			name:       "use input model when provided",
			cfgModel:   testModelGPT4,
			inputModel: "gpt-3.5-turbo",
			want:       "gpt-3.5-turbo",
		},
		{
			name:       "fallback to config model",
			cfgModel:   "gpt-4-turbo",
			inputModel: "",
			want:       "gpt-4-turbo",
		},
		{
			name:       "fallback to default when both empty",
			cfgModel:   "",
			inputModel: "",
			want:       "gpt-4o-mini",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &openaiClient{cfg: &config.Config{LLMModel: tt.cfgModel}}
			got := c.resolveModel(tt.inputModel)

			if got != tt.want {
				t.Errorf("resolveModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMessageParts(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	t.Run("single message without media", func(t *testing.T) {
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Hello world"}},
		}

		parts := c.buildMessageParts(messages, testPromptProcess)

		// Should have at least 2 parts: prompt + message
		if len(parts) < 2 {
			t.Errorf(testErrBuildMessagePartsAtLeast2, len(parts))
		}

		// First part should be the prompt
		if parts[0].Text != testPromptProcess {
			t.Errorf("first part = %q, want %q", parts[0].Text, testPromptProcess)
		}
	})

	t.Run("multiple messages", func(t *testing.T) {
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Message 1"}},
			{RawMessage: domain.RawMessage{Text: "Message 2"}},
			{RawMessage: domain.RawMessage{Text: "Message 3"}},
		}

		parts := c.buildMessageParts(messages, "Summarize:")

		// Should have prompt + 3 messages
		if len(parts) != 4 {
			t.Errorf("buildMessageParts() returned %d parts, want 4", len(parts))
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		parts := c.buildMessageParts([]MessageInput{}, "Prompt only")

		// Should have just the prompt
		if len(parts) != 1 {
			t.Errorf("buildMessageParts() returned %d parts, want 1", len(parts))
		}
	})
}

func TestAlignBatchResults(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	t.Run("normal indexing", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst},
			{Index: 1, Summary: testSummarySecond},
		}
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Msg 1", ChannelTitle: "Ch1"}},
			{RawMessage: domain.RawMessage{Text: "Msg 2", ChannelTitle: "Ch2"}},
		}

		aligned, err := c.alignBatchResults(results, messages)
		if err != nil {
			t.Fatalf(testErrAlignBatchResults, err)
		}

		if len(aligned) != len(messages) {
			t.Errorf("alignBatchResults() returned %d results, want %d", len(aligned), len(messages))
		}

		if aligned[0].Summary != testSummaryFirst {
			t.Errorf(testErrAlignedSummary, aligned[0].Summary, testSummaryFirst)
		}
	})

	t.Run("all zero indices with source channel matching", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst, SourceChannel: "Channel1"},
			{Index: 0, Summary: testSummarySecond, SourceChannel: "Channel2"},
		}
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Msg 1", ChannelTitle: "Channel1"}},
			{RawMessage: domain.RawMessage{Text: "Msg 2", ChannelTitle: "Channel2"}},
		}

		aligned, err := c.alignBatchResults(results, messages)
		if err != nil {
			t.Fatalf(testErrAlignBatchResults, err)
		}

		if len(aligned) != 2 {
			t.Errorf(testErrAlignBatchResultsCountTwo, len(aligned))
		}
	})

	t.Run("all zero indices insufficient channel matching", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst, SourceChannel: "Unknown1"},
			{Index: 0, Summary: testSummarySecond, SourceChannel: "Unknown2"},
		}
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Msg 1", ChannelTitle: "Channel1"}},
			{RawMessage: domain.RawMessage{Text: "Msg 2", ChannelTitle: "Channel2"}},
		}

		aligned, err := c.alignBatchResults(results, messages)
		if err != nil {
			t.Fatalf(testErrAlignBatchResults, err)
		}

		// Should return original results when matching fails
		if len(aligned) != 2 {
			t.Errorf(testErrAlignBatchResultsCountTwo, len(aligned))
		}
	})
}

func TestAlignBySourceChannel(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	t.Run("successful matching", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst, SourceChannel: "Channel1"},
			{Index: 0, Summary: testSummarySecond, SourceChannel: "Channel2"},
		}
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Msg 1", ChannelTitle: "Channel1"}},
			{RawMessage: domain.RawMessage{Text: "Msg 2", ChannelTitle: "Channel2"}},
		}

		aligned, err := c.alignBySourceChannel(results, messages)
		if err != nil {
			t.Fatalf(testErrAlignBySourceChannel, err)
		}

		if aligned[0].Summary != testSummaryFirst {
			t.Errorf(testErrAlignedSummary, aligned[0].Summary, testSummaryFirst)
		}

		if aligned[1].Summary != testSummarySecond {
			t.Errorf("aligned[1].Summary = %q, want %q", aligned[1].Summary, testSummarySecond)
		}
	})

	t.Run("partial matching triggers fallback", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst, SourceChannel: "Channel1"},
			{Index: 0, Summary: testSummarySecond, SourceChannel: "Channel2"},
			{Index: 0, Summary: "Third", SourceChannel: "Channel3"},
		}
		messages := []MessageInput{
			{RawMessage: domain.RawMessage{Text: "Msg 1", ChannelTitle: "Channel1"}},
			{RawMessage: domain.RawMessage{Text: "Msg 2", ChannelTitle: "OtherChannel"}},
			{RawMessage: domain.RawMessage{Text: "Msg 3", ChannelTitle: "DifferentChannel"}},
		}

		aligned, err := c.alignBySourceChannel(results, messages)
		if err != nil {
			t.Fatalf(testErrAlignBySourceChannel, err)
		}

		// With less than half matching, should return original
		if len(aligned) != 3 {
			t.Errorf("alignBySourceChannel() returned %d results, want 3", len(aligned))
		}
	})
}

func TestFillUnmatchedResults(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	aligned := make([]BatchResult, 3)
	aligned[0] = BatchResult{Summary: "Matched"}

	results := []BatchResult{
		{Summary: "Used"},
		{Summary: "Unmatched 1"},
		{Summary: "Unmatched 2"},
	}
	messages := []MessageInput{
		{RawMessage: domain.RawMessage{ChannelTitle: "Ch1"}},
		{RawMessage: domain.RawMessage{ChannelTitle: "Ch2"}},
		{RawMessage: domain.RawMessage{ChannelTitle: "Ch3"}},
	}
	usedResults := map[int]bool{0: true}

	c.fillUnmatchedResults(aligned, results, messages, usedResults)

	// Check that unmatched slots are filled
	if aligned[1].Summary == "" {
		t.Error("aligned[1] should be filled")
	}

	if aligned[2].Summary == "" {
		t.Error("aligned[2] should be filled")
	}
}

func TestLogMissingIndices(_ *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	// This test just ensures the function doesn't panic
	foundIndices := map[int]bool{0: true, 2: true}
	c.logMissingIndices(foundIndices, 4) // indices 1 and 3 are missing
}

func TestParseResponseJSON(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	tests := []struct {
		name      string
		content   string
		wantLen   int
		wantError bool
	}{
		{
			name:    "wrapper format",
			content: `{"results": [{"index": 0, "summary": "Test"}]}`,
			wantLen: 1,
		},
		{
			name:    "array format",
			content: `[{"index": 0, "summary": "Test"}]`,
			wantLen: 1,
		},
		{
			name:    "arbitrary wrapper",
			content: `{"data": [{"index": 0, "summary": "Test"}]}`,
			wantLen: 1,
		},
		{
			name:      "invalid JSON",
			content:   `{invalid}`,
			wantLen:   0,
			wantError: true,
		},
		{
			name:      "empty",
			content:   ``,
			wantLen:   0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.parseResponseJSON(tt.content)
			if tt.wantError && err == nil {
				t.Errorf("parseResponseJSON() expected error, got nil")
			}

			if !tt.wantError && err != nil {
				t.Errorf("parseResponseJSON() unexpected error: %v", err)
			}

			if len(got) != tt.wantLen {
				t.Errorf("parseResponseJSON() returned %d results, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestBuildMessagePartsWithMedia(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	// Message with media data (PNG header)
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	messages := []MessageInput{
		{
			RawMessage: domain.RawMessage{
				Text:      "Message with image",
				MediaData: pngData,
			},
		},
	}

	parts := c.buildMessageParts(messages, "Process:")

	// Should have: prompt + message text + image
	if len(parts) < 3 {
		t.Errorf("buildMessageParts() returned %d parts, want at least 3", len(parts))
	}

	// Check for image URL part
	hasImagePart := false

	for _, p := range parts {
		if p.ImageURL != nil {
			hasImagePart = true

			break
		}
	}

	if !hasImagePart {
		t.Error("buildMessageParts() should include image URL part")
	}
}

func TestBuildMessagePartsWithResolvedLinks(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	messages := []MessageInput{
		{
			RawMessage: domain.RawMessage{Text: "Check this link"},
			ResolvedLinks: []domain.ResolvedLink{
				{
					LinkType:     "telegram",
					ChannelTitle: "TestChannel",
					Content:      "Referenced content",
					Views:        500,
				},
			},
		},
	}

	parts := c.buildMessageParts(messages, "Analyze:")

	// Should have prompt + message (which contains resolved links text)
	if len(parts) < 2 {
		t.Errorf(testErrBuildMessagePartsAtLeast2, len(parts))
	}

	// Check that resolved links are included in the message text
	found := false

	for _, p := range parts {
		if strings.Contains(p.Text, "Referenced content") {
			found = true

			break
		}
	}

	if !found {
		t.Error("buildMessageParts() should include resolved links content")
	}
}

func TestBuildResolvedLinksTextVariations(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	t.Run("youtube link", func(t *testing.T) {
		links := []domain.ResolvedLink{
			{
				LinkType: "youtube",
				Domain:   "youtube.com",
				Title:    "Video Title",
				Content:  "Video description",
			},
		}

		result := c.buildResolvedLinksText(links)

		if !strings.Contains(result, "youtube.com") {
			t.Error("should contain domain for youtube")
		}
	})

	t.Run("twitter link", func(t *testing.T) {
		links := []domain.ResolvedLink{
			{
				LinkType: "twitter",
				Domain:   "twitter.com",
				Content:  "Tweet content",
			},
		}

		result := c.buildResolvedLinksText(links)

		if !strings.Contains(result, "Tweet content") {
			t.Error("should contain tweet content")
		}
	})

	t.Run("multiple links", func(t *testing.T) {
		links := []domain.ResolvedLink{
			{LinkType: "web", Domain: "example.com", Title: "Link 1"},
			{LinkType: "web", Domain: "test.com", Title: "Link 2"},
		}

		result := c.buildResolvedLinksText(links)

		if !strings.Contains(result, "example.com") || !strings.Contains(result, "test.com") {
			t.Error("should contain both domains")
		}
	})
}

func TestRecordSuccessAndFailure(t *testing.T) {
	t.Run("recordSuccess resets failures", func(t *testing.T) {
		c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}
		c.consecutiveFailures = 5
		c.recordSuccess()

		if c.consecutiveFailures != 0 {
			t.Errorf("consecutiveFailures = %d, want 0", c.consecutiveFailures)
		}
	})

	t.Run("recordFailure increments counter", func(t *testing.T) {
		c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}
		c.recordFailure()

		if c.consecutiveFailures != 1 {
			t.Errorf("consecutiveFailures = %d, want 1", c.consecutiveFailures)
		}
	})

	t.Run("circuit breaker opens after threshold", func(t *testing.T) {
		c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

		// Trigger enough failures to open circuit
		for i := 0; i < 6; i++ {
			c.recordFailure()
		}

		// Circuit should be open
		err := c.checkCircuit()
		if err == nil {
			t.Error("checkCircuit() should return error when circuit is open")
		}
	})

	t.Run("checkCircuit succeeds when closed", func(t *testing.T) {
		c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

		err := c.checkCircuit()
		if err != nil {
			t.Errorf("checkCircuit() unexpected error: %v", err)
		}
	})
}

func TestPopulateResultsByIndexEdgeCases(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	t.Run("duplicate indices", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst},
			{Index: 0, Summary: "Duplicate"},
			{Index: 1, Summary: testSummarySecond},
		}

		final, found, _ := c.populateResultsByIndex(results, 2)

		if !found[0] || !found[1] {
			t.Error("should mark both indices as found")
		}

		// First result at index 0 should be kept
		if final[0].Summary != testSummaryFirst {
			t.Errorf("final[0].Summary = %q, want %q", final[0].Summary, testSummaryFirst)
		}
	})

	t.Run("sparse indices", func(t *testing.T) {
		results := []BatchResult{
			{Index: 0, Summary: testSummaryFirst},
			{Index: 4, Summary: "Fifth"},
		}

		final, found, _ := c.populateResultsByIndex(results, 5)

		if !found[0] || !found[4] {
			t.Error("should mark provided indices as found")
		}

		if found[1] || found[2] || found[3] {
			t.Error("should not mark missing indices as found")
		}

		// Verify sparse results are filled
		if final[0].Summary != testSummaryFirst {
			t.Errorf("final[0] = %q, want %q", final[0].Summary, testSummaryFirst)
		}
	})
}

func TestTruncateEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"", 0, ""},
		{"a", 1, "a"},
		{"ab", 1, "a..."},
		{"abcdefghij", 5, "abcde..."},
		{"unicode строка", 7, "unicode..."},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf(testErrTruncate, tt.input, tt.max, got, tt.want)
		}
	}
}

// mockPromptStore implements PromptStore for testing
type mockPromptStore struct {
	settings map[string]interface{}
	err      error
}

func (m *mockPromptStore) GetSetting(_ context.Context, key string, target interface{}) error {
	if m.err != nil {
		return m.err
	}

	val, ok := m.settings[key]
	if !ok {
		return fmt.Errorf(errKeyNotFoundFmt, errKeyNotFound, key)
	}

	// Handle string target
	if strPtr, ok := target.(*string); ok {
		if strVal, ok := val.(string); ok {
			*strPtr = strVal
			return nil
		}
	}

	return fmt.Errorf("%w for key %s", errTypeMismatch, key)
}

//nolint:gocyclo // table-driven test with many cases
func TestLoadPrompt(t *testing.T) {
	defaultPrompt := "default prompt text"

	t.Run("nil store uses default", func(t *testing.T) {
		c := &openaiClient{cfg: &config.Config{}, promptStore: nil}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != defaultPrompt {
			t.Errorf(testErrLoadPrompt, prompt, defaultPrompt)
		}

		if version != promptDefaultVersion {
			t.Errorf(testErrLoadPromptVersion, version, promptDefaultVersion)
		}
	})

	t.Run("store error uses default", func(t *testing.T) {
		store := &mockPromptStore{
			err: errStoreError,
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != defaultPrompt {
			t.Errorf(testErrLoadPrompt, prompt, defaultPrompt)
		}

		if version != promptDefaultVersion {
			t.Errorf(testErrLoadPromptVersion, version, promptDefaultVersion)
		}
	})

	t.Run("custom active version", func(t *testing.T) {
		store := &mockPromptStore{
			settings: map[string]interface{}{
				testPromptActiveKey: testPromptV2,
				testPromptV2Key:     testPromptCustomV2,
			},
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != testPromptCustomV2 {
			t.Errorf(testErrLoadPrompt, prompt, testPromptCustomV2)
		}

		if version != testPromptV2 {
			t.Errorf(testErrLoadPromptVersion, version, testPromptV2)
		}
	})

	t.Run("active version set but prompt not found uses default", func(t *testing.T) {
		store := &mockPromptStore{
			settings: map[string]interface{}{
				testPromptActiveKey: testPromptV3,
				// v3 prompt not set
			},
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != defaultPrompt {
			t.Errorf(testErrLoadPrompt, prompt, defaultPrompt)
		}

		if version != testPromptV3 {
			t.Errorf(testErrLoadPromptVersion, version, testPromptV3)
		}
	})

	t.Run("empty active version string uses default", func(t *testing.T) {
		store := &mockPromptStore{
			settings: map[string]interface{}{
				testPromptActiveKey: "",
			},
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != defaultPrompt {
			t.Errorf(testErrLoadPrompt, prompt, defaultPrompt)
		}

		if version != promptDefaultVersion {
			t.Errorf(testErrLoadPromptVersion, version, promptDefaultVersion)
		}
	})

	t.Run("whitespace-only active version uses default", func(t *testing.T) {
		store := &mockPromptStore{
			settings: map[string]interface{}{
				testPromptActiveKey: testWhitespaceOnly,
			},
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != defaultPrompt {
			t.Errorf(testErrLoadPrompt, prompt, defaultPrompt)
		}

		if version != promptDefaultVersion {
			t.Errorf(testErrLoadPromptVersion, version, promptDefaultVersion)
		}
	})

	t.Run("default version prompt override", func(t *testing.T) {
		store := &mockPromptStore{
			settings: map[string]interface{}{
				testPromptV1Key: testPromptOverride,
			},
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, version := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != testPromptOverride {
			t.Errorf(testErrLoadPrompt, prompt, testPromptOverride)
		}

		if version != promptDefaultVersion {
			t.Errorf(testErrLoadPromptVersion, version, promptDefaultVersion)
		}
	})

	t.Run("empty prompt override uses default", func(t *testing.T) {
		store := &mockPromptStore{
			settings: map[string]interface{}{
				testPromptV1Key: "",
			},
		}
		c := &openaiClient{cfg: &config.Config{}, promptStore: store}
		prompt, _ := c.loadPrompt(context.Background(), testPromptKey, defaultPrompt)

		if prompt != defaultPrompt {
			t.Errorf(testErrLoadPrompt, prompt, defaultPrompt)
		}
	})
}

func TestTranslateTextEmpty(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	t.Run("empty text returns unchanged", func(t *testing.T) {
		result, err := c.TranslateText(context.Background(), "", "en", testModelGPT4)
		if err != nil {
			t.Fatalf(testErrTranslateText, err)
		}

		if result != "" {
			t.Errorf("TranslateText() = %q, want empty", result)
		}
	})

	t.Run("whitespace text returns unchanged", func(t *testing.T) {
		result, err := c.TranslateText(context.Background(), testWhitespaceOnly, "en", testModelGPT4)
		if err != nil {
			t.Fatalf(testErrTranslateText, err)
		}

		if result != testWhitespaceOnly {
			t.Errorf("TranslateText() = %q, want %q", result, testWhitespaceOnly)
		}
	})

	t.Run("empty language returns unchanged", func(t *testing.T) {
		result, err := c.TranslateText(context.Background(), testHello, "", testModelGPT4)
		if err != nil {
			t.Fatalf(testErrTranslateText, err)
		}

		if result != testHello {
			t.Errorf(testErrTranslateHello, result)
		}
	})

	t.Run("whitespace language returns unchanged", func(t *testing.T) {
		result, err := c.TranslateText(context.Background(), testHello, testWhitespaceOnly, testModelGPT4)
		if err != nil {
			t.Fatalf(testErrTranslateText, err)
		}

		if result != testHello {
			t.Errorf(testErrTranslateHello, result)
		}
	})
}

func TestGenerateNarrativeEmpty(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	result, err := c.GenerateNarrative(context.Background(), []domain.Item{}, "en", testModelGPT4, ToneProfessional)
	if err != nil {
		t.Fatalf(testErrGenerateNarrative, err)
	}

	if result != "" {
		t.Errorf("GenerateNarrative() = %q, want empty for empty items", result)
	}
}

func TestSummarizeClusterEmpty(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	result, err := c.SummarizeCluster(context.Background(), []domain.Item{}, "en", testModelGPT4, ToneProfessional)
	if err != nil {
		t.Fatalf(testErrSummarizeCluster, err)
	}

	if result != "" {
		t.Errorf("SummarizeCluster() = %q, want empty for empty items", result)
	}
}

func TestGenerateClusterTopicEmpty(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	result, err := c.GenerateClusterTopic(context.Background(), []domain.Item{}, "en", testModelGPT4)
	if err != nil {
		t.Fatalf("GenerateClusterTopic() error = %v", err)
	}

	if result != "" {
		t.Errorf("GenerateClusterTopic() = %q, want empty for empty items", result)
	}
}

func TestCompressSummariesForCoverEmpty(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	result, err := c.CompressSummariesForCover(context.Background(), []string{}, "")
	if err != nil {
		t.Fatalf(testErrCompressSummaries, err)
	}

	if result != nil {
		t.Errorf("CompressSummariesForCover() = %v, want nil for empty summaries", result)
	}
}

func TestCheckCircuitBreakerRecovery(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}, logger: testLogger()}

	// Open circuit
	for i := 0; i < circuitBreakerThreshold+1; i++ {
		c.recordFailure()
	}

	// Circuit should be open
	err := c.checkCircuit()
	if err == nil {
		t.Error("expected circuit to be open")
	}

	// Simulate time passing by resetting circuitOpenUntil
	c.mu.Lock()
	c.circuitOpenUntil = c.circuitOpenUntil.Add(-2 * circuitBreakerTimeout)
	c.mu.Unlock()

	// Circuit should now be closed
	err = c.checkCircuit()
	if err != nil {
		t.Errorf("expected circuit to be closed after timeout, got error: %v", err)
	}
}

func TestBuildMessageTextPartWithAllFields(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}

	input := MessageInput{
		RawMessage: domain.RawMessage{
			Text:               "Main message text",
			ChannelTitle:       "My Channel",
			ChannelContext:     "Tech news",
			ChannelDescription: "Daily tech updates",
			ChannelCategory:    testTopicTechnology,
			ChannelTone:        ToneProfessional,
			ChannelUpdateFreq:  "Daily",
		},
		Context: []string{"Previous context 1", "Previous context 2"},
		ResolvedLinks: []domain.ResolvedLink{
			{
				LinkType:     LinkTypeTelegram,
				ChannelTitle: "Source Channel",
				Content:      "Referenced content",
				Views:        5000,
			},
		},
	}

	result := c.buildMessageTextPart(0, input)

	expectedParts := []string{
		"[0]",
		"(Source Channel: My Channel)",
		"(Channel Context: Tech news)",
		"(Channel Description: Daily tech updates)",
		"(Channel Category: " + testTopicTechnology + ")",
		"(Channel Tone: professional)",
		"(Channel Frequency: Daily)",
		"[BACKGROUND CONTEXT",
		"Previous context 1",
		"[Referenced Content:",
		"[Telegram]",
		"Source Channel",
		"5000 views",
		">>> MESSAGE TO SUMMARIZE <<<",
		"Main message text",
	}

	for _, part := range expectedParts {
		if !strings.Contains(result, part) {
			t.Errorf("buildMessageTextPart() missing %q in result: %s", part, result)
		}
	}
}

func TestBuildResolvedLinksTextWebWithDetails(t *testing.T) {
	c := &openaiClient{cfg: &config.Config{}}
	links := []domain.ResolvedLink{
		{
			LinkType: LinkTypeWeb,
			Domain:   "example.com",
			Title:    "Article Title",
			Content:  "This is the article content that should be included.",
		},
	}

	result := c.buildResolvedLinksText(links)

	expectedParts := []string{
		"[Web]",
		"example.com",
		"Title: Article Title",
		"Content: This is the article content",
	}

	for _, part := range expectedParts {
		if !strings.Contains(result, part) {
			t.Errorf("buildResolvedLinksText() missing %q in result: %s", part, result)
		}
	}
}

func TestDefaultTopicConstant(t *testing.T) {
	if DefaultTopic != expectedDefaultTopic {
		t.Errorf("DefaultTopic = %q, want %q", DefaultTopic, expectedDefaultTopic)
	}
}

func TestLinkTypeConstants(t *testing.T) {
	if LinkTypeTelegram != expectedLinkTypeTelegram {
		t.Errorf("LinkTypeTelegram = %q, want %q", LinkTypeTelegram, expectedLinkTypeTelegram)
	}

	if LinkTypeWeb != expectedLinkTypeWeb {
		t.Errorf("LinkTypeWeb = %q, want %q", LinkTypeWeb, expectedLinkTypeWeb)
	}
}

func TestToneConstants(t *testing.T) {
	if ToneProfessional != expectedToneProfessional {
		t.Errorf("ToneProfessional = %q, want %q", ToneProfessional, expectedToneProfessional)
	}

	if ToneCasual != expectedToneCasual {
		t.Errorf("ToneCasual = %q, want %q", ToneCasual, expectedToneCasual)
	}

	if ToneBrief != expectedToneBrief {
		t.Errorf("ToneBrief = %q, want %q", ToneBrief, expectedToneBrief)
	}
}
