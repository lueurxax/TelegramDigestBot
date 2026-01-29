package enrichment

import (
	"testing"
)

func TestNewDomainFilter(t *testing.T) {
	tests := []struct {
		name         string
		allowlist    string
		denylist     string
		expectedMode filterMode
	}{
		{
			name:         "empty lists allows all",
			allowlist:    "",
			denylist:     "",
			expectedMode: filterModeAllowAll,
		},
		{
			name:         "allowlist mode",
			allowlist:    "example.com,test.com",
			denylist:     "",
			expectedMode: filterModeAllowlist,
		},
		{
			name:         "denylist mode",
			allowlist:    "",
			denylist:     "spam.com",
			expectedMode: filterModeDenylist,
		},
		{
			name:         "allowlist takes precedence",
			allowlist:    "good.com",
			denylist:     "bad.com",
			expectedMode: filterModeAllowlist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewDomainFilter(tt.allowlist, tt.denylist)
			if f.mode != tt.expectedMode {
				t.Errorf("expected mode %v, got %v", tt.expectedMode, f.mode)
			}
		})
	}
}

func TestDomainFilter_IsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		denylist  string
		domain    string
		expected  bool
	}{
		{
			name:     "empty domain rejected",
			domain:   "",
			expected: false,
		},
		{
			name:     "allow all mode",
			domain:   "anything.com",
			expected: true,
		},
		{
			name:      "allowlist exact match",
			allowlist: "example.com",
			domain:    "example.com",
			expected:  true,
		},
		{
			name:      "allowlist subdomain match",
			allowlist: "example.com",
			domain:    "sub.example.com",
			expected:  true,
		},
		{
			name:      "allowlist no match",
			allowlist: "example.com",
			domain:    "other.com",
			expected:  false,
		},
		{
			name:     "denylist blocks domain",
			denylist: "spam.com",
			domain:   "spam.com",
			expected: false,
		},
		{
			name:     "denylist blocks subdomain",
			denylist: "spam.com",
			domain:   "sub.spam.com",
			expected: false,
		},
		{
			name:     "denylist allows other domains",
			denylist: "spam.com",
			domain:   "good.com",
			expected: true,
		},
		{
			name:      "normalize https prefix",
			allowlist: "example.com",
			domain:    "https://example.com",
			expected:  true,
		},
		{
			name:      "normalize www prefix",
			allowlist: "example.com",
			domain:    "www.example.com",
			expected:  true,
		},
		{
			name:      "case insensitive",
			allowlist: "Example.COM",
			domain:    "example.com",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewDomainFilter(tt.allowlist, tt.denylist)

			got := f.IsAllowed(tt.domain)
			if got != tt.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tt.domain, got, tt.expected)
			}
		})
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "example.com"},
		{"EXAMPLE.COM", "example.com"},
		{"  example.com  ", "example.com"},
		{"https://example.com", "example.com"},
		{"http://example.com", "example.com"},
		{"www.example.com", "example.com"},
		{"https://www.example.com/", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeDomain(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeDomain(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseDomainList(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"example.com", 1},
		{"a.com,b.com,c.com", 3},
		{"a.com, b.com , c.com", 3},
		{",,,", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDomainList(tt.input)
			if len(got) != tt.expected {
				t.Errorf("parseDomainList(%q) returned %d domains, expected %d", tt.input, len(got), tt.expected)
			}
		})
	}
}

func TestDomainFilter_SocialMediaFiltering(t *testing.T) {
	// Social media domains that should be blocked by default
	socialMediaDomainsList := []string{
		// Twitter/X
		"twitter.com",
		"x.com",
		"t.co",
		"mobile.twitter.com",
		// Facebook/Meta
		"facebook.com",
		"fb.com",
		"instagram.com",
		"threads.net",
		"m.facebook.com",
		// Video platforms
		"youtube.com",
		"youtu.be",
		"tiktok.com",
		"www.youtube.com",
		// Professional/Business
		"linkedin.com",
		// Messaging
		"telegram.org",
		"t.me",
		"discord.com",
		"discord.gg",
		// Other social
		"reddit.com",
		"old.reddit.com",
		"pinterest.com",
		// URL shorteners
		"bit.ly",
		"goo.gl",
		"tinyurl.com",
	}

	// Test with social media filtering enabled (default)
	t.Run("social media blocked by default", func(t *testing.T) {
		f := NewDomainFilter("", "")

		for _, domain := range socialMediaDomainsList {
			if f.IsAllowed(domain) {
				t.Errorf("expected social media domain %q to be blocked", domain)
			}
		}
	})

	// Test with social media filtering disabled
	t.Run("social media allowed when disabled", func(t *testing.T) {
		f := NewDomainFilterWithOptions("", "", false)

		for _, domain := range socialMediaDomainsList {
			if !f.IsAllowed(domain) {
				t.Errorf("expected social media domain %q to be allowed when filtering disabled", domain)
			}
		}
	})

	// Test that non-social media domains are not affected
	t.Run("news domains still allowed", func(t *testing.T) {
		f := NewDomainFilter("", "")

		newsDomains := []string{
			"reuters.com",
			"bbc.com",
			"cnn.com",
			"nytimes.com",
			"washingtonpost.com",
			"theguardian.com",
		}

		for _, domain := range newsDomains {
			if !f.IsAllowed(domain) {
				t.Errorf("expected news domain %q to be allowed", domain)
			}
		}
	})

	// Test that allowlist can override social media blocking
	t.Run("allowlist overrides social media blocking", func(t *testing.T) {
		const testAllowedDomain = "allowed-news.com"

		// When using allowlist mode, only allowlist domains matter
		f := NewDomainFilterWithOptions(testAllowedDomain, "", true)

		// Allowlisted domain should be allowed
		if !f.IsAllowed(testAllowedDomain) {
			t.Error("expected allowlisted domain to be allowed")
		}

		// Social media should still be blocked (checked before allowlist mode)
		if f.IsAllowed("twitter.com") {
			t.Error("expected social media to be blocked even with allowlist")
		}

		// Non-allowlisted, non-social media should be blocked in allowlist mode
		if f.IsAllowed("cnn.com") {
			t.Error("expected non-allowlisted domain to be blocked")
		}
	})
}
