package pipeline

import (
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links/linkextract"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

const (
	domainImportanceBoost   = 0.05
	domainImportancePenalty = 0.05
)

func applyDomainBias(importance float32, c llm.MessageInput, s *pipelineSettings) float32 {
	if len(s.domainAllowlist) == 0 && len(s.domainDenylist) == 0 {
		return importance
	}

	domains := extractDomains(c)
	if len(domains) == 0 {
		return importance
	}

	for _, domain := range domains {
		if _, ok := s.domainDenylist[domain]; ok {
			importance -= domainImportancePenalty
			break
		}
	}

	for _, domain := range domains {
		if _, ok := s.domainAllowlist[domain]; ok {
			importance += domainImportanceBoost
			break
		}
	}

	if importance < 0 {
		return 0
	}

	if importance > 1 {
		return 1
	}

	return importance
}

func extractDomains(c llm.MessageInput) []string {
	seen := make(map[string]struct{})

	var domains []string

	for _, link := range c.ResolvedLinks {
		domain := normalizeDomain(link.Domain)
		if domain == "" {
			continue
		}

		if _, ok := seen[domain]; ok {
			continue
		}

		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}

	if len(domains) > 0 {
		return domains
	}

	for _, link := range linkextract.ExtractLinks(c.Text) {
		domain := normalizeDomain(link.Domain)
		if domain == "" {
			continue
		}

		if _, ok := seen[domain]; ok {
			continue
		}

		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}

	return domains
}

func normalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, "www.")

	return domain
}
