package enrichment

import "testing"

func TestURLFilter_IsNavigationURL(t *testing.T) {
	filter := NewURLFilter(true)

	tests := []struct {
		name       string
		url        string
		wantReason string
	}{
		// Should be filtered (navigation pages)
		{name: "category page", url: "https://example.com/category/news/", wantReason: SkipReasonNavigationPage},
		{name: "tag page", url: "https://example.com/tag/politics/", wantReason: SkipReasonNavigationPage},
		{name: "tags page", url: "https://example.com/tags/world/", wantReason: SkipReasonNavigationPage},
		{name: "archive page", url: "https://example.com/archive/2024/", wantReason: SkipReasonNavigationPage},
		{name: "author page", url: "https://example.com/author/john-doe/", wantReason: SkipReasonNavigationPage},
		{name: "pagination", url: "https://example.com/news/page/5", wantReason: SkipReasonNavigationPage},
		{name: "search page", url: "https://example.com/search?q=test", wantReason: SkipReasonNavigationPage},
		{name: "feed page", url: "https://example.com/feed/", wantReason: SkipReasonNavigationPage},
		{name: "rss page", url: "https://example.com/rss/news", wantReason: SkipReasonNavigationPage},
		{name: "sitemap", url: "https://example.com/sitemap.xml", wantReason: SkipReasonNavigationPage},
		{name: "topic page", url: "https://example.com/topics/ai/", wantReason: SkipReasonNavigationPage},
		{name: "user profile", url: "https://example.com/users/johndoe/", wantReason: SkipReasonNavigationPage},
		{name: "gallery", url: "https://example.com/gallery/", wantReason: SkipReasonNavigationPage},
		{name: "video listing", url: "https://example.com/videos/", wantReason: SkipReasonNavigationPage},
		{name: "contact page", url: "https://example.com/contact/", wantReason: SkipReasonNavigationPage},
		{name: "about page", url: "https://example.com/about/", wantReason: SkipReasonNavigationPage},
		{name: "latest page", url: "https://example.com/latest/", wantReason: SkipReasonNavigationPage},
		{name: "section page", url: "https://example.com/section/world/", wantReason: SkipReasonNavigationPage},
		{name: "page query param", url: "https://example.com/news/?page=2", wantReason: SkipReasonNavigationPage},
		{name: "category query param", url: "https://example.com/news/?category=tech", wantReason: SkipReasonNavigationPage},
		{name: "homepage", url: "https://example.com/", wantReason: SkipReasonNavigationPage},
		{name: "index html", url: "https://example.com/index.html", wantReason: SkipReasonNavigationPage},
		{name: "russian topic", url: "https://example.ru/tema/politika/", wantReason: SkipReasonNavigationPage},

		// Should NOT be filtered (actual articles)
		{name: "article with slug", url: "https://example.com/news/2024/trump-wins-election", wantReason: ""},
		{name: "article with id", url: "https://example.com/articles/12345", wantReason: ""},
		{name: "article html", url: "https://example.com/news/article.html", wantReason: ""},
		{name: "article php", url: "https://example.com/news/story.php", wantReason: ""},
		{name: "deep article path", url: "https://example.com/2024/01/15/breaking-news-story", wantReason: ""},
		{name: "news article", url: "https://reuters.com/world/us/trump-announces-tariffs-2024/", wantReason: ""},
		{name: "bbc article", url: "https://www.bbc.com/news/world-us-canada-12345678", wantReason: ""},
		{name: "cnn article", url: "https://edition.cnn.com/2024/01/15/politics/trump-news/index.html", wantReason: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filter.IsNavigationURL(tt.url)
			if got != tt.wantReason {
				t.Errorf("IsNavigationURL(%q) = %q, want %q", tt.url, got, tt.wantReason)
			}
		})
	}
}

func TestURLFilter_Disabled(t *testing.T) {
	filter := NewURLFilter(false)

	// When disabled, all URLs should pass
	urls := []string{
		"https://example.com/category/news/",
		"https://example.com/",
		"https://example.com/tag/politics/",
	}

	for _, u := range urls {
		got := filter.IsNavigationURL(u)
		if got != "" {
			t.Errorf("IsNavigationURL(%q) with filter disabled = %q, want empty", u, got)
		}
	}
}

func TestCountPathSegments(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: "", want: 0},
		{path: "/", want: 0},
		{path: "/news", want: 1},
		{path: "/news/", want: 1},
		{path: "/news/article", want: 2},
		{path: "/en/news", want: 1},    // "en" is language prefix, doesn't count
		{path: "/en-US/news", want: 1}, // "en-US" is language prefix
		{path: "/ru/novosti/statya", want: 2},
		{path: "/2024/01/15/article", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := countPathSegments(tt.path)
			if got != tt.want {
				t.Errorf("countPathSegments(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestHasFileExtension(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/article.html", want: true},
		{path: "/article.htm", want: true},
		{path: "/article.php", want: true},
		{path: "/page.aspx", want: true},
		{path: "/article", want: false},
		{path: "/article/", want: false},
		{path: "/category/news", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := hasFileExtension(tt.path)
			if got != tt.want {
				t.Errorf("hasFileExtension(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
