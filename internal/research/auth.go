package research

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	authUserIDSize    = 8
	authExpSize       = 8
	authSigSize       = 16
	authPayloadSize   = authUserIDSize + authExpSize
	authTokenSize     = authPayloadSize + authSigSize
	sessionTokenBytes = 32

	DefaultLoginTokenTTL = 10 * time.Minute
	DefaultSessionTTL    = 24 * time.Hour
)

var (
	ErrAuthTokenInvalid = errors.New("invalid auth token")
	ErrAuthTokenExpired = errors.New("auth token expired")
)

// AuthTokenPayload contains the decoded auth token data.
type AuthTokenPayload struct {
	UserID    int64
	ExpiresAt time.Time
}

// AuthTokenService signs and verifies login tokens.
type AuthTokenService struct {
	secret []byte
	ttl    time.Duration
}

// NewAuthTokenService creates a new auth token service.
func NewAuthTokenService(secret string, ttl time.Duration) *AuthTokenService {
	return &AuthTokenService{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Generate creates a signed auth token for the given user ID.
func (s *AuthTokenService) Generate(userID int64) (string, error) {
	payload := make([]byte, authPayloadSize)
	//nolint:gosec // Telegram user IDs fit in int64
	binary.BigEndian.PutUint64(payload[0:authUserIDSize], uint64(userID))

	exp := time.Now().Add(s.ttl).Unix()
	//nolint:gosec // Unix timestamps fit in uint64
	binary.BigEndian.PutUint64(payload[authUserIDSize:], uint64(exp))

	sig := s.sign(payload)

	token := make([]byte, authTokenSize)
	copy(token[:authPayloadSize], payload)
	copy(token[authPayloadSize:], sig[:authSigSize])

	return base64.URLEncoding.EncodeToString(token), nil
}

// Verify validates and decodes an auth token.
func (s *AuthTokenService) Verify(token string) (*AuthTokenPayload, error) {
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrAuthTokenInvalid
	}

	if len(data) != authTokenSize {
		return nil, ErrAuthTokenInvalid
	}

	payload := data[:authPayloadSize]
	providedSig := data[authPayloadSize:]

	expectedSig := s.sign(payload)
	if !hmac.Equal(providedSig, expectedSig[:authSigSize]) {
		return nil, ErrAuthTokenInvalid
	}

	//nolint:gosec // Telegram user IDs fit in int64
	userID := int64(binary.BigEndian.Uint64(payload[:authUserIDSize]))
	//nolint:gosec // Unix timestamps fit in int64
	exp := int64(binary.BigEndian.Uint64(payload[authUserIDSize:]))
	expiresAt := time.Unix(exp, 0)

	if time.Now().After(expiresAt) {
		return nil, ErrAuthTokenExpired
	}

	return &AuthTokenPayload{
		UserID:    userID,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *AuthTokenService) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)

	return mac.Sum(nil)
}

// GenerateSessionToken returns a random session token.
func GenerateSessionToken() (string, error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}

	return base64.URLEncoding.EncodeToString(buf), nil
}
