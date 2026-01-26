package expandedview

import (
	"testing"
	"time"
)

func TestTokenService_GenerateAndVerify(t *testing.T) {
	secret := "test-secret-key-12345"
	ttlHours := 24
	svc := NewTokenService(secret, ttlHours)

	itemID := "550e8400-e29b-41d4-a716-446655440000"
	userID := int64(12345678)

	// Generate token
	token, err := svc.Generate(itemID, userID)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if token == "" {
		t.Fatal("Generate() returned empty token")
	}

	// Verify token
	payload, err := svc.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
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
	svc := NewTokenService("secret", 24)

	_, err := svc.Generate("not-a-uuid", 12345)
	if err != ErrInvalidItemID {
		t.Errorf("Generate() error = %v, want %v", err, ErrInvalidItemID)
	}
}

func TestTokenService_Verify_InvalidToken(t *testing.T) {
	svc := NewTokenService("secret", 24)

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
			if err != ErrInvalidToken {
				t.Errorf("Verify() error = %v, want %v", err, ErrInvalidToken)
			}
		})
	}
}

func TestTokenService_Verify_ExpiredToken(t *testing.T) {
	// Create service with 0 TTL (immediate expiry)
	svc := &TokenService{
		secret: []byte("secret"),
		ttl:    -time.Hour, // Already expired
	}

	itemID := "550e8400-e29b-41d4-a716-446655440000"
	token, err := svc.Generate(itemID, 12345)

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	_, err = svc.Verify(token)
	if err != ErrTokenExpired {
		t.Errorf("Verify() error = %v, want %v", err, ErrTokenExpired)
	}
}

func TestTokenService_DifferentSecrets(t *testing.T) {
	svc1 := NewTokenService("secret1", 24)
	svc2 := NewTokenService("secret2", 24)

	itemID := "550e8400-e29b-41d4-a716-446655440000"
	token, err := svc1.Generate(itemID, 12345)

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Token from svc1 should not verify with svc2
	_, err = svc2.Verify(token)
	if err != ErrInvalidToken {
		t.Errorf("Verify() with different secret: error = %v, want %v", err, ErrInvalidToken)
	}
}
