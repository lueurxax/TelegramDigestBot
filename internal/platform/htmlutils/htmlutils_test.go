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
		{
			name:     "upper-case tags are normalized",
			input:    `<B>Bold</B>`,
			expected: `<b>Bold</b>`,
		},
		{
			name:     "strips style attribute from b tag",
			input:    `<b style="color:red">Bold</b>`,
			expected: `<b>Bold</b>`,
		},
		{
			name:     "strips class attribute from i tag",
			input:    `<i class="highlight">Italic</i>`,
			expected: `<i>Italic</i>`,
		},
		{
			name:     "strips all attributes from blockquote",
			input:    `<blockquote expandable>Quote</blockquote>`,
			expected: `<blockquote>Quote</blockquote>`,
		},
		{
			name:     "strips extra anchor attributes",
			input:    `<a href="https://example.com" target="_blank" onclick="alert(1)">Link</a>`,
			expected: `<a href="https://example.com">Link</a>`,
		},
		{
			name:     "anchor without href",
			input:    `<a onclick="alert(1)">Link</a>`,
			expected: "<a>Link</a>",
		},
		{
			name:     "single quoted href",
			input:    "<a href='https://example.com/path?x=1&y=2'>Link</a>",
			expected: `<a href="https://example.com/path?x=1&amp;y=2">Link</a>`,
		},
		{
			name:     "strips javascript href with whitespace",
			input:    `<a href=" JavaScript:alert(1)">Click</a>`,
			expected: "<a>Click</a>",
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

//nolint:gocyclo // test function with multiple test cases
func TestSplitHTML(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		limit         int
		wantParts     int
		checkFirst    string // substring that should be in first part
		checkSecond   string // substring that should be in second part
		checkNotSplit string // substring that should NOT be split across parts
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
			name:       "split at word boundary",
			text:       "This is a long sentence that needs to be split somewhere reasonable.",
			limit:      30,
			wantParts:  3,
			checkFirst: "This is a long",
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
			name:          "UTF-8 emoji not split mid-character",
			text:          "Hello 🔴🔵🟢🟡 world test message with emojis 🎉🎊🎁",
			limit:         30, // UTF-16 units: emojis are 2 units each
			wantParts:     2,
			checkNotSplit: "🔴🔵🟢🟡", // These 4 emojis should stay together (8 UTF-16 units)
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
		{
			name:        "does not duplicate item boundary newline",
			text:        ItemStart + "Item one content that is long enough to be split" + ItemEnd + "\n" + ItemStart + "Item two content continues here" + ItemEnd,
			limit:       65,
			wantParts:   2,
			checkFirst:  "Item one content",
			checkSecond: "Item two content",
		},
		{
			name:        "blockquote not reopened after split",
			text:        "<blockquote>Long blockquote content that spans multiple parts and needs to be split</blockquote>\nMore content after the blockquote here",
			limit:       50,
			wantParts:   3, // Split happens naturally due to length
			checkFirst:  "blockquote",
			checkSecond: "and needs", // Second part should NOT have unbalanced </blockquote>
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

func TestSplitHTMLReopensAnchor(t *testing.T) {
	text := `<a href="https://example.com">` + strings.Repeat("word ", 30) + `</a>`
	parts := SplitHTML(text, 20)

	if len(parts) < 2 {
		t.Fatalf("SplitHTML() got %d parts, want at least 2. Parts: %v", len(parts), parts)
	}

	for i, part := range parts {
		if !strings.Contains(part, `<a href="https://example.com">`) {
			t.Errorf("Part %d missing reopened anchor tag: %q", i, part)
		}
	}
}

func TestSplitHTMLItemBoundaryFlush(t *testing.T) {
	itemOne := ItemStart + strings.Repeat("A", 40) + ItemEnd
	itemTwo := ItemStart + strings.Repeat("B", 40) + ItemEnd
	text := itemOne + "\n" + itemTwo

	parts := SplitHTML(text, 70)
	if len(parts) != 2 {
		t.Fatalf("SplitHTML() got %d parts, want 2. Parts: %v", len(parts), parts)
	}

	if strings.Contains(parts[0], "BBBB") { //nolint:goconst // test literal
		t.Errorf("First part should not include second item: %q", parts[0])
	}

	if !strings.Contains(parts[1], "BBBB") {
		t.Errorf("Second part missing second item: %q", parts[1])
	}

	if strings.HasPrefix(parts[1], "\n") {
		t.Errorf("Second part should not start with a newline: %q", parts[1])
	}
}

func TestStripHTMLTags(t *testing.T) {
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
			name:     "simple bold tag",
			input:    "<b>Bold text</b>",
			expected: "Bold text",
		},
		{
			name:     "multiple tags",
			input:    "<b>Bold</b> and <i>italic</i> text",
			expected: "Bold and italic text",
		},
		{
			name:     "nested tags",
			input:    "<b><i>Bold italic</i></b>",
			expected: "Bold italic",
		},
		{
			name:     "anchor with href",
			input:    `<a href="https://example.com">Link text</a>`,
			expected: "Link text",
		},
		{
			name:     "escaped HTML entities",
			input:    "Apple &amp; Google &gt; Microsoft",
			expected: "Apple & Google > Microsoft",
		},
		{
			name:     "mixed content",
			input:    "<b>Breaking:</b> Trump announces <i>new tariffs</i> on imports",
			expected: "Breaking: Trump announces new tariffs on imports",
		},
		{
			name:     "blockquote",
			input:    "<blockquote>Quoted content here</blockquote>",
			expected: "Quoted content here",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only tags no content",
			input:    "<b></b><i></i>",
			expected: "",
		},
		{
			name:     "whitespace preservation",
			input:    "<b>Word1</b>  <i>Word2</i>",
			expected: "Word1  Word2",
		},
		{
			name:     "newlines preserved",
			input:    "<b>Line1</b>\n<i>Line2</i>",
			expected: "Line1\nLine2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripHTMLTags(tt.input)
			if got != tt.expected {
				t.Errorf("StripHTMLTags() = %q, want %q", got, tt.expected)
			}
		})
	}
}
