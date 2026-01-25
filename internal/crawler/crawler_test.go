package crawler

import "testing"

func TestHasSkipSuffix(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Should skip
		{name: "css file", url: "https://example.com/style.css", want: true},
		{name: "css with query", url: "https://example.com/style.css?v=123", want: true},
		{name: "js with timestamp", url: "https://example.com/app.js?241230-134616", want: true},
		{name: "image png", url: "https://example.com/logo.png", want: true},
		{name: "image with query", url: "https://example.com/logo.png?size=large", want: true},
		{name: "pdf file", url: "https://example.com/doc.pdf", want: true},
		{name: "json api", url: "https://example.com/api/data.json", want: true},
		{name: "font woff2", url: "https://example.com/font.woff2?v=1", want: true},
		{name: "css with fragment", url: "https://example.com/style.css#section", want: true},
		{name: "css query and fragment", url: "https://example.com/style.css?v=1#top", want: true},

		// Should not skip
		{name: "html page", url: "https://example.com/article.html", want: false},
		{name: "no extension", url: "https://example.com/article", want: false},
		{name: "html with query", url: "https://example.com/page.html?id=123", want: false},
		{name: "path ending in css word", url: "https://example.com/css", want: false},
		{name: "query param with css value", url: "https://example.com/page?style=custom.css", want: false},
		{name: "news article", url: "https://reuters.com/world/article-title/", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasSkipSuffix(tt.url); got != tt.want {
				t.Errorf("hasSkipSuffix(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsValidCrawlURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Valid
		{name: "https news", url: "https://reuters.com/world/news", want: true},
		{name: "http page", url: "http://example.com/article", want: true},
		{name: "html with query", url: "https://example.com/page.html?id=123", want: true},

		// Invalid - wrong protocol
		{name: "ftp", url: "ftp://example.com/file", want: false},
		{name: "mailto", url: "mailto:test@example.com", want: false},
		{name: "javascript", url: "javascript:void(0)", want: false},

		// Invalid - skip patterns
		{name: "twitter share", url: "https://twitter.com/share?url=test", want: false},
		{name: "login page", url: "https://example.com/login", want: false},
		{name: "utm tracking", url: "https://example.com/page?utm_source=test", want: false},

		// Invalid - file extensions
		{name: "css file", url: "https://example.com/style.css?v=123", want: false},
		{name: "js file", url: "https://example.com/app.js", want: false},
		{name: "image", url: "https://example.com/photo.jpg", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidCrawlURL(tt.url); got != tt.want {
				t.Errorf("isValidCrawlURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
