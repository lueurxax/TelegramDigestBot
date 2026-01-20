package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestExtractor_Extract_BinaryContent_Repro(t *testing.T) {
	// Create a test server that returns a PDF
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")

		_, err := w.Write([]byte(pdfBinary))
		if err != nil {
			t.Errorf(fmtErrFailedToWriteResp, err)
		}
	}))
	defer server.Close()

	logger := zerolog.New(nil)
	e := NewExtractor(&logger)

	res := SearchResult{
		URL:    server.URL,
		Title:  "Test PDF",
		Domain: "example.com",
	}

	evidence, err := e.Extract(context.Background(), res, testProvider, time.Hour)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if evidence.Source.Content != "" {
		t.Errorf("expected empty content for PDF, got %q", evidence.Source.Content)
	}

	if !evidence.Source.ExtractionFailed {
		t.Error(errFmtExtractionFailed)
	}
}

func TestExtractor_Extract_BinaryContent_NoContentType_Repro(t *testing.T) {
	// Create a test server that returns a PDF without Content-Type
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No Content-Type header
		_, err := w.Write([]byte(pdfBinary))
		if err != nil {
			t.Errorf(fmtErrFailedToWriteResp, err)
		}
	}))
	defer server.Close()

	logger := zerolog.New(nil)
	e := NewExtractor(&logger)

	res := SearchResult{
		URL:    server.URL,
		Title:  "Test PDF",
		Domain: "example.com",
	}

	evidence, err := e.Extract(context.Background(), res, testProvider, time.Hour)
	if err != nil {
		t.Fatalf(unexpectedErrFmt, err)
	}

	if evidence.Source.Content != "" {
		t.Errorf("expected empty content for PDF without Content-Type, got %q", evidence.Source.Content)
	}

	if !evidence.Source.ExtractionFailed {
		t.Error(errFmtExtractionFailed)
	}
}

const (
	errFmtExtractionFailed  = "expected ExtractionFailed to be true"
	testProvider            = "test"
	fmtErrFailedToWriteResp = "failed to write response: %v"
	pdfBinary               = "%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n4 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 72 712 Td (Hello World) Tj ET\nendstream\nendobj\nxref\n0 5\n0000000000 65535 f\n0000000009 00000 n\n0000000060 00000 n\n0000000116 00000 n\n0000000213 00000 n\ntrailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n308\n%%EOF"
)
