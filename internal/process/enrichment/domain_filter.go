package enrichment

import (
	"strings"
)

// socialMediaDomains contains domains of social media platforms that should be
// automatically denied for enrichment. These platforms:
// 1. Use JavaScript rendering - content not available in initial HTML
// 2. Are primary sources, not corroborating evidence sources
// 3. Often have short descriptions that don't yield useful claims
var socialMediaDomains = map[string]bool{
	// Twitter/X
	"twitter.com": true,
	"x.com":       true,
	"t.co":        true,

	// Facebook/Meta
	"facebook.com":  true,
	"fb.com":        true,
	"fb.me":         true,
	"instagram.com": true,
	"threads.net":   true,

	// Video platforms
	"youtube.com": true,
	"youtu.be":    true,
	"tiktok.com":  true,
	"vimeo.com":   true,
	"twitch.tv":   true,

	// Professional/Business social
	"linkedin.com": true,

	// Messaging platforms
	"telegram.org": true,
	"t.me":         true,
	"discord.com":  true,
	"discord.gg":   true,
	"whatsapp.com": true,

	// Other social platforms
	"reddit.com":    true,
	"pinterest.com": true,
	"tumblr.com":    true,
	"snapchat.com":  true,
	"weibo.com":     true,
	"vk.com":        true,
	"ok.ru":         true,

	// URL shorteners (can't extract content)
	"bit.ly":      true,
	"goo.gl":      true,
	"tinyurl.com": true,
	"ow.ly":       true,
	"buff.ly":     true,
	"is.gd":       true,
	"v.gd":        true,
	"cutt.ly":     true,
	"rebrand.ly":  true,
}

// DomainFilter handles domain allowlist/denylist filtering for evidence sources.
type DomainFilter struct {
	allowlist       map[string]bool
	denylist        map[string]bool
	mode            filterMode
	skipSocialMedia bool
}

type filterMode int

const (
	filterModeAllowAll  filterMode = iota // No filtering (empty lists)
	filterModeAllowlist                   // Only allow domains in allowlist
	filterModeDenylist                    // Allow all except denylist
)

// NewDomainFilter creates a new domain filter from allowlist and denylist strings.
// Domains should be comma-separated.
// If allowlist is provided, only those domains are allowed.
// If only denylist is provided, all domains except those are allowed.
// If both are empty, all domains are allowed.
// Social media domains are always denied by default.
func NewDomainFilter(allowlistStr, denylistStr string) *DomainFilter {
	return NewDomainFilterWithOptions(allowlistStr, denylistStr, true)
}

// NewDomainFilterWithOptions creates a domain filter with configurable options.
func NewDomainFilterWithOptions(allowlistStr, denylistStr string, skipSocialMedia bool) *DomainFilter {
	allowlist := parseDomainList(allowlistStr)
	denylist := parseDomainList(denylistStr)

	var mode filterMode

	switch {
	case len(allowlist) > 0:
		mode = filterModeAllowlist
	case len(denylist) > 0:
		mode = filterModeDenylist
	default:
		mode = filterModeAllowAll
	}

	return &DomainFilter{
		allowlist:       allowlist,
		denylist:        denylist,
		mode:            mode,
		skipSocialMedia: skipSocialMedia,
	}
}

// IsAllowed checks if a domain is allowed based on the filter configuration.
func (f *DomainFilter) IsAllowed(domain string) bool {
	if domain == "" {
		return false
	}

	domain = normalizeDomain(domain)

	// Always check social media domains first (unless disabled)
	if f.skipSocialMedia && f.isSocialMediaDomain(domain) {
		return false
	}

	switch f.mode {
	case filterModeAllowAll:
		return true
	case filterModeAllowlist:
		return f.matchesList(domain, f.allowlist)
	case filterModeDenylist:
		return !f.matchesList(domain, f.denylist)
	default:
		return true
	}
}

// isSocialMediaDomain checks if a domain is a social media platform.
func (f *DomainFilter) isSocialMediaDomain(domain string) bool {
	// Exact match
	if socialMediaDomains[domain] {
		return true
	}

	// Suffix match for subdomains (e.g., mobile.twitter.com)
	for d := range socialMediaDomains {
		if strings.HasSuffix(domain, "."+d) {
			return true
		}
	}

	return false
}

// matchesList checks if a domain matches any entry in the list.
// Supports exact match and suffix match (e.g., "example.com" matches "sub.example.com").
func (f *DomainFilter) matchesList(domain string, list map[string]bool) bool {
	// Exact match
	if list[domain] {
		return true
	}

	// Suffix match for subdomains
	for d := range list {
		if strings.HasSuffix(domain, "."+d) {
			return true
		}
	}

	return false
}

// parseDomainList parses a comma-separated list of domains into a map.
func parseDomainList(s string) map[string]bool {
	if s == "" {
		return nil
	}

	result := make(map[string]bool)

	for _, domain := range strings.Split(s, ",") {
		domain = normalizeDomain(domain)
		if domain != "" {
			result[domain] = true
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// normalizeDomain normalizes a domain for comparison.
func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.ToLower(domain)

	// Remove protocol if present
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")

	// Remove trailing slash
	domain = strings.TrimSuffix(domain, "/")

	// Remove www. prefix
	domain = strings.TrimPrefix(domain, "www.")

	return domain
}
