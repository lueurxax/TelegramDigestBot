package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	yacyTestQuery           = "test"
	yacyExpectedErrGotNil   = "expected error, got nil"
	yacyExpected0ResultsGot = "expected 0 results, got %d"
	yacyFailedToWriteResp   = "failed to write response: %v"
	yacyAPIErrorStr         = "yacy api error"
)

func TestYaCyProvider_Search_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte(`{"channels": [{"items": [{"link": "https://example.com/1", "title": "Test 1", "description": "Desc 1", "ranking": 1.0}]}]}`))
		if err != nil {
			t.Errorf(yacyFailedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewYaCyProvider(YaCyConfig{
		Enabled: true,
		BaseURL: ts.URL,
		Timeout: 5 * time.Second,
	})

	results, err := p.Search(context.Background(), yacyTestQuery, 1)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].URL != "https://example.com/1" {
		t.Errorf("expected URL https://example.com/1, got %s", results[0].URL)
	}
}

func TestYaCyProvider_Search_NonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte("<html><body>YaCy Error Page</body></html>"))
		if err != nil {
			t.Errorf(yacyFailedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewYaCyProvider(YaCyConfig{
		Enabled: true,
		BaseURL: ts.URL,
		Timeout: 5 * time.Second,
	})

	results, err := p.Search(context.Background(), yacyTestQuery, 1)
	if err == nil {
		t.Fatal(yacyExpectedErrGotNil)
	}

	if !strings.Contains(err.Error(), yacyAPIErrorStr) {
		t.Errorf("expected error to contain %q, got: %v", yacyAPIErrorStr, err)
	}

	if len(results) != 0 {
		t.Errorf(yacyExpected0ResultsGot, len(results))
	}
}
