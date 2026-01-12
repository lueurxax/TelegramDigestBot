package telegramreader

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Pre-compiled regexes for canonicalize (avoid recompilation on every call)
var (
	urlRegex   = regexp.MustCompile(`https?://\S+`)
	spaceRegex = regexp.MustCompile(`\s+`)
)

func (r *Reader) sanitizePhone(phone string) string {
	var sb strings.Builder

	phone = strings.TrimSpace(phone)

	if strings.HasPrefix(phone, "+") {
		sb.WriteByte('+')

		phone = phone[1:]
	}

	for _, char := range phone {
		if char >= '0' && char <= '9' {
			sb.WriteRune(char)
		}
	}

	return sb.String()
}

func (r *Reader) maskPhone(phone string) string {
	if len(phone) < 7 {
		return "****"
	}

	return phone[:3] + "****" + phone[len(phone)-2:]
}

func (r *Reader) canonicalize(text string) string {
	text = strings.ToLower(text)
	// Remove URLs (using pre-compiled regex)
	text = urlRegex.ReplaceAllString(text, "")
	// Normalize whitespace (using pre-compiled regex)
	text = spaceRegex.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	hash := sha256.Sum256([]byte(text))

	return hex.EncodeToString(hash[:])
}
