package solr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Document ID prefixes.
const (
	prefixWeb      = "web:"
	prefixTelegram = "tg:"
	portHTTP       = ":80"
	portHTTPS      = ":443"
	schemeHTTP     = "http"
	schemeHTTPS    = "https"
)

// WebDocID generates a document ID for a web page URL.
// The ID is a SHA-256 hash of the canonicalized URL prefixed with "web:".
func WebDocID(rawURL string) string {
	canonical := canonicalizeURL(rawURL)
	hash := sha256.Sum256([]byte(canonical))

	return prefixWeb + hex.EncodeToString(hash[:16])
}

// TelegramDocID generates a document ID for a Telegram message.
// Format: "tg:{peer_id}:{message_id}"
func TelegramDocID(peerID, messageID int64) string {
	return fmt.Sprintf(prefixTelegram+"%d:%d", peerID, messageID)
}

// TelegramDisplayURL generates a display URL for a Telegram message.
// If username is available, returns a t.me link; otherwise returns a descriptive string.
func TelegramDisplayURL(username string, peerID, messageID int64) string {
	if username != "" {
		return fmt.Sprintf("https://t.me/%s/%d", username, messageID)
	}

	return fmt.Sprintf("tg://channel/%d/%d", peerID, messageID)
}

// canonicalizeURL normalizes a URL for consistent document ID generation.
// It lowercases the scheme and host, removes default ports, removes fragments,
// sorts query parameters, and removes trailing slashes from the path.
func canonicalizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// Normalize scheme
	parsed.Scheme = strings.ToLower(parsed.Scheme)

	// Normalize host
	parsed.Host = strings.ToLower(parsed.Host)

	// Remove default ports
	parsed.Host = removeDefaultPort(parsed.Host, parsed.Scheme)

	// Remove fragment
	parsed.Fragment = ""

	// Normalize path - remove trailing slash unless it's the root
	if parsed.Path != "/" && strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}

	// Sort query parameters
	if parsed.RawQuery != "" {
		query := parsed.Query()
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}

// removeDefaultPort removes default ports (80 for http, 443 for https).
func removeDefaultPort(host, scheme string) string {
	switch {
	case scheme == schemeHTTP && strings.HasSuffix(host, portHTTP):
		return strings.TrimSuffix(host, portHTTP)
	case scheme == schemeHTTPS && strings.HasSuffix(host, portHTTPS):
		return strings.TrimSuffix(host, portHTTPS)
	default:
		return host
	}
}

// ParseTelegramDocID extracts peer ID and message ID from a Telegram document ID.
// Returns (0, 0, false) if the ID is not a valid Telegram document ID.
func ParseTelegramDocID(docID string) (peerID, messageID int64, ok bool) {
	if !strings.HasPrefix(docID, prefixTelegram) {
		return 0, 0, false
	}

	parts := strings.Split(docID[len(prefixTelegram):], ":")
	if len(parts) != 2 {
		return 0, 0, false
	}

	var err error

	if _, err = fmt.Sscanf(parts[0], "%d", &peerID); err != nil {
		return 0, 0, false
	}

	if _, err = fmt.Sscanf(parts[1], "%d", &messageID); err != nil {
		return 0, 0, false
	}

	return peerID, messageID, true
}

// IsWebDocID checks if a document ID is for a web page.
func IsWebDocID(docID string) bool {
	return strings.HasPrefix(docID, prefixWeb)
}

// IsTelegramDocID checks if a document ID is for a Telegram message.
func IsTelegramDocID(docID string) bool {
	return strings.HasPrefix(docID, prefixTelegram)
}
