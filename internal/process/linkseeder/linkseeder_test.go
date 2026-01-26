package linkseeder

import (
	"testing"
)

const filterURLErrFormat = "filterURL(%q) = %q, want %q"

func TestFilterURL(t *testing.T) {
	seeder := &Seeder{
		extensionDenylist: map[string]struct{}{
			".pdf": {},
			".exe": {},
			".zip": {},
		},
		domainAllowlist: nil,
		domainDenylist: map[string]struct{}{
			"badsite.com": {},
		},
	}

	tests := []struct {
		name       string
		url        string
		wantReason string
	}{
		{
			name:       "valid http url",
			url:        "http://example.com/page",
			wantReason: "",
		},
		{
			name:       "valid https url",
			url:        "https://example.com/page",
			wantReason: "",
		},
		{
			name:       "invalid scheme ftp",
			url:        "ftp://example.com/file",
			wantReason: SkipReasonInvalidScheme,
		},
		{
			name:       "invalid scheme mailto",
			url:        "mailto:test@example.com",
			wantReason: SkipReasonInvalidScheme,
		},
		{
			name:       "telegram domain t.me",
			url:        "https://t.me/channel/123",
			wantReason: SkipReasonTelegramDomain,
		},
		{
			name:       "telegram domain telegram.me",
			url:        "https://telegram.me/channel",
			wantReason: SkipReasonTelegramDomain,
		},
		{
			name:       "telegram domain telesco.pe",
			url:        "https://telesco.pe/channel",
			wantReason: SkipReasonTelegramDomain,
		},
		{
			name:       "denied extension pdf",
			url:        "https://example.com/document.pdf",
			wantReason: SkipReasonDeniedExtension,
		},
		{
			name:       "denied extension exe",
			url:        "https://example.com/program.exe",
			wantReason: SkipReasonDeniedExtension,
		},
		{
			name:       "allowed extension html",
			url:        "https://example.com/page.html",
			wantReason: "",
		},
		{
			name:       "denied domain",
			url:        "https://badsite.com/page",
			wantReason: SkipReasonDeniedDomain,
		},
		{
			name:       "denied domain subdomain",
			url:        "https://sub.badsite.com/page",
			wantReason: SkipReasonDeniedDomain,
		},
		{
			name:       "url with tracking params should pass filter",
			url:        "https://example.com/page?utm_source=test",
			wantReason: "",
		},
		{
			name:       "url without path",
			url:        "https://example.com",
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := seeder.filterURL(tt.url)
			if got != tt.wantReason {
				t.Errorf(filterURLErrFormat, tt.url, got, tt.wantReason)
			}
		})
	}
}

func TestFilterURLWithAllowlist(t *testing.T) {
	seeder := &Seeder{
		domainAllowlist: map[string]struct{}{
			"allowed.com":    {},
			"alsoallowed.io": {},
		},
		domainDenylist:    nil,
		extensionDenylist: nil,
	}

	tests := []struct {
		name       string
		url        string
		wantReason string
	}{
		{
			name:       "allowed domain",
			url:        "https://allowed.com/page",
			wantReason: "",
		},
		{
			name:       "allowed domain subdomain",
			url:        "https://sub.allowed.com/page",
			wantReason: "",
		},
		{
			name:       "not in allowlist",
			url:        "https://notallowed.com/page",
			wantReason: SkipReasonNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := seeder.filterURL(tt.url)
			if got != tt.wantReason {
				t.Errorf(filterURLErrFormat, tt.url, got, tt.wantReason)
			}
		})
	}
}

func TestMatchesDomain(t *testing.T) {
	seeder := &Seeder{}

	domains := map[string]struct{}{
		"example.com": {},
		"test.io":     {},
	}

	tests := []struct {
		name  string
		host  string
		match bool
	}{
		{"exact match", "example.com", true},
		{"subdomain match", "sub.example.com", true},
		{"deep subdomain", "deep.sub.example.com", true},
		{"no match", "notexample.com", false},
		{"partial match should fail", "myexample.com", false},
		{"other domain match", "test.io", true},
		{"other domain subdomain", "api.test.io", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := seeder.matchesDomain(tt.host, domains)
			if got != tt.match {
				t.Errorf("matchesDomain(%q) = %v, want %v", tt.host, got, tt.match)
			}
		})
	}
}

func TestBuildExtensionDenylist(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect map[string]struct{}
	}{
		{
			name:  "with dots",
			input: []string{".pdf", ".exe"},
			expect: map[string]struct{}{
				".pdf": {},
				".exe": {},
			},
		},
		{
			name:  "without dots",
			input: []string{"pdf", "exe"},
			expect: map[string]struct{}{
				".pdf": {},
				".exe": {},
			},
		},
		{
			name:  "mixed case",
			input: []string{".PDF", "EXE"},
			expect: map[string]struct{}{
				".pdf": {},
				".exe": {},
			},
		},
		{
			name:  "with whitespace",
			input: []string{" .pdf ", "  exe  "},
			expect: map[string]struct{}{
				".pdf": {},
				".exe": {},
			},
		},
		{
			name:   "empty strings filtered",
			input:  []string{"", "  ", ".pdf"},
			expect: map[string]struct{}{".pdf": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildExtensionDenylist(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("buildExtensionDenylist() len = %d, want %d", len(got), len(tt.expect))
			}

			for k := range tt.expect {
				if _, ok := got[k]; !ok {
					t.Errorf("buildExtensionDenylist() missing key %q", k)
				}
			}
		})
	}
}

func TestBuildDomainSet(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect map[string]struct{}
	}{
		{
			name:  "normal domains",
			input: []string{"example.com", "test.io"},
			expect: map[string]struct{}{
				"example.com": {},
				"test.io":     {},
			},
		},
		{
			name:  "uppercase normalized",
			input: []string{"Example.COM", "TEST.io"},
			expect: map[string]struct{}{
				"example.com": {},
				"test.io":     {},
			},
		},
		{
			name:  "with whitespace",
			input: []string{" example.com ", "  test.io  "},
			expect: map[string]struct{}{
				"example.com": {},
				"test.io":     {},
			},
		},
		{
			name:   "empty strings filtered",
			input:  []string{"", "  ", "example.com"},
			expect: map[string]struct{}{"example.com": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDomainSet(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("buildDomainSet() len = %d, want %d", len(got), len(tt.expect))
			}

			for k := range tt.expect {
				if _, ok := got[k]; !ok {
					t.Errorf("buildDomainSet() missing key %q", k)
				}
			}
		})
	}
}

func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "simple list",
			input:  "a,b,c",
			expect: []string{"a", "b", "c"},
		},
		{
			name:   "with spaces",
			input:  "a , b , c",
			expect: []string{"a", "b", "c"},
		},
		{
			name:   "empty string",
			input:  "",
			expect: nil,
		},
		{
			name:   "single item",
			input:  "single",
			expect: []string{"single"},
		},
		{
			name:   "empty items filtered",
			input:  "a,,b,  ,c",
			expect: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCommaSeparated(tt.input)

			if tt.expect == nil {
				if got != nil {
					t.Errorf("parseCommaSeparated(%q) = %v, want nil", tt.input, got)
				}

				return
			}

			if len(got) != len(tt.expect) {
				t.Errorf("parseCommaSeparated(%q) len = %d, want %d", tt.input, len(got), len(tt.expect))
			}

			for i := range tt.expect {
				if got[i] != tt.expect[i] {
					t.Errorf("parseCommaSeparated(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expect[i])
				}
			}
		})
	}
}
