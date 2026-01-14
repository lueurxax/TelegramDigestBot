package bot

import (
	"fmt"
	"html"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
)

// SplitHTML splits an HTML string into multiple parts, each within the specified limit.
// The limit applies to the text length *after* HTML entities are parsed, consistent with Telegram's API.
// It tries to split at line breaks if possible, otherwise splits at any character.
// It ensures that all supported HTML tags are properly handled and closed/reopened across parts.
// Item boundary markers are stripped from the output before returning.
func SplitHTML(text string, limit int) []string {
	parts := htmlutils.SplitHTML(text, limit)

	// Strip item boundary markers from each part
	for i, part := range parts {
		parts[i] = htmlutils.StripItemMarkers(part)
	}

	return parts
}

// FormatLink generates a Telegram message link for public or private channels.
func FormatLink(username string, peerID, msgID int64, label string) string {
	if username != "" {
		return fmt.Sprintf("<a href=\"https://t.me/%s/%d\">%s</a>", html.EscapeString(username), msgID, html.EscapeString(label))
	}
	// For private channels or channels without username
	return fmt.Sprintf("<a href=\"https://t.me/c/%d/%d\">%s</a>", peerID, msgID, html.EscapeString(label))
}
