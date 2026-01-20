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
	gdeltTestQuery           = "test"
	gdeltExpectedErrGotNil   = "expected error, got nil"
	gdeltExpected0ResultsGot = "expected 0 results, got %d"
	gdeltExpected1ResultGot  = "expected 1 result, got %d"
	gdeltFailedToWriteResp   = "failed to write response: %v"
	gdeltAPIErrorStr         = "gdelt api error"
	gdeltExpectedErrFmt      = "expected error to contain %q, got: %v"
	gdeltTestURL1            = "https://example.com/1"
	gdeltExpectedURLFmt      = "expected URL https://example.com/1, got %s"
)

func TestGDELTProvider_Search_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte(`{"articles": [{"url": "` + gdeltTestURL1 + `", "title": "Test 1", "domain": "example.com", "seendate": "20260120T065540Z"}]}`))
		if err != nil {
			t.Errorf(gdeltFailedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewGDELTProvider(GDELTConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), gdeltTestQuery, 1)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if len(results) != 1 {
		t.Errorf(gdeltExpected1ResultGot, len(results))
	}

	if results[0].URL != gdeltTestURL1 {
		t.Errorf(gdeltExpectedURLFmt, results[0].URL)
	}
}

func TestGDELTProvider_Search_NonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte("Your query was too broad. Please try again with more specific keywords."))
		if err != nil {
			t.Errorf(gdeltFailedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewGDELTProvider(GDELTConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), gdeltTestQuery, 1)
	if err == nil {
		t.Fatal(gdeltExpectedErrGotNil)
	}

	if !strings.Contains(err.Error(), gdeltAPIErrorStr) {
		t.Errorf(gdeltExpectedErrFmt, gdeltAPIErrorStr, err)
	}

	if len(results) != 0 {
		t.Errorf(gdeltExpected0ResultsGot, len(results))
	}
}

func TestGDELTProvider_Search_NoResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte(`{"articles": []}`))
		if err != nil {
			t.Errorf(gdeltFailedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewGDELTProvider(GDELTConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), gdeltTestQuery, 1)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if len(results) != 0 {
		t.Errorf(gdeltExpected0ResultsGot, len(results))
	}
}

func TestGDELTProvider_Search_NonJSONResponse_Truncated(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte(strings.Repeat("Too long error message. ", 20)))
		if err != nil {
			t.Errorf(gdeltFailedToWriteResp, err)
		}
	}))
	defer ts.Close()

	p := NewGDELTProvider(GDELTConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
	})
	p.baseURL = ts.URL

	results, err := p.Search(context.Background(), gdeltTestQuery, 1)
	if err == nil {
		t.Fatal(gdeltExpectedErrGotNil)
	}

	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("expected error to be truncated with ..., got: %v", err)
	}

	if len(results) != 0 {
		t.Errorf(gdeltExpected0ResultsGot, len(results))
	}
}
