package links

import (
	"testing"
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

	content, err := ExtractWebContent(htmlContent, "https://example.com/article", 5000)
	if err != nil {
		t.Fatalf("ExtractWebContent() error = %v", err)
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
