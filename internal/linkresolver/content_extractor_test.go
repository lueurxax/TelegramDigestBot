package linkresolver

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
