package filters

import "testing"

func TestIsEmojiOnly(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "empty string",
			text:     "",
			expected: true,
		},
		{
			name:     "whitespace only",
			text:     "   \t\n  ",
			expected: true,
		},
		{
			name:     "emoji only",
			text:     "👍🔥❤️",
			expected: true,
		},
		{
			name:     "emoji with spaces",
			text:     "  👍  🔥  ",
			expected: true,
		},
		{
			name:     "punctuation only",
			text:     "!!!...",
			expected: true,
		},
		{
			name:     "text with letters",
			text:     "Hello",
			expected: false,
		},
		{
			name:     "text with numbers",
			text:     "123",
			expected: false,
		},
		{
			name:     "mixed emoji and text",
			text:     "👍 Great job!",
			expected: false,
		},
		{
			name:     "cyrillic text",
			text:     "Привет",
			expected: false,
		},
		{
			name:     "symbols only",
			text:     "→←↑↓",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEmojiOnly(tt.text); got != tt.expected {
				t.Errorf("IsEmojiOnly(%q) = %v, want %v", tt.text, got, tt.expected)
			}
		})
	}
}

func TestStripFooterBoilerplate(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantText    string
		wantChanged bool
	}{
		{
			name:        "no footer",
			text:        "This is regular content.\nMore content here.",
			wantText:    "This is regular content.\nMore content here.",
			wantChanged: false,
		},
		{
			name:        "single line",
			text:        "Single line content",
			wantText:    "Single line content",
			wantChanged: false,
		},
		{
			name:        "footer with subscribe only - needs 2 keywords",
			text:        "Main content here.\nMore content.\n\nSubscribe to our channel!",
			wantText:    "Main content here.\nMore content.\n\nSubscribe to our channel!",
			wantChanged: false,
		},
		{
			name:        "footer with share and url",
			text:        "Main content.\nMore stuff.\n\nShare this\nhttps://t.me/channel",
			wantText:    "Main content.\nMore stuff.",
			wantChanged: true,
		},
		{
			name:        "russian boilerplate - needs 2 keywords",
			text:        "Новости дня.\nПодробности события.\n\nподпишись на канал!",
			wantText:    "Новости дня.\nПодробности события.\n\nподпишись на канал!",
			wantChanged: false,
		},
		{
			name:        "donate footer",
			text:        "Article content.\nSecond paragraph.\n\nDonate to support us\nhttps://donate.example.com",
			wantText:    "Article content.\nSecond paragraph.",
			wantChanged: true,
		},
		{
			name:        "no blank line before footer - needs 2 keywords",
			text:        "Content line one.\nContent line two.\nFollow us on Twitter",
			wantText:    "Content line one.\nContent line two.\nFollow us on Twitter",
			wantChanged: false,
		},
		{
			name:        "footer without boilerplate keywords",
			text:        "Main content.\nMore content.\n\nJust a normal ending.",
			wantText:    "Main content.\nMore content.\n\nJust a normal ending.",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotChanged := StripFooterBoilerplate(tt.text)
			if gotText != tt.wantText {
				t.Errorf("StripFooterBoilerplate() text = %q, want %q", gotText, tt.wantText)
			}

			if gotChanged != tt.wantChanged {
				t.Errorf("StripFooterBoilerplate() changed = %v, want %v", gotChanged, tt.wantChanged)
			}
		})
	}
}

func TestIsBoilerplateOnly(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
		reason   string
	}{
		{
			name:     "empty string",
			text:     "",
			expected: false,
			reason:   "empty input has no boilerplate keywords",
		},
		{
			name:     "single subscribe line",
			text:     "Subscribe to our channel",
			expected: true,
			reason:   "single line with subscribe keyword is boilerplate",
		},
		{
			name:     "multiple boilerplate lines",
			text:     "Subscribe now\nFollow us\nShare this post",
			expected: true,
			reason:   "all lines contain boilerplate keywords",
		},
		{
			name:     "russian boilerplate",
			text:     "подписаться\nподдержи нас",
			expected: true,
			reason:   "russian subscribe and support keywords detected",
		},
		{
			name:     "url only - not boilerplate",
			text:     "https://example.com",
			expected: false,
			reason:   "URLs alone are not boilerplate without keywords",
		},
		{
			name:     "mixed content",
			text:     "Important news!\nSubscribe to channel",
			expected: false,
			reason:   "contains non-boilerplate content line",
		},
		{
			name:     "normal content",
			text:     "This is actual news content that should not be filtered.",
			expected: false,
			reason:   "regular content without boilerplate markers",
		},
		{
			name:     "whitespace only",
			text:     "   \n\n   ",
			expected: false,
			reason:   "whitespace has no boilerplate keywords",
		},
		{
			name:     "donate line",
			text:     "Donate to support our work",
			expected: true,
			reason:   "donate keyword indicates boilerplate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBoilerplateOnly(tt.text)
			if got != tt.expected {
				t.Errorf("IsBoilerplateOnly(%q) = %v, want %v; reason: %s", tt.text, got, tt.expected, tt.reason)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "unix newlines",
			text:     "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "windows newlines",
			text:     "line1\r\nline2\r\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "single line",
			text:     "single line",
			expected: []string{"single line"},
		},
		{
			name:     "empty string",
			text:     "",
			expected: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.text)
			if len(got) != len(tt.expected) {
				t.Errorf("splitLines() len = %d, want %d", len(got), len(tt.expected))
				return
			}

			for i, line := range got {
				if line != tt.expected[i] {
					t.Errorf("splitLines()[%d] = %q, want %q", i, line, tt.expected[i])
				}
			}
		})
	}
}

func TestFindFooterStart(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected int
	}{
		{
			name:     "blank line before footer",
			lines:    []string{"content", "more", "", "footer"},
			expected: 3,
		},
		{
			name:     "no blank line - use last 2",
			lines:    []string{"line1", "line2", "line3", "line4"},
			expected: 2,
		},
		{
			name:     "too few lines",
			lines:    []string{"line1", "line2"},
			expected: -1,
		},
		{
			name:     "single line",
			lines:    []string{"single"},
			expected: -1,
		},
		{
			name:     "blank line at end returns next line index",
			lines:    []string{"content", "more", ""},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findFooterStart(tt.lines); got != tt.expected {
				t.Errorf("findFooterStart() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestIsBoilerplateBlock(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected bool
	}{
		{
			name:     "empty lines",
			lines:    []string{},
			expected: false,
		},
		{
			name:     "only whitespace",
			lines:    []string{"", "  ", ""},
			expected: false,
		},
		{
			name:     "two boilerplate keywords",
			lines:    []string{"Subscribe now", "Follow us"},
			expected: true,
		},
		{
			name:     "one keyword and url",
			lines:    []string{"Share this", "https://t.me/channel"},
			expected: true,
		},
		{
			name:     "only one keyword",
			lines:    []string{"Subscribe", "Normal text"},
			expected: false,
		},
		{
			name:     "normal content",
			lines:    []string{"Breaking news", "More details here"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBoilerplateBlock(tt.lines); got != tt.expected {
				t.Errorf("isBoilerplateBlock() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsBoilerplateLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "subscribe",
			line:     "Subscribe to our channel",
			expected: true,
		},
		{
			name:     "share",
			line:     "Share this post",
			expected: true,
		},
		{
			name:     "follow",
			line:     "Follow us on social media",
			expected: true,
		},
		{
			name:     "donate",
			line:     "Donate to support",
			expected: true,
		},
		{
			name:     "russian subscribe - подпис prefix",
			line:     "подписаться на канал",
			expected: true,
		},
		{
			name:     "russian support",
			line:     "Поддержи нас",
			expected: true,
		},
		{
			name:     "normal text",
			line:     "Breaking news today",
			expected: false,
		},
		{
			name:     "case insensitive",
			line:     "SUBSCRIBE NOW!",
			expected: true,
		},
		{
			name:     "with leading whitespace",
			line:     "   Subscribe",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBoilerplateLine(tt.line); got != tt.expected {
				t.Errorf("isBoilerplateLine(%q) = %v, want %v", tt.line, got, tt.expected)
			}
		})
	}
}

func TestLooksLikeURL(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "http url",
			line:     "http://example.com",
			expected: true,
		},
		{
			name:     "https url",
			line:     "https://example.com",
			expected: true,
		},
		{
			name:     "telegram link",
			line:     "t.me/channel",
			expected: true,
		},
		{
			name:     "uppercase HTTP",
			line:     "HTTP://EXAMPLE.COM",
			expected: true,
		},
		{
			name:     "not a url",
			line:     "This is regular text",
			expected: false,
		},
		{
			name:     "url in middle of text",
			line:     "Check out https://example.com for more",
			expected: false,
		},
		{
			name:     "empty string",
			line:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeURL(tt.line); got != tt.expected {
				t.Errorf("looksLikeURL(%q) = %v, want %v", tt.line, got, tt.expected)
			}
		})
	}
}
