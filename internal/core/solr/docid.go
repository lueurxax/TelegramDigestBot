package solr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Constants for URL canonicalization.
const (
	portHTTP  = ":80"
	portHTTPS = ":443"
)

// WebDocID generates a document ID for a web page URL.
// The ID is a full SHA-256 hash of the canonicalized URL.
func WebDocID(rawURL string) string {
	canonical := canonicalizeURL(rawURL)
	return hashToID(canonical)
}

// TelegramDocID generates a document ID for a Telegram message.
// Uses the same SHA-256 hash approach as web pages with a canonical tg:// URL.
// Format: SHA-256 hash of "tg://peer/{peer_id}/msg/{msg_id}"
func TelegramDocID(peerID, messageID int64) string {
	canonicalURL := fmt.Sprintf("tg://peer/%d/msg/%d", peerID, messageID)
	return hashToID(canonicalURL)
}

// hashToID generates a document ID from a canonical URL string.
func hashToID(canonicalURL string) string {
	hash := sha256.Sum256([]byte(canonicalURL))
	return hex.EncodeToString(hash[:])
}

// TelegramDisplayURL generates a display URL for a Telegram message.
// If username is available, returns a t.me link; otherwise returns a descriptive string.
func TelegramDisplayURL(username string, peerID, messageID int64) string {
	if username != "" {
		return fmt.Sprintf("https://t.me/%s/%d", username, messageID)
	}

	return fmt.Sprintf("tg://channel/%d/%d", peerID, messageID)
}

// CanonicalizeURL exposes the canonicalization logic for other packages.
func CanonicalizeURL(rawURL string) string {
	return canonicalizeURL(rawURL)
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
	case scheme == "http" && strings.HasSuffix(host, portHTTP):
		return strings.TrimSuffix(host, portHTTP)
	case scheme == "https" && strings.HasSuffix(host, portHTTPS):
		return strings.TrimSuffix(host, portHTTPS)
	default:
		return host
	}
}
