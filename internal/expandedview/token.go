package expandedview

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Token layout constants.
const (
	uuidSize      = 16 // UUID binary size
	userIDSize    = 8  // int64 big-endian
	expSize       = 8  // Unix timestamp big-endian
	sigSize       = 16 // Truncated HMAC-SHA256
	payloadSize   = uuidSize + userIDSize + expSize
	fullTokenSize = payloadSize + sigSize
)

// Token errors.
var (
	ErrInvalidToken  = errors.New("invalid token")
	ErrTokenExpired  = errors.New("token expired")
	ErrInvalidItemID = errors.New("invalid item id")
)

// TokenPayload contains the decoded token data.
type TokenPayload struct {
	ItemID    string
	UserID    int64
	ExpiresAt time.Time
}

// TokenService handles token generation and verification.
type TokenService struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenService creates a new token service with the given secret and TTL.
func NewTokenService(secret string, ttlHours int) *TokenService {
	return &TokenService{
		secret: []byte(secret),
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

// Generate creates a signed token for the given item and user.
func (s *TokenService) Generate(itemID string, userID int64) (string, error) {
	// Parse UUID
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return "", ErrInvalidItemID
	}

	// Build payload: item_id (16) | user_id (8) | exp (8)
	payload := make([]byte, payloadSize)
	copy(payload[0:uuidSize], uid[:])

	//nolint:gosec // User IDs from Telegram are always positive and fit in uint64
	binary.BigEndian.PutUint64(payload[uuidSize:uuidSize+userIDSize], uint64(userID))

	exp := time.Now().Add(s.ttl).Unix()

	//nolint:gosec // Unix timestamps fit safely in uint64 for foreseeable future
	binary.BigEndian.PutUint64(payload[uuidSize+userIDSize:], uint64(exp))

	// Sign
	sig := s.sign(payload)

	// Combine payload + signature
	token := make([]byte, fullTokenSize)
	copy(token[0:payloadSize], payload)
	copy(token[payloadSize:], sig[:sigSize])

	return base64.URLEncoding.EncodeToString(token), nil
}

// Verify validates and decodes a token.
func (s *TokenService) Verify(token string) (*TokenPayload, error) {
	// Decode base64
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidToken
	}

	if len(data) != fullTokenSize {
		return nil, ErrInvalidToken
	}

	// Extract payload and signature
	payload := data[0:payloadSize]
	providedSig := data[payloadSize:]

	// Verify signature
	expectedSig := s.sign(payload)
	if !hmac.Equal(providedSig, expectedSig[:sigSize]) {
		return nil, ErrInvalidToken
	}

	// Parse payload
	var uid uuid.UUID

	copy(uid[:], payload[0:uuidSize])

	//nolint:gosec // Telegram user IDs fit in int64
	userID := int64(binary.BigEndian.Uint64(payload[uuidSize : uuidSize+userIDSize]))

	//nolint:gosec // Unix timestamps fit in int64 for foreseeable future
	exp := int64(binary.BigEndian.Uint64(payload[uuidSize+userIDSize:]))
	expiresAt := time.Unix(exp, 0)

	// Check expiration
	if time.Now().After(expiresAt) {
		return nil, ErrTokenExpired
	}

	return &TokenPayload{
		ItemID:    uid.String(),
		UserID:    userID,
		ExpiresAt: expiresAt,
	}, nil
}

// sign computes HMAC-SHA256 of the payload.
func (s *TokenService) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)

	return mac.Sum(nil)
}
