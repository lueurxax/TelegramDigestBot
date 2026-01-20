package enrichment

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewsAPIProvider_Search_RateLimited(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)

		if _, err := w.Write([]byte(`{
			"status": "error",
			"code": "rateLimited",
			"message": "You have made too many requests recently. Developer accounts are limited to 100 requests per 24 hours."
		}`)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewNewsAPIProvider(NewsAPIConfig{
		Enabled:        true,
		APIKey:         "test-key",
		RequestsPerMin: 100,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), testQueryFull, 5)
	if results != nil {
		t.Error("expected nil results on 429")
	}

	if err == nil {
		t.Fatal(expectedErrGotNil)
	}

	if !errors.Is(err, errNewsAPIRateLimited) {
		t.Errorf("expected errNewsAPIRateLimited, got %v", err)
	}
}

func TestNewsAPIProvider_Search_ErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)

		if _, err := w.Write([]byte(`{
			"status": "error",
			"code": "parameterInvalid",
			"message": "The parameter q is invalid."
		}`)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewNewsAPIProvider(NewsAPIConfig{
		Enabled:        true,
		APIKey:         "test-key",
		RequestsPerMin: 100,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), testQueryFull, 5)
	if results != nil {
		t.Error("expected nil results on error")
	}

	if err == nil {
		t.Fatal(expectedErrGotNil)
	}

	expectedMsg := "newsapi api error: The parameter q is invalid. (parameterInvalid)"
	if err.Error() != expectedMsg {
		t.Errorf(expectedFmt, expectedMsg, err.Error())
	}
}

func TestNewsAPIProvider_Search_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(`{
			"status": "ok",
			"totalResults": 1,
			"articles": [
				{
					"title": "Test Title",
					"description": "Test Description",
					"url": "https://example.com/test",
					"publishedAt": "2026-01-20T10:00:00Z"
				}
			]
		}`)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewNewsAPIProvider(NewsAPIConfig{
		Enabled:        true,
		APIKey:         "test-key",
		RequestsPerMin: 100,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), testQueryFull, 5)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if len(results) != 1 {
		t.Fatalf(expected1ResultGot, len(results))
	}

	if results[0].Title != "Test Title" {
		t.Errorf("expected title 'Test Title', got %q", results[0].Title)
	}
}

func TestNewsAPIProvider_Search_NonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)

		if _, err := w.Write([]byte(`Something went wrong`)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewNewsAPIProvider(NewsAPIConfig{
		Enabled:        true,
		APIKey:         "test-key",
		RequestsPerMin: 100,
	})
	p.baseURL = ts.URL

	_, err := p.Search(context.Background(), testQueryFull, 5)
	if err == nil {
		t.Fatal(expectedErrGotNil)
	}

	expectedMsg := "newsapi api error: Something went wrong"
	if err.Error() != expectedMsg {
		t.Errorf(expectedFmt, expectedMsg, err.Error())
	}
}

func TestNewsAPIProvider_SearchWithLanguage_SetsLanguage(t *testing.T) {
	var capturedLang string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLang = r.URL.Query().Get(newsAPIParamLanguage)

		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(newsAPIEmptyResponse)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewNewsAPIProvider(NewsAPIConfig{
		Enabled:        true,
		APIKey:         "test-key",
		RequestsPerMin: 100,
	})
	p.baseURL = ts.URL

	_, err := p.SearchWithLanguage(context.Background(), testQueryFull, "ru", 5)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if capturedLang != "ru" {
		t.Errorf("expected language param ru, got %q", capturedLang)
	}
}

func TestNewsAPIProvider_SearchWithLanguage_UnsupportedLanguage(t *testing.T) {
	var capturedLang string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLang = r.URL.Query().Get(newsAPIParamLanguage)

		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(newsAPIEmptyResponse)); err != nil {
			t.Errorf(failedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewNewsAPIProvider(NewsAPIConfig{
		Enabled:        true,
		APIKey:         "test-key",
		RequestsPerMin: 100,
	})
	p.baseURL = ts.URL

	_, err := p.SearchWithLanguage(context.Background(), testQueryFull, "el", 5)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if capturedLang != "" {
		t.Errorf("expected no language param for unsupported language, got %q", capturedLang)
	}
}
