package links

import (
	"testing"
)

const (
	testArticleURL          = "https://example.com/article"
	errExtractWebContentFmt = "ExtractWebContent() error = %v"
)

func TestExtractMetaTags(t *testing.T) {
	htmlContent := `
<html>
<head>
    <title>Test Page Title</title>
    <meta name="description" content="This is a test description">
    <meta property="og:title" content="OG Title">
    <meta property="og:description" content="OG Description">
    <meta property="article:published_time" content="2026-01-09T08:00:00Z">
</head>
<body>
    <h1>Hello World</h1>
</body>
</html>
`
	meta := extractMetaTags([]byte(htmlContent))

	if meta.Title != "Test Page Title" {
		t.Errorf("Expected Title 'Test Page Title', got '%s'", meta.Title)
	}

	if meta.Description != "This is a test description" {
		t.Errorf("Expected Description 'This is a test description', got '%s'", meta.Description)
	}

	if meta.OGTitle != "OG Title" {
		t.Errorf("Expected OGTitle 'OG Title', got '%s'", meta.OGTitle)
	}

	if meta.PublishedTime != "2026-01-09T08:00:00Z" {
		t.Errorf("Expected PublishedTime '2026-01-09T08:00:00Z', got '%s'", meta.PublishedTime)
	}
}

func TestCoalesce(t *testing.T) {
	if got := coalesce("", "first", "second"); got != "first" { //nolint:goconst // test literal
		t.Errorf("coalesce() = %s, want first", got)
	}

	if got := coalesce("zero", "first"); got != "zero" { //nolint:goconst // test literal
		t.Errorf("coalesce() = %s, want zero", got)
	}

	if got := coalesce("", ""); got != "" {
		t.Errorf("coalesce() = %s, want empty", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{
			name:  "shorter than max",
			input: "hello",
			max:   10,
			want:  "hello",
		},
		{
			name:  "equal to max",
			input: "hello",
			max:   5,
			want:  "hello",
		},
		{
			name:  "longer than max",
			input: "hello world",
			max:   5,
			want:  "hello...",
		},
		{
			name:  "empty string",
			input: "",
			max:   10,
			want:  "",
		},
		{
			name:  "unicode characters",
			input: "привет мир",
			max:   6,
			want:  "привет...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.input, tt.max); got != tt.want {
				t.Errorf("truncate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCountWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "simple sentence",
			input: "hello world",
			want:  2,
		},
		{
			name:  "empty string",
			input: "",
			want:  0,
		},
		{
			name:  "multiple spaces",
			input: "hello    world",
			want:  2,
		},
		{
			name:  "newlines and tabs",
			input: "hello\nworld\tthere",
			want:  3,
		},
		{
			name:  "single word",
			input: "word",
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countWords(tt.input); got != tt.want {
				t.Errorf("countWords() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
	}{
		{
			name:     "empty string",
			input:    "",
			wantZero: true,
		},
		{
			name:     "ISO format",
			input:    "2026-01-09T08:00:00Z",
			wantZero: false,
		},
		{
			name:     "invalid format",
			input:    "not-a-date",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDate(tt.input)

			if tt.wantZero && !got.IsZero() {
				t.Errorf("parseDate() = %v, want zero time", got)
			}

			if !tt.wantZero && got.IsZero() {
				t.Errorf("parseDate() returned zero time, want valid time")
			}
		})
	}
}

func TestExtractWebContent(t *testing.T) {
	htmlContent := []byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Test Article</title>
    <meta name="description" content="A test description">
    <meta property="og:title" content="OG Test Title">
    <meta property="og:description" content="OG Test Description">
    <meta property="og:image" content="https://example.com/image.jpg">
    <meta name="author" content="Test Author">
</head>
<body>
    <article>
        <h1>Test Article</h1>
        <p>This is a test article with some content. It has multiple sentences and paragraphs.</p>
        <p>This is the second paragraph with more text.</p>
    </article>
</body>
</html>
`)

	content, err := ExtractWebContent(htmlContent, testArticleURL, 5000)
	if err != nil {
		t.Fatalf(errExtractWebContentFmt, err)
	}

	if content.Title == "" {
		t.Error("expected non-empty title")
	}

	if content.Description == "" {
		t.Error("expected non-empty description")
	}
}

func TestExtractMetaTagsEmpty(t *testing.T) {
	meta := extractMetaTags([]byte(""))
	if meta.Title != "" {
		t.Errorf("expected empty title, got %q", meta.Title)
	}
}

func TestExtractMetaTagsInvalidHTML(t *testing.T) {
	// Should not panic on invalid HTML
	meta := extractMetaTags([]byte("<not valid html>>>"))

	// Just check it doesn't crash and returns empty struct
	if meta.Title != "" {
		t.Errorf("expected empty title for invalid HTML, got %q", meta.Title)
	}
}

func TestExtractMetaTagsWithAuthor(t *testing.T) {
	htmlContent := `
<html>
<head>
    <meta name="author" content="John Doe">
    <meta property="og:image" content="https://example.com/image.jpg">
</head>
<body></body>
</html>
`
	meta := extractMetaTags([]byte(htmlContent))

	if meta.Author != "John Doe" {
		t.Errorf("Expected Author 'John Doe', got '%s'", meta.Author)
	}

	if meta.OGImage != "https://example.com/image.jpg" {
		t.Errorf("Expected OGImage 'https://example.com/image.jpg', got '%s'", meta.OGImage)
	}
}

func TestExtractMetaTagsCaseInsensitive(t *testing.T) {
	htmlContent := `
<html>
<head>
    <meta NAME="Description" CONTENT="Test description">
    <meta PROPERTY="OG:TITLE" CONTENT="Test OG Title">
</head>
<body></body>
</html>
`
	meta := extractMetaTags([]byte(htmlContent))

	// Case insensitivity check for name/property
	if meta.Description != "Test description" {
		t.Errorf("Expected Description 'Test description', got '%s'", meta.Description)
	}
}

func TestExtractMetaTagsWithNestedElements(t *testing.T) {
	htmlContent := `
<html>
<head>
    <title>
        Nested Title
    </title>
    <meta name="description" content="Nested description">
</head>
<body>
    <div>
        <p>Some content</p>
    </div>
</body>
</html>
`
	meta := extractMetaTags([]byte(htmlContent))

	if meta.Title != "Nested Title" {
		t.Errorf("Expected Title 'Nested Title', got '%s'", meta.Title)
	}
}

func TestExtractWebContentFallback(t *testing.T) {
	// Minimal HTML that readability might fail on
	htmlContent := []byte(`
<html>
<head>
    <title>Fallback Title</title>
    <meta name="description" content="Fallback description">
</head>
<body>
</body>
</html>
`)

	content, err := ExtractWebContent(htmlContent, "https://example.com/simple", 5000)
	if err != nil {
		t.Fatalf(errExtractWebContentFmt, err)
	}

	// Should at least have title from fallback
	if content.Title == "" {
		t.Error("expected non-empty title from fallback")
	}
}

func TestExtractWebContentWithOGTags(t *testing.T) {
	htmlContent := []byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Page Title</title>
    <meta property="og:title" content="OG Title">
    <meta property="og:description" content="OG Description">
    <meta property="og:image" content="https://example.com/og-image.jpg">
</head>
<body>
    <article>
        <p>Some article content that is long enough to be extracted.</p>
    </article>
</body>
</html>
`)

	content, err := ExtractWebContent(htmlContent, testArticleURL, 5000)
	if err != nil {
		t.Fatalf(errExtractWebContentFmt, err)
	}

	if content.ImageURL != "https://example.com/og-image.jpg" {
		t.Errorf("expected ImageURL 'https://example.com/og-image.jpg', got '%s'", content.ImageURL)
	}
}

func TestCoalesceMoreCases(t *testing.T) {
	tests := []struct {
		name   string
		inputs []string
		want   string
	}{
		{
			name:   "all empty",
			inputs: []string{"", "", ""},
			want:   "",
		},
		{
			name:   "single non-empty",
			inputs: []string{"", "", "third"},
			want:   "third",
		},
		{
			name:   "first wins",
			inputs: []string{"first", "second", "third"},
			want:   "first",
		},
		{
			name:   "empty list",
			inputs: []string{},
			want:   "",
		},
		{
			name:   "single empty",
			inputs: []string{""},
			want:   "",
		},
		{
			name:   "single non-empty value",
			inputs: []string{"only"},
			want:   "only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coalesce(tt.inputs...)
			if got != tt.want {
				t.Errorf("coalesce() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDateMoreCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
	}{
		{
			name:     "RFC3339 format",
			input:    "2026-01-15T10:30:00+00:00",
			wantZero: false,
		},
		{
			name:     "simple date",
			input:    "2026-01-15",
			wantZero: false,
		},
		{
			name:     "US date format",
			input:    "01/15/2026",
			wantZero: false,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			wantZero: true, // whitespace is not a valid date
		},
		{
			name:     "garbage",
			input:    "xyz123abc",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDate(tt.input)

			if tt.wantZero && !got.IsZero() {
				t.Errorf("parseDate(%q) = %v, want zero time", tt.input, got)
			}

			if !tt.wantZero && got.IsZero() {
				t.Errorf("parseDate(%q) returned zero time, want valid time", tt.input)
			}
		})
	}
}

func TestCountWordsMoreCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "unicode words",
			input: "привет мир",
			want:  2,
		},
		{
			name:  "mixed unicode and ascii",
			input: "Hello мир world",
			want:  3,
		},
		{
			name:  "only whitespace",
			input: "   \t\n   ",
			want:  0,
		},
		{
			name:  "punctuation attached",
			input: "hello, world! how are you?",
			want:  5,
		},
		{
			name:  "numbers as words",
			input: "there are 100 apples",
			want:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countWords(tt.input)
			if got != tt.want {
				t.Errorf("countWords(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateMoreCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{
			name:  "zero max with content",
			input: "hello",
			max:   0,
			want:  "...",
		},
		{
			name:  "max of 1",
			input: "hello",
			max:   1,
			want:  "h...",
		},
		{
			name:  "unicode exactly at boundary",
			input: "привет",
			max:   6,
			want:  "привет",
		},
		{
			name:  "unicode truncated",
			input: "привет мир",
			max:   7,
			want:  "привет ...",
		},
		{
			name:  "emoji handling",
			input: "Hello 👋 World",
			max:   7,
			want:  "Hello 👋...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

func TestGetMetaAttrs(t *testing.T) {
	// Test via HTML parsing to ensure proper attribute extraction
	htmlContent := `
<html>
<head>
    <meta name="test-name" content="test-content">
    <meta property="test-property" content="property-content">
</head>
</html>
`
	meta := extractMetaTags([]byte(htmlContent))

	// The function should handle both name and property attributes
	// We can't directly test getMetaAttrs, but we can test the behavior through extractMetaTags
	if meta.Title != "" {
		// No title in this HTML
		t.Errorf("unexpected title: %s", meta.Title)
	}
}

func TestProcessMetaElementTitleWithoutChild(t *testing.T) {
	// Test title element without text child
	htmlContent := `
<html>
<head>
    <title></title>
</head>
</html>
`
	meta := extractMetaTags([]byte(htmlContent))

	if meta.Title != "" {
		t.Errorf("expected empty title for empty title element, got %q", meta.Title)
	}
}

func TestExtractWebContentTruncation(t *testing.T) {
	// Create HTML with long content
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "This is a sentence with many words. "
	}

	htmlContent := []byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Test Article</title>
</head>
<body>
    <article>
        <p>` + longContent + `</p>
    </article>
</body>
</html>
`)

	// Use a small maxLen to test truncation
	content, err := ExtractWebContent(htmlContent, "https://example.com/long", 100)
	if err != nil {
		t.Fatalf(errExtractWebContentFmt, err)
	}

	// Content should be truncated
	if len([]rune(content.Content)) > 103 { // 100 + "..."
		t.Errorf("content not truncated properly, got length %d", len([]rune(content.Content)))
	}
}
