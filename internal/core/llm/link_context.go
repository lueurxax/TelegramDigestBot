package llm

import (
	"strings"
	"unicode/utf8"

	linkscore "github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

const wwwPrefix = "www."

func buildLinkContextConfig(cfg *config.Config) linkscore.LinkContextConfig {
	return linkscore.LinkContextConfig{
		PrimaryMinWords:      cfg.LinkPrimaryMinWords,
		PrimaryShortMsgChars: cfg.LinkPrimaryShortMsg,
		PrimaryAllowlist:     parseDomainList(cfg.LinkPrimaryAllowlist),
		PrimaryCTATerms:      parseCSVList(cfg.LinkPrimaryCTATerms),
		PrimaryMaxLinks:      cfg.LinkPrimaryMaxLinks,
		DonationDenylist:     parseDomainList(cfg.LinkPrimaryDonationDL),
	}
}

func buildLinkContextString(cfg *config.Config, m MessageInput) string {
	if cfg == nil || len(m.ResolvedLinks) == 0 {
		return ""
	}

	if !shouldIncludeLinkContext(cfg, m.Text) {
		return ""
	}

	linkCfg := buildLinkContextConfig(cfg)
	primary, supplemental := linkscore.SelectLinkContexts(m.Text, m.PreviewText, m.ResolvedLinks, linkCfg)

	context := formatLinkContext(primary, supplemental, cfg.LinkSnippetMaxChars)
	if context != "" {
		observability.LinkContextUsedTotal.Inc()
	}

	return context
}

func shouldIncludeLinkContext(cfg *config.Config, text string) bool {
	scope := cfg.LinkEnrichmentScope
	if scope == "" {
		scope = contextTypeSummary
	}

	if strings.Contains(scope, "summary") {
		return true
	}

	shortThreshold := cfg.LinkPrimaryShortMsg
	if shortThreshold <= 0 {
		shortThreshold = 120
	}

	isShort := utf8.RuneCountInString(strings.TrimSpace(text)) < shortThreshold

	return isShort && (strings.Contains(scope, "topic") || strings.Contains(scope, "relevance"))
}

func formatLinkContext(primary, supplemental *linkscore.LinkContext, maxChars int) string {
	var sb strings.Builder

	if primary != nil {
		sb.WriteString("[PRIMARY ARTICLE]\n")
		sb.WriteString(formatLinkContextEntry(primary, maxChars))
		sb.WriteString("\n")
	}

	if supplemental != nil {
		sb.WriteString("[SUPPLEMENTAL LINK]\n")
		sb.WriteString(formatLinkContextEntry(supplemental, maxChars))
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatLinkContextEntry(ctx *linkscore.LinkContext, maxChars int) string {
	if ctx == nil {
		return ""
	}

	var sb strings.Builder

	if ctx.Domain != "" {
		sb.WriteString("Source: ")
		sb.WriteString(ctx.Domain)
		sb.WriteString("\n")
	}

	if ctx.Title != "" {
		sb.WriteString("Title: ")
		sb.WriteString(ctx.Title)
		sb.WriteString("\n")
	}

	content := ctx.Content
	if maxChars > 0 && utf8.RuneCountInString(content) > maxChars {
		runes := []rune(content)
		content = string(runes[:maxChars])
	}

	sb.WriteString("Content: ")
	sb.WriteString(content)

	return sb.String()
}

func parseDomainList(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return out
	}

	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(strings.ToLower(part))

		part = strings.TrimPrefix(part, wwwPrefix)
		if part != "" {
			out[part] = struct{}{}
		}
	}

	return out
}

func parseCSVList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}
