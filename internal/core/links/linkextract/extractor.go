package linkextract

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type LinkType string

const (
	LinkTypeWeb      LinkType = "web"
	LinkTypeTelegram LinkType = "telegram"
	LinkTypeBlocked  LinkType = "blocked"
)

type Link struct {
	URL      string
	Domain   string
	Type     LinkType
	Position int

	// Telegram-specific
	TelegramType string // "post", "channel", "invite"
	Username     string
	ChannelID    int64
	MessageID    int64
}

var (
	urlRegex      = regexp.MustCompile(`https?://[^\s<>"{}|\\^\x60\[\]]+`)
	tgPostRegex   = regexp.MustCompile(`t\.me/(?:c/(\d+)|([a-zA-Z][a-zA-Z0-9_]{3,}))/(\d+)`)
	tgInviteRegex = regexp.MustCompile(`t\.me/(?:\+|joinchat/)([a-zA-Z0-9_-]+)`)
	mentionRegex  = regexp.MustCompile(`@([a-zA-Z][a-zA-Z0-9_]{3,31})`)
)

var blockedDomains = map[string]bool{
	"twitter.com":   true,
	"x.com":         true,
	"instagram.com": true,
	"facebook.com":  true,
	"linkedin.com":  true,
	"tiktok.com":    true,
}

func ExtractLinks(text string) []Link {
	matches := urlRegex.FindAllStringIndex(text, -1)

	var links []Link

	seen := make(map[string]bool)

	for _, match := range matches {
		rawURL := strings.TrimRight(text[match[0]:match[1]], ".,;:!?)")
		normalized := normalizeURL(rawURL)

		if normalized == "" {
			continue
		}

		if seen[normalized] {
			continue
		}

		seen[normalized] = true

		domain := extractDomain(normalized)
		link := Link{
			URL:      normalized,
			Domain:   domain,
			Position: match[0],
		}

		// Classify link type
		if strings.Contains(normalized, "t.me/") {
			link.Type = LinkTypeTelegram
			parseTelegramLink(&link)
		} else if blockedDomains[domain] {
			link.Type = LinkTypeBlocked
		} else {
			link.Type = LinkTypeWeb
		}

		links = append(links, link)
	}

	return links
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return strings.ToLower(u.Host)
}

func parseTelegramLink(link *Link) {
	if matches := tgPostRegex.FindStringSubmatch(link.URL); matches != nil {
		link.TelegramType = "post"
		if matches[1] != "" {
			link.ChannelID, _ = strconv.ParseInt(matches[1], 10, 64) //nolint:errcheck // regex guarantees numeric input
		} else {
			link.Username = matches[2]
		}

		link.MessageID, _ = strconv.ParseInt(matches[3], 10, 64) //nolint:errcheck // regex guarantees numeric input
	} else if tgInviteRegex.MatchString(link.URL) {
		link.TelegramType = "invite"
	} else {
		link.TelegramType = "channel"
	}
}

// ExtractMentions extracts @username mentions from text
func ExtractMentions(text string) []string {
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)

	var mentions []string

	for _, match := range matches {
		if len(match) > 1 {
			username := match[1]
			if !seen[username] {
				seen[username] = true
				mentions = append(mentions, username)
			}
		}
	}

	return mentions
}
