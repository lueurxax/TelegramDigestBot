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

func TestYaCyProvider_SearchWithLanguage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		resp := `{"channels": [{"items": [
			{"link": "http://example.com/en", "title": "English Title", "description": "This is in English", "ranking": 1.0},
			{"link": "http://example.com/ru", "title": "Русский заголовок", "description": "Это на русском", "ranking": 1.0},
			{"link": "http://example.com/el", "title": "Ελληνικός τίτλος", "description": "Αυτό είναι στα ελληνικά", "ranking": 1.0}
		]}]}`

		if _, err := w.Write([]byte(resp)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewYaCyProvider(YaCyConfig{
		Enabled: true,
		BaseURL: ts.URL,
	})

	testCases := []struct {
		name        string
		lang        string
		expected    int
		expectedURL string
	}{
		{"English", "en", 1, "http://example.com/en"},
		{"Russian", "ru", 1, "http://example.com/ru"},
		{"Greek", "el", 1, "http://example.com/el"},
		{"Unknown", "", 3, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := p.SearchWithLanguage(context.Background(), yacyTestQuery, tc.lang, 10)
			if err != nil {
				t.Fatal(err)
			}

			if len(results) != tc.expected {
				t.Errorf("expected %d results, got %d", tc.expected, len(results))
			}

			if tc.expectedURL != "" && len(results) > 0 && results[0].URL != tc.expectedURL {
				t.Errorf(expectedURLFmt, results[0].URL)
			}
		})
	}
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
