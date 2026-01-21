package links

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testDomain      = "example.com"
	headerUserAgent = "User-Agent"
	headerAccept    = "Accept"
	testHTMLBody    = "<html><body>Test content</body></html>"
)

func TestNewWebFetcher(t *testing.T) {
	tests := []struct {
		name    string
		rps     float64
		timeout time.Duration
	}{
		{
			name:    "default timeout",
			rps:     2.0,
			timeout: 0,
		},
		{
			name:    "custom timeout",
			rps:     5.0,
			timeout: 10 * time.Second,
		},
		{
			name:    "negative timeout uses default",
			rps:     1.0,
			timeout: -1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := NewWebFetcher(tt.rps, tt.timeout)

			require.NotNil(t, fetcher, "NewWebFetcher() returned nil")
			require.NotNil(t, fetcher.client, "client is nil")
			require.NotNil(t, fetcher.globalLimiter, "globalLimiter is nil")
			require.NotNil(t, fetcher.domainLimiters, "domainLimiters is nil")
			require.NotEmpty(t, fetcher.userAgent, "userAgent is empty")
		})
	}
}

func TestWebFetcherExtractDomain(t *testing.T) {
	fetcher := NewWebFetcher(1, time.Second)

	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{
			name:   "simple domain",
			rawURL: "https://example.com/page",
			want:   "example.com",
		},
		{
			name:   "domain with subdomain",
			rawURL: "https://api.example.com/v1",
			want:   "api.example.com",
		},
		{
			name:   "domain with port",
			rawURL: "https://example.com:8080/page",
			want:   "example.com:8080",
		},
		{
			name:   "uppercase domain normalized",
			rawURL: "https://EXAMPLE.COM/page",
			want:   "example.com",
		},
		{
			name:   "invalid URL",
			rawURL: "not a valid url",
			want:   "",
		},
		{
			name:   "empty URL",
			rawURL: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fetcher.extractDomain(tt.rawURL)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestWebFetcherGetDomainLimiter(t *testing.T) {
	fetcher := NewWebFetcher(1, time.Second)

	// Get limiter for first domain
	limiter1 := fetcher.getDomainLimiter(testDomain)
	if limiter1 == nil {
		t.Fatal("getDomainLimiter() returned nil")
	}

	// Get limiter for same domain - should return same limiter
	limiter2 := fetcher.getDomainLimiter(testDomain)
	if limiter1 != limiter2 {
		t.Error("getDomainLimiter() should return same limiter for same domain")
	}

	// Get limiter for different domain - should return different limiter
	limiter3 := fetcher.getDomainLimiter("other.com")
	if limiter1 == limiter3 {
		t.Error("getDomainLimiter() should return different limiter for different domain")
	}
}

func TestWebFetcherFetch(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify headers
			if r.Header.Get(headerUserAgent) == "" {
				t.Error("User-Agent header not set")
			}

			if r.Header.Get(headerAccept) == "" {
				t.Error("Accept header not set")
			}

			w.WriteHeader(http.StatusOK)

			if _, err := w.Write([]byte(testHTMLBody)); err != nil {
				t.Errorf("write response body: %v", err)
			}
		}))
		defer server.Close()

		fetcher := NewWebFetcher(10, 5*time.Second)

		body, err := fetcher.Fetch(context.Background(), server.URL)
		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}

		if string(body) != testHTMLBody {
			t.Errorf("Fetch() body = %q, want %q", string(body), testHTMLBody)
		}
	})

	t.Run("non-200 status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		fetcher := NewWebFetcher(10, 5*time.Second)

		_, err := fetcher.Fetch(context.Background(), server.URL)
		if err == nil {
			t.Fatal("Fetch() expected error for 404 status")
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fetcher := NewWebFetcher(10, 5*time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := fetcher.Fetch(ctx, server.URL)
		if err == nil {
			t.Error("Fetch() expected error for canceled context")
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		fetcher := NewWebFetcher(10, 5*time.Second)

		_, err := fetcher.Fetch(context.Background(), "://invalid-url")
		if err == nil {
			t.Error("Fetch() expected error for invalid URL")
		}
	})
}

func TestWebFetcherRedirectLimit(t *testing.T) {
	redirectCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount <= 10 {
			http.Redirect(w, r, "/redirect", http.StatusFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fetcher := NewWebFetcher(10, 5*time.Second)
	_, err := fetcher.Fetch(context.Background(), server.URL)

	// Should fail due to too many redirects
	if err == nil {
		t.Error("Fetch() expected error for too many redirects")
	}
}
