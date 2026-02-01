package links

import (
	"net/url"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

const wwwPrefix = "www."

// TrustedCanonical returns the canonical URL and domain if it passes trust checks.
// It rejects known aggregators and homepage/root URLs.
func TrustedCanonical(link domain.ResolvedLink, allowlist, trusted, denylist map[string]struct{}) (string, string) {
	canonical := strings.TrimSpace(link.CanonicalURL)
	if canonical == "" {
		return "", ""
	}

	parsed, err := url.Parse(canonical)
	if err != nil {
		return "", ""
	}

	domainName := normalizeDomain(parsed.Host)
	if domainName == "" {
		return "", ""
	}

	if _, ok := denylist[domainName]; ok {
		return "", ""
	}

	path := strings.TrimSpace(parsed.EscapedPath())
	if path == "" || path == "/" {
		return "", ""
	}

	originDomain := normalizeDomain(link.Domain)

	if domainName != originDomain {
		if _, ok := allowlist[domainName]; !ok {
			if _, ok := trusted[domainName]; !ok {
				return "", ""
			}
		}
	}

	return canonical, domainName
}

func normalizeDomain(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, wwwPrefix)

	return host
}
