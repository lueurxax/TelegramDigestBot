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
	yacyTestQuery   = "test"
	yacyAPIErrorStr = "yacy api error"
)

func TestYaCyProvider_Search_Success(t *testing.T) {
	runYaCySuccessTest(t, `{"channels": [{"items": [{"link": "`+testURL1+`", "title": "Test 1", "description": "Desc 1", "ranking": 1.0}]}]}`)
}

func TestYaCyProvider_Search_NonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte("<html><body>YaCy Error Page</body></html>"))
		if err != nil {
			t.Errorf(failedToWriteResp, err)
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
		t.Fatal(expectedErrGotNil)
	}

	if !strings.Contains(err.Error(), yacyAPIErrorStr) {
		t.Errorf("expected error to contain %q, got: %v", yacyAPIErrorStr, err)
	}

	if len(results) != 0 {
		t.Errorf(expected0ResultsGot, len(results))
	}
}

func TestYaCyProvider_Search_JSONWithWhitespace(t *testing.T) {
	// Leading newline and spaces before JSON
	runYaCySuccessTest(t, "\n  \t {\"channels\": [{\"items\": [{\"link\": \""+testURL1+"\", \"title\": \"Test 1\", \"description\": \"Desc 1\", \"ranking\": 1.0}]}]}")
}

func runYaCySuccessTest(t *testing.T, response string) {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(response)); err != nil {
			t.Errorf(failedToWriteResp, err)
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
		t.Errorf(expected1ResultGot, len(results))
	}

	if results[0].URL != testURL1 {
		t.Errorf(expectedURLFmt, results[0].URL)
	}
}
