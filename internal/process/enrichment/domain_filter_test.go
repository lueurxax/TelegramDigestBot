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
