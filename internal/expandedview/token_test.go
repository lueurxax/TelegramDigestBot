package expandedview

import (
	"errors"
	"testing"
	"time"
)

const (
	testItemID = "550e8400-e29b-41d4-a716-446655440000"
	testSecret = "secret"
)

func requireGenerate(t *testing.T, svc *TokenService, itemID string, userID int64) string {
	t.Helper()

	token, err := svc.Generate(itemID, userID)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	return token
}

func TestTokenService_GenerateAndVerify(t *testing.T) {
	secret := "test-secret-key-12345"
	ttlHours := 24
	svc := NewTokenService(secret, ttlHours)

	itemID := testItemID
	userID := int64(12345678)

	// Generate token
	token := requireGenerate(t, svc, itemID, userID)

	if token == "" {
		t.Fatal("Generate() returned empty token")
	}

	// Verify token
	payload, err := svc.Verify(token)
	if err != nil {
		t.Fatalf("failed to verify token: %v", err)
	}

	if payload.ItemID != itemID {
		t.Errorf("Verify() ItemID = %v, want %v", payload.ItemID, itemID)
	}

	if payload.UserID != userID {
		t.Errorf("Verify() UserID = %v, want %v", payload.UserID, userID)
	}

	if time.Until(payload.ExpiresAt) < 23*time.Hour {
		t.Errorf("Verify() ExpiresAt too soon: %v", payload.ExpiresAt)
	}
}

func TestTokenService_Generate_InvalidItemID(t *testing.T) {
	svc := NewTokenService(testSecret, 24)

	_, err := svc.Generate("not-a-uuid", 12345)
	if !errors.Is(err, ErrInvalidItemID) {
		t.Errorf("expected ErrInvalidItemID, got: %v", err)
	}
}

func TestTokenService_Verify_InvalidToken(t *testing.T) {
	svc := NewTokenService(testSecret, 24)

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"invalid base64", "not-valid-base64!!!"},
		{"too short", "YWJjZA=="},
		{"wrong signature", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Verify(tt.token)
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("expected ErrInvalidToken, got: %v", err)
			}
		})
	}
}

func TestTokenService_Verify_ExpiredToken(t *testing.T) {
	// Create service with 0 TTL (immediate expiry)
	svc := &TokenService{
		secret: []byte(testSecret),
		ttl:    -time.Hour, // Already expired
	}

	token := requireGenerate(t, svc, testItemID, 12345)

	_, err := svc.Verify(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestTokenService_DifferentSecrets(t *testing.T) {
	svc1 := NewTokenService("secret1", 24)
	svc2 := NewTokenService("secret2", 24)

	token := requireGenerate(t, svc1, testItemID, 12345)

	// Token from svc1 should not verify with svc2
	_, err := svc2.Verify(token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken for different secret, got: %v", err)
	}
}
