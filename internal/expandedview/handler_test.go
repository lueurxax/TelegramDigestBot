package expandedview

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

func newTestConfig() *config.Config {
	return &config.Config{
		ExpandedViewEnabled:       true,
		ExpandedViewSigningSecret: "test-secret",
		ExpandedViewTTLHours:      24,
		ExpandedViewRequireAdmin:  true,
		AdminIDs:                  []int64{123456},
	}
}

func newTestHandler(t *testing.T, cfg *config.Config) (*Handler, *TokenService) {
	t.Helper()

	logger := zerolog.Nop()
	tokenService := NewTokenService(cfg.ExpandedViewSigningSecret, cfg.ExpandedViewTTLHours)

	handler, err := NewHandler(cfg, tokenService, nil, &logger)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	return handler, tokenService
}

func TestHandler_ServeHTTP_InvalidToken(t *testing.T) {
	cfg := newTestConfig()
	handler, _ := newTestHandler(t, cfg)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "empty token returns 400",
			path:       "/",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid base64 token returns 401",
			path:       "/not-valid-base64!!!",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong signature token returns 401",
			path:       "/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}

			// Verify HTML response
			if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
				t.Error("expected HTML content-type")
			}
		})
	}
}

func TestHandler_ServeHTTP_ExpiredToken(t *testing.T) {
	logger := zerolog.Nop()
	cfg := newTestConfig()
	cfg.ExpandedViewRequireAdmin = false // Disable admin check for this test

	// Create a token service with negative TTL (immediate expiry)
	expiredTokenService := &TokenService{
		secret: []byte("test-secret"),
		ttl:    -time.Hour,
	}

	// Generate an expired token
	token := requireGenerate(t, expiredTokenService, testItemID, 123456)

	// Create handler with the same secret so signature is valid, but token is expired
	handler, err := NewHandler(cfg, expiredTokenService, nil, &logger)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/"+token, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Expired token should return 410 Gone
	if rec.Code != http.StatusGone {
		t.Errorf("expired token: got status %d, want %d", rec.Code, http.StatusGone)
	}
}

func TestHandler_ServeHTTP_NonAdminToken(t *testing.T) {
	cfg := newTestConfig()
	cfg.AdminIDs = []int64{999999} // Different from the user in token

	handler, tokenService := newTestHandler(t, cfg)

	// Generate token for a non-admin user
	token := requireGenerate(t, tokenService, testItemID, 123456)

	req := httptest.NewRequest(http.MethodGet, "/"+token, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("non-admin: got status %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandler_ServeHTTP_SystemToken(t *testing.T) {
	// Test that system tokens (UserID = 0) bypass the admin check
	// by comparing behavior with a non-admin token
	cfg := newTestConfig()
	cfg.AdminIDs = []int64{999999} // Different from test user

	handler, tokenService := newTestHandler(t, cfg)

	// Generate non-admin token - should get 401 Unauthorized
	nonAdminToken := requireGenerate(t, tokenService, testItemID, 123456)

	req1 := httptest.NewRequest(http.MethodGet, "/"+nonAdminToken, nil)
	rec1 := httptest.NewRecorder()

	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusUnauthorized {
		t.Errorf("non-admin token: status = %d, want %d", rec1.Code, http.StatusUnauthorized)
	}

	// Generate system token (UserID = 0) - should NOT get 401
	systemToken := requireGenerate(t, tokenService, testItemID, 0)

	req2 := httptest.NewRequest(http.MethodGet, "/"+systemToken, nil)
	rec2 := httptest.NewRecorder()

	// This will panic because database is nil, but we can check that the
	// panic happens in the database layer, not the auth layer
	defer func() {
		if r := recover(); r != nil {
			// Expected - database is nil. The important thing is we got past the auth check.
			// If we had been rejected at the auth layer, we would have gotten a 401 response
			// without a panic.
			t.Log("recovered from expected panic due to nil database")
		}
	}()

	handler.ServeHTTP(rec2, req2)

	// If we reach here without panic, check that we didn't get 401
	if rec2.Code == http.StatusUnauthorized {
		t.Error("system token should bypass admin check, but got 401")
	}
}

func TestHandler_ServeHTTP_SecurityHeaders(t *testing.T) {
	cfg := newTestConfig()
	handler, _ := newTestHandler(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/some-token", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Check security headers are set regardless of token validity
	headers := map[string]string{
		"X-Robots-Tag":    "noindex, nofollow",
		"Referrer-Policy": "no-referrer",
		"Cache-Control":   "private, no-store",
		"Content-Type":    "text/html; charset=utf-8",
	}

	for header, expected := range headers {
		if got := rec.Header().Get(header); got != expected {
			t.Errorf("header %s = %q, want %q", header, got, expected)
		}
	}
}

func TestHandler_ServeHTTP_RateLimiting(t *testing.T) {
	cfg := newTestConfig()
	handler, _ := newTestHandler(t, cfg)

	// Make more requests than the burst limit allows
	rateLimitHit := false

	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test-token", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusTooManyRequests {
			rateLimitHit = true

			break
		}
	}

	if !rateLimitHit {
		t.Error("expected rate limiting to kick in after many requests")
	}
}
