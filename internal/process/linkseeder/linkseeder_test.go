package linkseeder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
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

func TestSeedInputFields(t *testing.T) {
	// Test that SeedInput has correct field types for traceability
	input := SeedInput{
		PeerID:    1234567890,
		MessageID: 42,
		Channel:   "@example",
		URLs:      []string{"https://example.com"},
	}

	if input.PeerID != 1234567890 {
		t.Errorf("PeerID = %d, want 1234567890", input.PeerID)
	}

	if input.MessageID != 42 {
		t.Errorf("MessageID = %d, want 42", input.MessageID)
	}

	if input.Channel != "@example" {
		t.Errorf("Channel = %q, want @example", input.Channel)
	}
}

func TestSeedResultInit(t *testing.T) {
	// Test that SeedResult initializes correctly with skip tracking
	result := SeedResult{
		Skipped: make(map[string]int),
	}

	result.Skipped[SkipReasonDisabled] = 5
	result.Skipped[SkipReasonDeniedDomain] = 3

	totalSkipped := 0
	for _, count := range result.Skipped {
		totalSkipped += count
	}

	if totalSkipped != 8 {
		t.Errorf("expected 8 total skipped, got %d", totalSkipped)
	}
}

func TestEnqueueURLCanonicalizes(t *testing.T) {
	rawURL := "HTTP://Example.com:80/path/?utm_source=a&b=2#frag"
	expectedCanonical := "http://example.com/path?b=2"
	expectedDocID := solr.WebDocID(expectedCanonical)

	var indexedDoc map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/get"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/update"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read update body: %v", err)
			}

			var docs []map[string]interface{}
			if err := json.Unmarshal(body, &docs); err != nil {
				t.Fatalf("unmarshal update body: %v", err)
			}

			if len(docs) != 1 {
				t.Fatalf("expected 1 doc, got %d", len(docs))
			}

			indexedDoc = docs[0]
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := solr.New(solr.Config{BaseURL: server.URL})
	logger := zerolog.New(io.Discard)
	seeder := New(Config{}, client, &logger)

	if err := seeder.enqueueURL(context.Background(), rawURL, "tg://peer/1/msg/2"); err != nil {
		t.Fatalf("enqueueURL error: %v", err)
	}

	if indexedDoc == nil {
		t.Fatalf("expected indexed document")
	}

	if gotID, _ := indexedDoc["id"].(string); gotID != expectedDocID {
		t.Fatalf("doc id = %q, want %q", gotID, expectedDocID)
	}

	if gotCanonical, _ := indexedDoc["url_canonical"].(string); gotCanonical != expectedCanonical {
		t.Fatalf("url_canonical = %q, want %q", gotCanonical, expectedCanonical)
	}

	if gotURL, _ := indexedDoc["url"].(string); gotURL != rawURL {
		t.Fatalf("url = %q, want %q", gotURL, rawURL)
	}

	if gotDomain, _ := indexedDoc["domain"].(string); gotDomain != "example.com" {
		t.Fatalf("domain = %q, want example.com", gotDomain)
	}
}

func TestEnqueueURLDuplicate(t *testing.T) {
	updateCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/get"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"doc":{"id":"existing"}}`))
		case strings.HasPrefix(r.URL.Path, "/update"):
			updateCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := solr.New(solr.Config{BaseURL: server.URL})
	logger := zerolog.New(io.Discard)
	seeder := New(Config{}, client, &logger)

	err := seeder.enqueueURL(context.Background(), "https://example.com/article", "tg://peer/1/msg/2")
	if !errors.Is(err, errDuplicate) {
		t.Fatalf("expected errDuplicate, got %v", err)
	}

	if updateCalled {
		t.Fatalf("did not expect update call when document exists")
	}
}
