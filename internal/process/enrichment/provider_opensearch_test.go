package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	opensearchTestIndex   = "articles"
	opensearchTestBaseURL = "http://localhost:9200"
	errFmtSearchError     = "Search() error = %v"
	errFmtGotTimeout      = "got timeout %v, want %v"
	errFmtGotIndex        = "got index %s, want %s"
	testContentTypeJSON   = "application/json"
)

func TestNewOpenSearchProvider(t *testing.T) {
	t.Run("default timeout", func(t *testing.T) {
		p := NewOpenSearchProvider(OpenSearchConfig{
			Enabled: true,
			BaseURL: opensearchTestBaseURL,
		})

		if p.httpClient.Timeout != opensearchDefaultTimeout {
			t.Errorf(errFmtGotTimeout, p.httpClient.Timeout, opensearchDefaultTimeout)
		}
	})

	t.Run("custom timeout", func(t *testing.T) {
		customTimeout := 45 * time.Second
		p := NewOpenSearchProvider(OpenSearchConfig{
			Enabled: true,
			BaseURL: opensearchTestBaseURL,
			Timeout: customTimeout,
		})

		if p.httpClient.Timeout != customTimeout {
			t.Errorf(errFmtGotTimeout, p.httpClient.Timeout, customTimeout)
		}
	})

	t.Run("default index", func(t *testing.T) {
		p := NewOpenSearchProvider(OpenSearchConfig{
			Enabled: true,
			BaseURL: opensearchTestBaseURL,
		})

		if p.index != opensearchDefaultIndex {
			t.Errorf(errFmtGotIndex, p.index, opensearchDefaultIndex)
		}
	})

	t.Run("custom index", func(t *testing.T) {
		p := NewOpenSearchProvider(OpenSearchConfig{
			Enabled: true,
			BaseURL: opensearchTestBaseURL,
			Index:   opensearchTestIndex,
		})

		if p.index != opensearchTestIndex {
			t.Errorf(errFmtGotIndex, p.index, opensearchTestIndex)
		}
	})

	t.Run("URL normalization", func(t *testing.T) {
		p := NewOpenSearchProvider(OpenSearchConfig{
			Enabled: true,
			BaseURL: opensearchTestBaseURL + "/",
		})

		if p.baseURL != opensearchTestBaseURL {
			t.Errorf("expected trailing slash to be removed, got %s", p.baseURL)
		}
	})
}

func TestOpenSearchProvider_Name(t *testing.T) {
	p := NewOpenSearchProvider(OpenSearchConfig{Enabled: true})
	if p.Name() != ProviderOpenSearch {
		t.Errorf("got name %s, want %s", p.Name(), ProviderOpenSearch)
	}
}

func TestOpenSearchProvider_IsAvailable_Disabled(t *testing.T) {
	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: false,
		BaseURL: opensearchTestBaseURL,
	})

	if p.IsAvailable(context.Background()) {
		t.Error("expected disabled provider to be unavailable")
	}
}

func TestOpenSearchProvider_IsAvailable_EmptyURL(t *testing.T) {
	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: "",
	})

	if p.IsAvailable(context.Background()) {
		t.Error("expected provider with empty base URL to be unavailable")
	}
}

func TestOpenSearchProvider_IsAvailable_Reachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	if !p.IsAvailable(context.Background()) {
		t.Error("expected reachable server to be available")
	}
}

func TestOpenSearchProvider_IsAvailable_Unreachable(t *testing.T) {
	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: "http://localhost:99999",
		Timeout: 100 * time.Millisecond,
	})

	if p.IsAvailable(context.Background()) {
		t.Error("expected unreachable server to be unavailable")
	}
}

func TestOpenSearchProvider_Search_Success(t *testing.T) {
	response := createTestResponse()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("got method %s, want POST", r.Method)
		}

		if r.URL.Path != "/news/_search" {
			t.Errorf("got path %s, want /news/_search", r.URL.Path)
		}

		w.Header().Set(httpHeaderContent, testContentTypeJSON)
		writeJSON(w, response)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQueryFull, 5)
	if err != nil {
		t.Fatalf(errFmtSearchError, err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if results[0].URL != "https://example.com/article1" {
		t.Errorf("got URL %s, want https://example.com/article1", results[0].URL)
	}

	if results[0].Title != "Test Article 1" {
		t.Errorf("got title %s, want Test Article 1", results[0].Title)
	}

	if results[0].Domain != "example.com" {
		t.Errorf("got domain %s, want example.com", results[0].Domain)
	}

	if results[0].Score != 1.5 {
		t.Errorf("got score %f, want 1.5", results[0].Score)
	}
}

func TestOpenSearchProvider_Search_Disabled(t *testing.T) {
	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: false,
		BaseURL: opensearchTestBaseURL,
	})

	_, err := p.Search(context.Background(), testQuery, 5)
	if err == nil {
		t.Error("expected error for disabled provider")
	}
}

func TestOpenSearchProvider_Search_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	_, err := p.Search(context.Background(), testQuery, 5)
	if err == nil {
		t.Error("expected error for server error")
	}
}

func TestOpenSearchProvider_Search_EmptyResults(t *testing.T) {
	response := opensearchResponse{
		Hits: opensearchHits{
			Hits: []opensearchHit{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, testContentTypeJSON)
		writeJSON(w, response)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 5)
	if err != nil {
		t.Fatalf(errFmtSearchError, err)
	}

	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestOpenSearchProvider_Search_MaxResults(t *testing.T) {
	response := opensearchResponse{
		Hits: opensearchHits{
			Hits: []opensearchHit{
				{ID: "1", Source: opensearchDocument{URL: "https://a.com/1", Title: "A1"}},
				{ID: "2", Source: opensearchDocument{URL: "https://a.com/2", Title: "A2"}},
				{ID: "3", Source: opensearchDocument{URL: "https://a.com/3", Title: "A3"}},
				{ID: "4", Source: opensearchDocument{URL: "https://a.com/4", Title: "A4"}},
				{ID: "5", Source: opensearchDocument{URL: "https://a.com/5", Title: "A5"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, testContentTypeJSON)
		writeJSON(w, response)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 3)
	if err != nil {
		t.Fatalf(errFmtSearchError, err)
	}

	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestOpenSearchProvider_Search_SkipsEmptyURL(t *testing.T) {
	response := opensearchResponse{
		Hits: opensearchHits{
			Hits: []opensearchHit{
				{ID: "1", Source: opensearchDocument{URL: "", Title: "No URL"}},
				{ID: "2", Source: opensearchDocument{URL: "https://a.com/2", Title: "Has URL"}},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, testContentTypeJSON)
		writeJSON(w, response)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 5)
	if err != nil {
		t.Fatalf(errFmtSearchError, err)
	}

	if len(results) != 1 {
		t.Errorf("got %d results, want 1 (skipping empty URL)", len(results))
	}
}

func TestOpenSearchProvider_Search_ExtractsDomain(t *testing.T) {
	response := opensearchResponse{
		Hits: opensearchHits{
			Hits: []opensearchHit{
				{
					ID:    "1",
					Score: 1.0,
					Source: opensearchDocument{
						URL:   "https://news.example.com/article",
						Title: "Test",
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, testContentTypeJSON)
		writeJSON(w, response)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 5)
	if err != nil {
		t.Fatalf(errFmtSearchError, err)
	}

	if results[0].Domain != "news.example.com" {
		t.Errorf("got domain %s, want news.example.com", results[0].Domain)
	}
}

func TestOpenSearchProvider_Search_ContentAsDescriptionFallback(t *testing.T) {
	response := opensearchResponse{
		Hits: opensearchHits{
			Hits: []opensearchHit{
				{
					ID:    "1",
					Score: 1.0,
					Source: opensearchDocument{
						URL:     "https://example.com/article",
						Title:   "Test",
						Content: "This is the article content.",
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(httpHeaderContent, testContentTypeJSON)
		writeJSON(w, response)
	}))
	defer server.Close()

	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: server.URL,
	})

	results, err := p.Search(context.Background(), testQuery, 5)
	if err != nil {
		t.Fatalf(errFmtSearchError, err)
	}

	if results[0].Description == "" {
		t.Error("expected content to be used as description")
	}
}

func TestOpenSearchProvider_buildSearchURL(t *testing.T) {
	p := NewOpenSearchProvider(OpenSearchConfig{
		Enabled: true,
		BaseURL: opensearchTestBaseURL,
		Index:   opensearchTestIndex,
	})

	url := p.buildSearchURL()
	expected := opensearchTestBaseURL + "/" + opensearchTestIndex + "/_search"

	if url != expected {
		t.Errorf("got %s, want %s", url, expected)
	}
}

func createTestResponse() opensearchResponse {
	return opensearchResponse{
		Hits: opensearchHits{
			Hits: []opensearchHit{
				{
					ID:    "1",
					Score: 1.5,
					Source: opensearchDocument{
						URL:         "https://example.com/article1",
						Title:       "Test Article 1",
						Description: "Description 1",
						Domain:      "example.com",
						PublishedAt: "2024-01-15T10:00:00Z",
					},
				},
				{
					ID:    "2",
					Score: 1.2,
					Source: opensearchDocument{
						URL:         "https://example.org/article2",
						Title:       "Test Article 2",
						Description: "Description 2",
						Domain:      "example.org",
						PublishedAt: "2024-01-14T10:00:00Z",
					},
				},
			},
		},
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
