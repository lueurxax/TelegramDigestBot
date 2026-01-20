package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testBaseURL      = "http://localhost:8888"
	testArticleURL1  = "https://example.com/article1"
	testArticleURL2  = "https://example.org/article2"
	testDomain       = "example.com"
	testQuery        = "test"
	testQueryFull    = "test query"
	testTitle1       = "Test Article 1"
	testDescription1 = "Description of article 1"
	searxngAPIError  = "searxng api error"
	searxngErrFmt    = "expected error to contain %q, got: %v"
)

func TestNewSearxNGProvider(t *testing.T) {
	t.Run("uses default timeout when not specified", func(t *testing.T) {
		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: testBaseURL,
		})

		if p.httpClient.Timeout != searxngDefaultTimeout {
			t.Errorf("got %v, want %v", p.httpClient.Timeout, searxngDefaultTimeout)
		}
	})

	t.Run("uses custom timeout when specified", func(t *testing.T) {
		customTimeout := 60 * time.Second
		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: testBaseURL,
			Timeout: customTimeout,
		})

		if p.httpClient.Timeout != customTimeout {
			t.Errorf("got timeout %v, want %v", p.httpClient.Timeout, customTimeout)
		}
	})

	t.Run("stores engine list", func(t *testing.T) {
		engines := []string{"google", "duckduckgo", "bing"}
		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: testBaseURL,
			Engines: engines,
		})

		if len(p.engines) != len(engines) {
			t.Errorf("engines count: got %d, want %d", len(p.engines), len(engines))
		}
	})

	t.Run("trims trailing slash from URL", func(t *testing.T) {
		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: testBaseURL + "/",
		})

		if p.baseURL != testBaseURL {
			t.Errorf("got %q, want %q", p.baseURL, testBaseURL)
		}
	})
}

func TestSearxNGProvider_Name(t *testing.T) {
	p := NewSearxNGProvider(SearxNGConfig{})
	if p.Name() != ProviderSearxNG {
		t.Errorf("got provider name %v, want %v", p.Name(), ProviderSearxNG)
	}
}

func TestSearxNGProvider_IsAvailable(t *testing.T) {
	t.Run("returns false when disabled", func(t *testing.T) {
		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: false,
			BaseURL: testBaseURL,
		})

		if p.IsAvailable() {
			t.Error("expected unavailable when disabled")
		}
	})

	t.Run("returns false when no base URL", func(t *testing.T) {
		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: "",
		})

		if p.IsAvailable() {
			t.Error("expected unavailable when no base URL")
		}
	})

	t.Run("returns true when server responds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: server.URL,
		})

		if !p.IsAvailable() {
			t.Error("expected available when server responds")
		}
	})
}

func TestSearxNGProvider_Search_Disabled(t *testing.T) {
	p := NewSearxNGProvider(SearxNGConfig{
		Enabled: false,
	})

	_, err := p.Search(context.Background(), testQuery, 10)
	if !errors.Is(err, errProviderNotFound) {
		t.Errorf("got error %v, want %v", err, errProviderNotFound)
	}
}

func TestSearxNGProvider_Search_ParsesResults(t *testing.T) {
	resp := searxngResponse{
		Query: testQueryFull,
		Results: []searxngResult{
			{
				URL:           testArticleURL1,
				Title:         testTitle1,
				Content:       testDescription1,
				PublishedDate: "2024-01-15T10:30:00Z",
				Engine:        "google",
				Score:         1.5,
			},
			{
				URL:     testArticleURL2,
				Title:   "Test Article 2",
				Content: "Description of article 2",
				Engine:  "duckduckgo",
				Score:   1.2,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, httpContentTypeJSON)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	p := NewSearxNGProvider(SearxNGConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQueryFull, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("result count: got %d, want 2", len(results))
	}

	if results[0].URL != testArticleURL1 {
		t.Errorf("URL: got %q, want %q", results[0].URL, testArticleURL1)
	}

	if results[0].Title != testTitle1 {
		t.Errorf("Title: got %q, want %q", results[0].Title, testTitle1)
	}

	if results[0].Description != testDescription1 {
		t.Errorf("Description: got %q, want %q", results[0].Description, testDescription1)
	}

	if results[0].Domain != testDomain {
		t.Errorf("Domain: got %q, want %q", results[0].Domain, testDomain)
	}

	if results[0].PublishedAt.IsZero() {
		t.Error("PublishedAt should not be zero")
	}
}

func TestSearxNGProvider_Search_RespectsMaxResults(t *testing.T) {
	resp := searxngResponse{
		Query: testQuery,
		Results: []searxngResult{
			{URL: "https://example.com/1", Title: "Article 1"},
			{URL: "https://example.com/2", Title: "Article 2"},
			{URL: "https://example.com/3", Title: "Article 3"},
			{URL: "https://example.com/4", Title: "Article 4"},
			{URL: "https://example.com/5", Title: "Article 5"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, httpContentTypeJSON)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	p := NewSearxNGProvider(SearxNGConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 3)
	if err != nil {
		t.Fatalf("search failed with max 3: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("result count: got %d, want 3", len(results))
	}
}

func TestSearxNGProvider_Search_SkipsEmptyURL(t *testing.T) {
	resp := searxngResponse{
		Query: testQuery,
		Results: []searxngResult{
			{URL: "https://example.com/1", Title: "Valid"},
			{URL: "", Title: "Invalid - No URL"},
			{URL: "https://example.com/2", Title: "Also Valid"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, httpContentTypeJSON)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	p := NewSearxNGProvider(SearxNGConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 10)
	if err != nil {
		t.Fatalf("search with empty URLs failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("result count after filtering: got %d, want 2", len(results))
	}
}

func TestSearxNGProvider_Search_IncludesEngines(t *testing.T) {
	var capturedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery

		w.Header().Set(httpHeaderContent, httpContentTypeJSON)

		if err := json.NewEncoder(w).Encode(searxngResponse{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	p := NewSearxNGProvider(SearxNGConfig{
		Enabled: true,
		BaseURL: server.URL,
		Engines: []string{"google", "duckduckgo"},
	})

	_, err := p.Search(context.Background(), testQuery, 10)
	if err != nil {
		t.Fatalf("search with engines failed: %v", err)
	}

	if capturedQuery == "" {
		t.Fatal("no query captured")
	}

	// Check that engines parameter is in the query
	if !strings.Contains(capturedQuery, "engines=") {
		t.Errorf("query should contain engines parameter: %s", capturedQuery)
	}
}

func TestParseSearxNGDate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantZero bool
	}{
		{"RFC3339", "2024-01-15T10:30:00Z", false},
		{"RFC3339 with timezone", "2024-01-15T10:30:00-05:00", false},
		{"date only", "2024-01-15", false},
		{"US format", "Jan 2, 2024", false},
		{"invalid format", "not a date", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSearxNGDate(tt.input)
			if tt.wantZero && !result.IsZero() {
				t.Errorf("expected zero time for %q", tt.input)
			}

			if !tt.wantZero && result.IsZero() {
				t.Errorf("expected non-zero time for %q", tt.input)
			}
		})
	}
}

func TestSearxNGProvider_Search_NonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte("<html><body>SearxNG Error Page</body></html>"))
		if err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	p := NewSearxNGProvider(SearxNGConfig{
		Enabled: true,
		BaseURL: ts.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), searxngAPIError) {
		t.Errorf(searxngErrFmt, searxngAPIError, err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
