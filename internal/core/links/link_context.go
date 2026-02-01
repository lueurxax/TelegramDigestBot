package links

import (
	"strings"
	"unicode/utf8"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

type LinkContextRole string

const (
	LinkContextPrimary       LinkContextRole = "primary"
	LinkContextSupplement    LinkContextRole = "supplemental"
	defaultPrimaryMinWords                   = 200
	defaultPrimaryShortChars                 = 120
	ctaTailPositionRatio                     = 0.8 // Only check last 20% of content for CTA phrases
)

type LinkContext struct {
	Role        LinkContextRole
	URL         string
	Domain      string
	Title       string
	Description string
	Content     string
	WordCount   int
	Language    string
}

type LinkContextConfig struct {
	PrimaryMinWords      int
	PrimaryShortMsgChars int
	PrimaryAllowlist     map[string]struct{}
	PrimaryCTATerms      []string
	PrimaryMaxLinks      int
	DonationDenylist     map[string]struct{}
}

func SelectLinkContexts(messageText, previewText string, links []domain.ResolvedLink, cfg LinkContextConfig) (*LinkContext, *LinkContext) {
	if len(links) == 0 {
		return nil, nil
	}

	maxLinks := cfg.PrimaryMaxLinks
	if maxLinks <= 0 {
		maxLinks = len(links)
	}

	if cfg.PrimaryMinWords <= 0 {
		cfg.PrimaryMinWords = defaultPrimaryMinWords
	}

	if cfg.PrimaryShortMsgChars <= 0 {
		cfg.PrimaryShortMsgChars = defaultPrimaryShortChars
	}

	if maxLinks < len(links) {
		links = links[:maxLinks]
	}

	isShort := utf8.RuneCountInString(strings.TrimSpace(messageText)) < cfg.PrimaryShortMsgChars
	primaryCandidate := selectBestLinkContext(links, previewText, cfg, isShort, true)

	if primaryCandidate != nil {
		return primaryCandidate, nil
	}

	// No primary candidate, select supplemental if available.
	supplemental := selectBestLinkContext(links, previewText, cfg, isShort, false)

	return nil, supplemental
}

func selectBestLinkContext(links []domain.ResolvedLink, previewText string, cfg LinkContextConfig, isShort, requirePrimary bool) *LinkContext {
	var best *LinkContext

	bestScore := -1
	allowPreview := len(links) == 1

	for _, link := range links {
		ctx := buildLinkContext(link, previewText, allowPreview)
		if ctx == nil {
			continue
		}

		if isLinkCTA(ctx.Content, ctx.Domain, cfg) {
			continue
		}

		if requirePrimary && !isPrimaryEligible(ctx, cfg, isShort) {
			continue
		}

		score := ctx.WordCount
		if score <= 0 {
			score = len(strings.Fields(ctx.Content))
		}

		if score > bestScore {
			best = ctx
			bestScore = score
		}
	}

	if best == nil {
		return nil
	}

	if requirePrimary {
		best.Role = LinkContextPrimary
	} else {
		best.Role = LinkContextSupplement
	}

	return best
}

func buildLinkContext(link domain.ResolvedLink, previewText string, allowPreview bool) *LinkContext {
	content := strings.TrimSpace(link.Content)
	if content == "" {
		combined := strings.TrimSpace(strings.TrimSpace(link.Title) + ". " + strings.TrimSpace(link.Description))
		content = strings.TrimSpace(combined)
	}

	if content == "" && allowPreview {
		content = strings.TrimSpace(previewText)
	}

	if content == "" {
		return nil
	}

	wordCount := link.WordCount
	if wordCount <= 0 {
		wordCount = len(strings.Fields(content))
	}

	return &LinkContext{
		URL:         link.URL,
		Domain:      strings.ToLower(strings.TrimSpace(link.Domain)),
		Title:       link.Title,
		Description: link.Description,
		Content:     content,
		WordCount:   wordCount,
		Language:    link.Language,
	}
}

func isPrimaryEligible(ctx *LinkContext, cfg LinkContextConfig, isShort bool) bool {
	if isShort {
		return true
	}

	if ctx.WordCount >= cfg.PrimaryMinWords {
		return true
	}

	if _, ok := cfg.PrimaryAllowlist[strings.ToLower(ctx.Domain)]; ok {
		return true
	}

	return false
}

func isLinkCTA(content, domain string, cfg LinkContextConfig) bool {
	if domain != "" {
		if _, ok := cfg.DonationDenylist[strings.ToLower(domain)]; ok {
			return true
		}
	}

	content = strings.ToLower(strings.TrimSpace(content))
	if content == "" || len(cfg.PrimaryCTATerms) == 0 {
		return false
	}

	runes := []rune(content)
	start := int(float64(len(runes)) * ctaTailPositionRatio)

	if start < 0 {
		start = 0
	}

	tail := string(runes[start:])

	for _, term := range cfg.PrimaryCTATerms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}

		if strings.Contains(tail, term) {
			return true
		}
	}

	return false
}
