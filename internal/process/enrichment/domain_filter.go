package enrichment

import (
	"strings"
)

// DomainFilter handles domain allowlist/denylist filtering for evidence sources.
type DomainFilter struct {
	allowlist map[string]bool
	denylist  map[string]bool
	mode      filterMode
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
func NewDomainFilter(allowlistStr, denylistStr string) *DomainFilter {
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
		allowlist: allowlist,
		denylist:  denylist,
		mode:      mode,
	}
}

// IsAllowed checks if a domain is allowed based on the filter configuration.
func (f *DomainFilter) IsAllowed(domain string) bool {
	if domain == "" {
		return false
	}

	domain = normalizeDomain(domain)

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
