package expandedview

import (
	"bytes"
	"strings"
	"testing"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Test constants.
const testTruncationSuffix = "..."

func requireRenderer(t *testing.T) *Renderer {
	t.Helper()

	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	return renderer
}

func TestRenderer_RenderExpanded(t *testing.T) {
	renderer := requireRenderer(t)

	data := &ExpandedViewData{
		Item: &db.ItemDebugDetail{
			ID:              "550e8400-e29b-41d4-a716-446655440000",
			Topic:           "Test Topic",
			Summary:         "Test summary of the item",
			Text:            "Original message text goes here",
			ChannelTitle:    "Test Channel",
			ChannelUsername: "testchannel",
			MessageID:       12345,
			RelevanceScore:  0.85,
			ImportanceScore: 0.72,
			TGDate:          time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		Evidence: []db.ItemEvidenceWithSource{
			{
				ItemEvidence: db.ItemEvidence{
					AgreementScore:  0.9,
					IsContradiction: false,
				},
				Source: db.EvidenceSource{
					URL:         "https://example.com/article1",
					Title:       "Evidence Article 1",
					Domain:      "example.com",
					Description: "Description of the evidence",
				},
			},
		},
		ClusterItems: []ClusterItemView{
			{
				ID:              "cluster-item-1",
				Summary:         "Related item summary",
				Text:            "Full text of the related message",
				ChannelUsername: "relatedchannel",
				MessageID:       67890,
			},
		},
		ChatGPTPrompt:   "Test prompt for ChatGPT",
		OriginalMsgLink: "https://t.me/testchannel/12345",
		GeneratedAt:     time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer

	if err := renderer.RenderExpanded(&buf, data); err != nil {
		t.Fatalf("render expanded failed: %v", err)
	}

	html := buf.String()

	// Verify key content is present
	checks := []struct {
		name    string
		content string
	}{
		{"topic", "Test Topic"},
		{"summary", "Test summary of the item"},
		{"original text", "Original message text goes here"},
		{"channel username", "@testchannel"},
		{"evidence URL", "https://example.com/article1"},
		{"evidence title", "Evidence Article 1"},
		{"cluster item summary", "Related item summary"},
		{"chatgpt prompt", "Test prompt for ChatGPT"},
		{"original message link", "https://t.me/testchannel/12345"},
		{"noindex meta", `name="robots" content="noindex, nofollow"`},
	}

	for _, check := range checks {
		if !strings.Contains(html, check.content) {
			t.Errorf("HTML missing %s: expected to contain %q", check.name, check.content)
		}
	}
}

func TestRenderer_RenderExpanded_NoEvidence(t *testing.T) {
	renderer := requireRenderer(t)

	data := &ExpandedViewData{
		Item: &db.ItemDebugDetail{
			ID:              "550e8400-e29b-41d4-a716-446655440000",
			Topic:           "Test Topic",
			Summary:         "Test summary",
			Text:            "Original text",
			ChannelTitle:    "Test Channel",
			ChannelUsername: "testchannel",
			MessageID:       12345,
			TGDate:          time.Now(),
		},
		Evidence:        nil,
		ClusterItems:    nil,
		ChatGPTPrompt:   "Test prompt",
		OriginalMsgLink: "https://t.me/testchannel/12345",
		GeneratedAt:     time.Now(),
	}

	var buf bytes.Buffer

	if err := renderer.RenderExpanded(&buf, data); err != nil {
		t.Fatalf("render expanded (no evidence) failed: %v", err)
	}

	html := buf.String()

	// Should show "No evidence available" message
	if !strings.Contains(html, "No evidence available") {
		t.Error("HTML should show 'No evidence available' when evidence is empty")
	}
}

func TestRenderer_RenderError(t *testing.T) {
	renderer := requireRenderer(t)

	tests := []struct {
		name        string
		data        *ErrorData
		wantCode    string
		wantTitle   string
		wantMessage string
	}{
		{
			name: "404 not found",
			data: &ErrorData{
				Code:        404,
				Title:       "Not Found",
				Message:     "This item no longer exists.",
				BotUsername: "testbot",
			},
			wantCode:    "404",
			wantTitle:   "Not Found",
			wantMessage: "This item no longer exists.",
		},
		{
			name: "410 expired",
			data: &ErrorData{
				Code:        410,
				Title:       "Link Expired",
				Message:     "This link has expired.",
				BotUsername: "testbot",
			},
			wantCode:    "410",
			wantTitle:   "Link Expired",
			wantMessage: "This link has expired.",
		},
		{
			name: "401 unauthorized without bot username",
			data: &ErrorData{
				Code:        401,
				Title:       "Unauthorized",
				Message:     "You do not have permission.",
				BotUsername: "",
			},
			wantCode:    "401",
			wantTitle:   "Unauthorized",
			wantMessage: "You do not have permission.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := renderer.RenderError(&buf, tt.data)
			if err != nil {
				t.Fatalf("RenderError() error: %v", err)
			}

			html := buf.String()

			if !strings.Contains(html, tt.wantCode) {
				t.Errorf("HTML missing error code %s", tt.wantCode)
			}

			if !strings.Contains(html, tt.wantTitle) {
				t.Errorf("HTML missing title %s", tt.wantTitle)
			}

			if !strings.Contains(html, tt.wantMessage) {
				t.Errorf("HTML missing message %s", tt.wantMessage)
			}

			// Check bot deep link when username is provided
			if tt.data.BotUsername != "" {
				if !strings.Contains(html, "tg://resolve?domain="+tt.data.BotUsername) {
					t.Error("HTML missing Telegram deep link")
				}
			} else {
				// Should fall back to generic Telegram link
				if !strings.Contains(html, "https://t.me/") {
					t.Error("HTML missing fallback Telegram link")
				}
			}
		})
	}
}

func TestBuildChatGPTPrompt(t *testing.T) {
	item := &db.ItemDebugDetail{
		Topic:           "Test Topic",
		Summary:         "Test Summary",
		Text:            "Original message text",
		PreviewText:     "Preview content",
		ChannelUsername: "testchannel",
		ChannelTitle:    "Test Channel",
		MessageID:       12345,
	}

	evidence := []db.ItemEvidenceWithSource{
		{
			Source: db.EvidenceSource{
				URL:         "https://example.com/source1",
				Title:       "Source 1",
				Domain:      "example.com",
				Description: "Description of source 1",
			},
		},
	}

	clusterItems := []ClusterItemView{
		{
			Summary:         "Related item",
			Text:            "Full text of the duplicate message from related channel",
			ChannelUsername: "relatedchannel",
			MessageID:       99999,
		},
	}

	cfg := PromptBuilderConfig{MaxChars: 12000}

	prompt := BuildChatGPTPrompt(item, evidence, clusterItems, cfg)

	// Verify prompt includes all expected sections
	checks := []string{
		"## Topic",
		"Test Topic",
		"## Summary",
		"Test Summary",
		"## Original Source",
		"https://t.me/testchannel/12345",
		"## Original Text",
		"Original message text",
		"## Preview/Link Content",
		"Preview content",
		"## Duplicate/Related Messages (Corroboration)",
		"Full text of the duplicate message",
		"https://t.me/relatedchannel/99999",
		"## External Sources (Evidence)",
		"Source 1",
		"https://example.com/source1",
		"## Questions",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("Prompt missing expected content: %q", check)
		}
	}
}

func TestBuildChatGPTPrompt_WithOriginalLinks(t *testing.T) {
	// Simulate entities_json with a TextURL entity
	// The entities JSON contains URL entities that linkextract can parse
	entitiesJSON := []byte(`[{"_":"messageEntityTextUrl","offset":0,"length":10,"url":"https://news.example.com/article"}]`)

	item := &db.ItemDebugDetail{
		Topic:        "Test Topic",
		Summary:      "Test Summary",
		Text:         "Check this link for details",
		EntitiesJSON: entitiesJSON,
	}

	cfg := PromptBuilderConfig{MaxChars: 12000}

	prompt := BuildChatGPTPrompt(item, nil, nil, cfg)

	// Should include the Links in Message section with the URL
	linksHeader := "## Links in Message"
	if !strings.Contains(prompt, linksHeader) {
		t.Errorf("Prompt should contain %q section when URLs are present", linksHeader)
	}

	if !strings.Contains(prompt, "https://news.example.com/article") {
		t.Error("Prompt should contain the extracted URL from entities")
	}
}

func TestBuildChatGPTPrompt_NoLinksSection(t *testing.T) {
	// Item with no URLs in text or entities
	item := &db.ItemDebugDetail{
		Topic:        "Test Topic",
		Summary:      "Test Summary",
		Text:         "Just some plain text without any links",
		EntitiesJSON: nil,
		MediaJSON:    nil,
	}

	cfg := PromptBuilderConfig{MaxChars: 12000}

	prompt := BuildChatGPTPrompt(item, nil, nil, cfg)

	// Should NOT include Links in Message section when there are no URLs
	linksHeader := "Links in Message"
	if strings.Contains(prompt, linksHeader) {
		t.Errorf("Prompt should not contain %q section when no URLs are present", linksHeader)
	}
}

func TestBuildChatGPTPrompt_Truncation(t *testing.T) {
	item := &db.ItemDebugDetail{
		Topic:   "Topic",
		Summary: "Summary",
		Text:    strings.Repeat("x", 20000), // Very long text
	}

	cfg := PromptBuilderConfig{MaxChars: 1000}

	prompt := BuildChatGPTPrompt(item, nil, nil, cfg)

	if len(prompt) > 1000 {
		t.Errorf("Prompt should be truncated to %d chars, got %d", 1000, len(prompt))
	}

	if !strings.HasSuffix(prompt, testTruncationSuffix) {
		t.Errorf("Truncated prompt should end with %q", testTruncationSuffix)
	}
}

func TestBuildOriginalMsgLink(t *testing.T) {
	tests := []struct {
		name     string
		item     *db.ItemDebugDetail
		wantLink string
	}{
		{
			name: "with username",
			item: &db.ItemDebugDetail{
				ChannelUsername: "testchannel",
				MessageID:       12345,
			},
			wantLink: "https://t.me/testchannel/12345",
		},
		{
			name: "without username (private channel)",
			item: &db.ItemDebugDetail{
				ChannelUsername: "",
				ChannelPeerID:   1234567890,
				MessageID:       12345,
			},
			wantLink: "https://t.me/c/1234567890/12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildOriginalMsgLink(tt.item)
			if got != tt.wantLink {
				t.Errorf("BuildOriginalMsgLink() = %v, want %v", got, tt.wantLink)
			}
		})
	}
}

func TestBuildChatGPTPrompt_NoTruncation(t *testing.T) {
	// Very long text that would normally be truncated
	longText := strings.Repeat("x", 20000)
	item := &db.ItemDebugDetail{
		Topic:   "Topic",
		Summary: "Summary",
		Text:    longText,
	}

	// MaxChars = 0 means no truncation
	cfg := PromptBuilderConfig{MaxChars: 0}

	prompt := BuildChatGPTPrompt(item, nil, nil, cfg)

	// Should contain the full long text without truncation
	if !strings.Contains(prompt, longText) {
		t.Error("Prompt with MaxChars=0 should not truncate the text")
	}

	if strings.HasSuffix(prompt, testTruncationSuffix) {
		t.Errorf("Prompt with MaxChars=0 should not end with truncation suffix %q", testTruncationSuffix)
	}
}

func TestBuildShortcutURL(t *testing.T) {
	tests := []struct {
		name         string
		shortcutName string
		prompt       string
		maxChars     int
		wantContains []string
	}{
		{
			name:         "basic shortcut URL",
			shortcutName: "Ask ChatGPT",
			prompt:       "Hello world",
			maxChars:     2000,
			wantContains: []string{
				"shortcuts://run-shortcut",
				"name=Ask%20ChatGPT",
				"input=Hello%20world",
			},
		},
		{
			name:         "URL encodes special characters",
			shortcutName: "Ask ChatGPT",
			prompt:       "What is 2+2?",
			maxChars:     2000,
			wantContains: []string{
				"shortcuts://run-shortcut",
				"input=What%20is%202%2B2%3F",
			},
		},
		{
			name:         "truncates long prompt",
			shortcutName: "Ask ChatGPT",
			prompt:       strings.Repeat("x", 3000),
			maxChars:     100,
			wantContains: []string{
				"shortcuts://run-shortcut",
				"Full%20prompt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildShortcutURL(tt.shortcutName, tt.prompt, tt.maxChars)

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("BuildShortcutURL() = %v, want to contain %v", got, want)
				}
			}

			// Verify it starts with the correct scheme
			if !strings.HasPrefix(got, "shortcuts://") {
				t.Errorf("BuildShortcutURL() should start with shortcuts://, got %v", got)
			}
		})
	}
}

func TestBuildShortcutURL_DefaultMaxChars(t *testing.T) {
	// Test that maxChars=0 uses default
	shortPrompt := "Test prompt"
	url := BuildShortcutURL("Test", shortPrompt, 0)

	if !strings.Contains(url, "Test%20prompt") {
		t.Error("BuildShortcutURL with maxChars=0 should use default and include prompt")
	}
}
