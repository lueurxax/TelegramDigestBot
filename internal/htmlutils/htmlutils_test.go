package htmlutils

import (
	"strings"
	"testing"
)

func TestStripItemMarkers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no markers",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "strip start marker",
			input:    ItemStart + "Content",
			expected: "Content",
		},
		{
			name:     "strip end marker",
			input:    "Content" + ItemEnd,
			expected: "Content",
		},
		{
			name:     "strip both markers",
			input:    ItemStart + "Content" + ItemEnd,
			expected: "Content",
		},
		{
			name:     "multiple items",
			input:    ItemStart + "Item 1" + ItemEnd + "\n" + ItemStart + "Item 2" + ItemEnd,
			expected: "Item 1\nItem 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripItemMarkers(tt.input)
			if got != tt.expected {
				t.Errorf("StripItemMarkers() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tags",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "valid tags",
			input:    "<b>Bold</b> <i>Italic</i>",
			expected: "<b>Bold</b> <i>Italic</i>",
		},
		{
			name:     "unsupported tags",
			input:    "<h1>Title</h1><p>Para</p>",
			expected: "TitlePara",
		},
		{
			name:     "mixed tags",
			input:    "<b>Bold</b> and <script>alert(1)</script>",
			expected: "<b>Bold</b> and alert(1)",
		},
		{
			name:     "escapes special characters",
			input:    "Apple & Google > Microsoft < Amazon",
			expected: "Apple &amp; Google &gt; Microsoft &lt; Amazon",
		},
		{
			name:     "keeps attributes on a tag",
			input:    "<a href=\"https://example.com\">Link</a>",
			expected: "<a href=\"https://example.com\">Link</a>",
		},
		{
			name:     "nested tags",
			input:    "<b><i>Bold Italic</i></b>",
			expected: "<b><i>Bold Italic</i></b>",
		},
		{
			name:     "unclosed tags",
			input:    "<b>Bold",
			expected: "<b>Bold",
		},
		{
			name:     "malformed tags",
			input:    "<b malformed",
			expected: "&lt;b malformed",
		},
		{
			name:     "strips javascript href",
			input:    `<a href="javascript:alert(1)">Click</a>`,
			expected: "<a>Click</a>",
		},
		{
			name:     "strips vbscript href",
			input:    `<a href="vbscript:msgbox(1)">Click</a>`,
			expected: "<a>Click</a>",
		},
		{
			name:     "strips data href",
			input:    `<a href="data:text/html,test">Click</a>`,
			expected: "<a>Click</a>",
		},
		{
			name:     "allows http href",
			input:    `<a href="http://example.com">Link</a>`,
			expected: `<a href="http://example.com">Link</a>`,
		},
		{
			name:     "allows https href",
			input:    `<a href="https://t.me/channel">Link</a>`,
			expected: `<a href="https://t.me/channel">Link</a>`,
		},
		{
			name:     "escapes special chars in href",
			input:    `<a href="https://example.com?q=a&b=c">Link</a>`,
			expected: `<a href="https://example.com?q=a&amp;b=c">Link</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHTML(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeHTML() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSplitHTML(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		limit          int
		wantParts      int
		checkFirst     string // substring that should be in first part
		checkSecond    string // substring that should be in second part
		checkNotSplit  string // substring that should NOT be split across parts
	}{
		{
			name:       "no split needed",
			text:       "short text",
			limit:      100,
			wantParts:  1,
			checkFirst: "short text",
		},
		{
			name:          "split at section separator",
			text:          "Part 1 content with more text here\n━━━━━━━━━━━━━━━━━━━━━━\nPart 2 content with more text here",
			limit:         60, // Now counts runes, not bytes. Total ~95 runes, split around separator
			wantParts:     2,
			checkFirst:    "Part 1 content",
			checkSecond:   "Part 2 content",
			checkNotSplit: "━━━━━━━━━━━━━━━━━━━━━━",
		},
		{
			name:        "split at paragraph break",
			text:        "First paragraph with some text.\n\nSecond paragraph with more text.",
			limit:       40,
			wantParts:   2,
			checkFirst:  "First paragraph",
			checkSecond: "Second paragraph",
		},
		{
			name:        "split at word boundary",
			text:        "This is a long sentence that needs to be split somewhere reasonable.",
			limit:       30,
			wantParts:   3,
			checkFirst:  "This is a long",
		},
		{
			name:        "split at newline",
			text:        "Line 1\nLine 2\nLine 3\nLine 4",
			limit:       15,
			wantParts:   2,
			checkFirst:  "Line 1",
			checkSecond: "Line",
		},
		{
			name:          "preserves HTML tags across split",
			text:          "<b>Bold text that is quite long</b> and <i>italic text that is also long</i>",
			limit:         35,
			wantParts:     2,
			checkNotSplit: "Bold text",
		},
		{
			name:        "split at paragraph break priority",
			text:        "First section content with enough text here for testing\n\n🔴 Breaking news\nMore breaking content here with more text for testing",
			limit:       58, // Forces split after \n\n and then after \n
			wantParts:   3,  // Splits at \n\n and then at \n
			checkFirst:  "First section",
			checkSecond: "Breaking",
		},
		{
			name:        "UTF-8 Cyrillic text does not split mid-rune",
			text:        "Привет мир это тестовое сообщение на русском языке для проверки",
			limit:       30,
			wantParts:   3,
			checkFirst:  "Привет",
			checkSecond: "сообщение",
		},
		{
			name:          "UTF-8 emoji not split",
			text:          "Hello 🔴🔵🟢🟡 world test message with emojis 🎉🎊🎁",
			limit:         25,
			wantParts:     2,
			checkNotSplit: "🔴🔵🟢🟡",
		},
		{
			name:        "split at item boundary marker",
			text:        ItemStart + "First item with some content here" + ItemEnd + "\n" + ItemStart + "Second item with more content" + ItemEnd + "\n",
			limit:       60,
			wantParts:   2,
			checkFirst:  "First item",
			checkSecond: "Second item",
		},
		{
			name:          "item boundary takes priority over other splits",
			text:          ItemStart + "Item one has some longer content here\n\nWith more paragraph text" + ItemEnd + "\n" + ItemStart + "Item two with additional content" + ItemEnd + "\n",
			limit:         70,
			wantParts:     2,
			checkFirst:    "Item one",
			checkSecond:   "Item two",
			checkNotSplit: "With more paragraph", // Should not split at \n\n within item
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := SplitHTML(tt.text, tt.limit)

			if len(parts) != tt.wantParts {
				t.Errorf("SplitHTML() got %d parts, want %d. Parts:\n", len(parts), tt.wantParts)
				for i, p := range parts {
					t.Errorf("  Part %d (%d chars): %q\n", i, len(p), p)
				}
			}

			if tt.checkFirst != "" && len(parts) > 0 {
				if !strings.Contains(parts[0], tt.checkFirst) {
					t.Errorf("First part should contain %q, got: %q", tt.checkFirst, parts[0])
				}
			}

			if tt.checkSecond != "" && len(parts) > 1 {
				if !strings.Contains(parts[1], tt.checkSecond) {
					t.Errorf("Second part should contain %q, got: %q", tt.checkSecond, parts[1])
				}
			}

			if tt.checkNotSplit != "" {
				// Check that the string is not split across parts
				for _, p := range parts {
					if strings.Contains(p, tt.checkNotSplit[:len(tt.checkNotSplit)/2]) &&
						!strings.Contains(p, tt.checkNotSplit) {
						t.Errorf("String %q appears to be split across parts", tt.checkNotSplit)
					}
				}
			}

			// Verify all parts have balanced HTML tags
			for i, p := range parts {
				// Count opening and closing tags
				openCount := strings.Count(p, "<b>") + strings.Count(p, "<i>") +
					strings.Count(p, "<blockquote>") + strings.Count(p, "<a ")
				closeCount := strings.Count(p, "</b>") + strings.Count(p, "</i>") +
					strings.Count(p, "</blockquote>") + strings.Count(p, "</a>")
				if openCount != closeCount {
					t.Errorf("Part %d has unbalanced tags (open: %d, close: %d): %q",
						i, openCount, closeCount, p)
				}
			}
		})
	}
}
