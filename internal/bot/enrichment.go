package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	enrichment "github.com/lueurxax/telegram-digest-bot/internal/process/enrichment"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	// DefaultEnrichmentRecentHours is the default lookback for recent evidence counts.
	DefaultEnrichmentRecentHours = 24

	// Setting keys for enrichment domain lists.
	SettingEnrichmentAllowDomains = "enrichment_allow_domains"
	SettingEnrichmentDenyDomains  = "enrichment_deny_domains"

	// Subcommand names.
	enrichmentSubCmdDomains = "domains"
	enrichmentSubCmdHealth  = "health"
	enrichmentSubCmdDebug   = "debug"
	enrichmentSubCmdHelp    = "help"

	// Domain list types.
	domainTypeAllow = "allow"
	domainTypeDeny  = "deny"

	enrichmentDebugDefaultLimit = 5
	enrichmentDebugMaxLimit     = 20
	enrichmentDebugMinQueryLen  = 3
	enrichmentDebugSummaryLimit = 280
	enrichmentDebugTextLimit    = 420
)

type enrichmentStats struct {
	queueStats    []db.EnrichmentQueueStat
	cacheCount    int
	totalMatches  int
	recentMatches int
	dailyUsage    int
	monthlyUsage  int
}

func (b *Bot) handleEnrichmentNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.handleEnrichmentStatus(ctx, msg)

		return
	}

	subcommand := strings.ToLower(args[0])

	switch subcommand {
	case enrichmentSubCmdDomains:
		b.handleEnrichmentDomains(ctx, msg, args[1:])
	case enrichmentSubCmdHealth:
		b.handleEnrichmentHealth(ctx, msg)
	case enrichmentSubCmdDebug:
		b.handleEnrichmentDebug(ctx, msg, args[1:])
	case enrichmentSubCmdHelp:
		b.reply(msg, enrichmentHelpMessage())
	default:
		b.reply(msg, fmt.Sprintf("Unknown subcommand: <code>%s</code>\n\nUse <code>/enrichment help</code> for available commands.", html.EscapeString(subcommand)))
	}
}

func enrichmentHelpMessage() string {
	return `📖 <b>Enrichment Commands</b>

<b>Status:</b>
• <code>/enrichment</code> - Show enrichment status
• <code>/enrichment health</code> - Provider health checks
• <code>/enrichment debug &lt;text|item_id&gt; [limit]</code> - Search items or show debug detail

<b>Domain Management:</b>
• <code>/enrichment domains</code> - List all domains
• <code>/enrichment domains allow</code> - List allowlist
• <code>/enrichment domains allow &lt;domain&gt;</code> - Add to allowlist
• <code>/enrichment domains allow remove &lt;domain&gt;</code> - Remove from allowlist
• <code>/enrichment domains deny</code> - List denylist
• <code>/enrichment domains deny &lt;domain&gt;</code> - Add to denylist
• <code>/enrichment domains deny remove &lt;domain&gt;</code> - Remove from denylist
• <code>/enrichment domains clear</code> - Clear all domain lists

<b>Retry Failed Items:</b>
Use <code>/system errors</code> to view and <code>/retry enrichment confirm</code> to requeue.

<b>Notes:</b>
• Allowlist mode: only listed domains are searched
• Denylist mode: listed domains are excluded
• If both are set, allowlist takes precedence`
}

type providerHealth struct {
	name   string
	status string
	emoji  string
	detail string
}

func (b *Bot) handleEnrichmentHealth(ctx context.Context, msg *tgbotapi.Message) {
	health := b.fetchEnrichmentProviderHealth(ctx)

	var sb strings.Builder

	sb.WriteString("🩺 <b>Enrichment Provider Health</b>\n\n")

	statusLabel := StatusDisabled
	if b.cfg.EnrichmentEnabled {
		statusLabel = StatusEnabled
	}

	sb.WriteString(fmt.Sprintf("• <b>Enrichment:</b> %s\n\n", statusLabel))

	for _, entry := range health {
		fmt.Fprintf(&sb, "• %s <b>%s</b>: %s", entry.emoji, html.EscapeString(entry.name), html.EscapeString(entry.status))

		if entry.detail != "" {
			fmt.Fprintf(&sb, " — %s", html.EscapeString(entry.detail))
		}

		sb.WriteString("\n")
	}

	sb.WriteString("\n<i>Notes: external APIs are marked as “configured” to avoid consuming paid quotas.</i>")

	b.reply(msg, sb.String())
}

func (b *Bot) fetchEnrichmentProviderHealth(ctx context.Context) []providerHealth {
	selection := parseProviderSelection(b.cfg.EnrichmentProviders)

	results := []providerHealth{
		b.checkYaCyHealth(ctx, selection),
		b.checkSearxNGHealth(ctx, selection),
		b.checkGDELTHealth(ctx, selection),
		b.checkEventRegistryHealth(ctx, selection),
		b.checkNewsAPIHealth(ctx, selection),
		b.checkOpenSearchHealth(ctx, selection),
	}

	return results
}

func (b *Bot) checkYaCyHealth(ctx context.Context, selection map[string]bool) providerHealth {
	selected := isProviderSelected(selection, string(enrichment.ProviderYaCy))
	if !selected {
		return providerHealth{name: "YaCy", status: "skipped", emoji: "⚪", detail: "not in ENRICHMENT_PROVIDERS"}
	}

	if !b.cfg.YaCyEnabled {
		return providerHealth{name: "YaCy", status: "disabled", emoji: "⚪"}
	}

	if b.cfg.YaCyBaseURL == "" {
		return providerHealth{name: "YaCy", status: "misconfigured", emoji: "⚠️", detail: "missing base URL"}
	}

	provider := enrichment.NewYaCyProvider(enrichment.YaCyConfig{
		Enabled:  true,
		BaseURL:  b.cfg.YaCyBaseURL,
		Timeout:  b.cfg.YaCyTimeout,
		Username: b.cfg.YaCyUser,
		Password: b.cfg.YaCyPassword,
		Resource: b.cfg.YaCyResource,
	})

	if provider.IsAvailable(ctx) {
		return providerHealth{name: "YaCy", status: "ok", emoji: "✅", detail: b.cfg.YaCyBaseURL}
	}

	return providerHealth{name: "YaCy", status: "unreachable", emoji: "❌", detail: b.cfg.YaCyBaseURL}
}

func (b *Bot) checkSearxNGHealth(ctx context.Context, selection map[string]bool) providerHealth {
	selected := isProviderSelected(selection, string(enrichment.ProviderSearxNG))
	if !selected {
		return providerHealth{name: "SearxNG", status: "skipped", emoji: "⚪", detail: "not in ENRICHMENT_PROVIDERS"}
	}

	if !b.cfg.SearxNGEnabled {
		return providerHealth{name: "SearxNG", status: "disabled", emoji: "⚪"}
	}

	if b.cfg.SearxNGBaseURL == "" {
		return providerHealth{name: "SearxNG", status: "misconfigured", emoji: "⚠️", detail: "missing base URL"}
	}

	provider := enrichment.NewSearxNGProvider(enrichment.SearxNGConfig{
		Enabled: true,
		BaseURL: b.cfg.SearxNGBaseURL,
		Timeout: b.cfg.SearxNGTimeout,
	})

	if provider.IsAvailable(ctx) {
		return providerHealth{name: "SearxNG", status: "ok", emoji: "✅", detail: b.cfg.SearxNGBaseURL}
	}

	return providerHealth{name: "SearxNG", status: "unreachable", emoji: "❌", detail: b.cfg.SearxNGBaseURL}
}

func (b *Bot) checkGDELTHealth(ctx context.Context, selection map[string]bool) providerHealth {
	selected := isProviderSelected(selection, string(enrichment.ProviderGDELT))
	if !selected {
		return providerHealth{name: "GDELT", status: "skipped", emoji: "⚪", detail: "not in ENRICHMENT_PROVIDERS"}
	}

	if !b.cfg.GDELTEnabled {
		return providerHealth{name: "GDELT", status: "disabled", emoji: "⚪"}
	}

	return providerHealth{name: "GDELT", status: "configured", emoji: "🟡"}
}

func (b *Bot) checkEventRegistryHealth(ctx context.Context, selection map[string]bool) providerHealth {
	selected := isProviderSelected(selection, string(enrichment.ProviderEventRegistry))
	if !selected {
		return providerHealth{name: "Event Registry", status: "skipped", emoji: "⚪", detail: "not in ENRICHMENT_PROVIDERS"}
	}

	if !b.cfg.EventRegistryEnabled {
		return providerHealth{name: "Event Registry", status: "disabled", emoji: "⚪"}
	}

	if b.cfg.EventRegistryAPIKey == "" {
		return providerHealth{name: "Event Registry", status: "misconfigured", emoji: "⚠️", detail: "missing API key"}
	}

	return providerHealth{name: "Event Registry", status: "configured", emoji: "🟡"}
}

func (b *Bot) checkNewsAPIHealth(ctx context.Context, selection map[string]bool) providerHealth {
	selected := isProviderSelected(selection, string(enrichment.ProviderNewsAPI))
	if !selected {
		return providerHealth{name: "NewsAPI", status: "skipped", emoji: "⚪", detail: "not in ENRICHMENT_PROVIDERS"}
	}

	if !b.cfg.NewsAPIEnabled {
		return providerHealth{name: "NewsAPI", status: "disabled", emoji: "⚪"}
	}

	if b.cfg.NewsAPIKey == "" {
		return providerHealth{name: "NewsAPI", status: "misconfigured", emoji: "⚠️", detail: "missing API key"}
	}

	return providerHealth{name: "NewsAPI", status: "configured", emoji: "🟡"}
}

func (b *Bot) checkOpenSearchHealth(ctx context.Context, selection map[string]bool) providerHealth {
	selected := isProviderSelected(selection, string(enrichment.ProviderOpenSearch))
	if !selected {
		return providerHealth{name: "OpenSearch", status: "skipped", emoji: "⚪", detail: "not in ENRICHMENT_PROVIDERS"}
	}

	if !b.cfg.OpenSearchEnabled {
		return providerHealth{name: "OpenSearch", status: "disabled", emoji: "⚪"}
	}

	if b.cfg.OpenSearchBaseURL == "" {
		return providerHealth{name: "OpenSearch", status: "misconfigured", emoji: "⚠️", detail: "missing base URL"}
	}

	provider := enrichment.NewOpenSearchProvider(enrichment.OpenSearchConfig{
		Enabled: true,
		BaseURL: b.cfg.OpenSearchBaseURL,
		Index:   b.cfg.OpenSearchIndex,
		Timeout: b.cfg.OpenSearchTimeout,
	})

	if provider.IsAvailable(ctx) {
		return providerHealth{name: "OpenSearch", status: "ok", emoji: "✅", detail: b.cfg.OpenSearchBaseURL}
	}

	return providerHealth{name: "OpenSearch", status: "unreachable", emoji: "❌", detail: b.cfg.OpenSearchBaseURL}
}

func parseProviderSelection(raw string) map[string]bool {
	selection := make(map[string]bool)

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return selection
	}

	for _, entry := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(entry))
		if name == "" {
			continue
		}

		selection[name] = true
	}

	return selection
}

func isProviderSelected(selection map[string]bool, name string) bool {
	if len(selection) == 0 {
		return true
	}

	return selection[strings.ToLower(name)]
}

func (b *Bot) handleEnrichmentStatus(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.fetchEnrichmentStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, b.renderEnrichmentStatus(stats))
}

func (b *Bot) handleEnrichmentDomains(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) == 0 {
		b.showEnrichmentDomainsSummary(ctx, msg)

		return
	}

	listType := strings.ToLower(args[0])

	switch listType {
	case domainTypeAllow:
		b.handleEnrichmentDomainList(ctx, msg, args[1:], SettingEnrichmentAllowDomains, domainTypeAllow)
	case domainTypeDeny:
		b.handleEnrichmentDomainList(ctx, msg, args[1:], SettingEnrichmentDenyDomains, domainTypeDeny)
	case SubCmdClear:
		b.clearEnrichmentDomains(ctx, msg)
	default:
		b.reply(msg, fmt.Sprintf("Unknown domain list type: <code>%s</code>\n\nUse <code>allow</code> or <code>deny</code>.", html.EscapeString(listType)))
	}
}

func (b *Bot) showEnrichmentDomainsSummary(ctx context.Context, msg *tgbotapi.Message) {
	allowDomains := b.getEnrichmentDomainList(ctx, SettingEnrichmentAllowDomains)
	denyDomains := b.getEnrichmentDomainList(ctx, SettingEnrichmentDenyDomains)

	var sb strings.Builder

	sb.WriteString("<b>Enrichment Domain Filters</b>\n\n")

	if len(allowDomains) == 0 && len(denyDomains) == 0 {
		sb.WriteString("No domain filters configured.\n")
		sb.WriteString("Using config defaults if set.\n\n")
	}

	if len(allowDomains) > 0 {
		fmt.Fprintf(&sb, "<b>Allowlist</b> (<code>%d</code> domains):\n<code>%s</code>\n\n",
			len(allowDomains), html.EscapeString(strings.Join(allowDomains, ", ")))
	}

	if len(denyDomains) > 0 {
		fmt.Fprintf(&sb, "<b>Denylist</b> (<code>%d</code> domains):\n<code>%s</code>\n\n",
			len(denyDomains), html.EscapeString(strings.Join(denyDomains, ", ")))
	}

	sb.WriteString("Use <code>/enrichment domains allow|deny</code> to manage.")

	b.reply(msg, sb.String())
}

func (b *Bot) handleEnrichmentDomainList(ctx context.Context, msg *tgbotapi.Message, args []string, settingKey, listType string) {
	domains := b.getEnrichmentDomainList(ctx, settingKey)

	if len(args) == 0 {
		b.showEnrichmentDomainList(msg, domains, listType)

		return
	}

	action := strings.ToLower(args[0])
	updated, ok := b.processEnrichmentDomainAction(msg, listType, action, args[1:], domains)

	if !ok {
		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, settingKey, updated, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving %s domains: %s", html.EscapeString(listType), html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Enrichment %s domains updated. Total: <code>%d</code>.", html.EscapeString(listType), len(updated)))
}

func (b *Bot) showEnrichmentDomainList(msg *tgbotapi.Message, domains []string, listType string) {
	if len(domains) == 0 {
		b.reply(msg, fmt.Sprintf("📋 No enrichment %s domains configured.", html.EscapeString(listType)))

		return
	}

	b.reply(msg, fmt.Sprintf("📋 <b>Enrichment %s domains:</b>\n<code>%s</code>\n\nUsage: <code>/enrichment domains %s &lt;domain&gt;</code> | <code>/enrichment domains %s remove &lt;domain&gt;</code>",
		html.EscapeString(listType),
		html.EscapeString(strings.Join(domains, ", ")),
		html.EscapeString(listType),
		html.EscapeString(listType)))
}

func (b *Bot) processEnrichmentDomainAction(msg *tgbotapi.Message, listType, action string, args []string, domains []string) ([]string, bool) {
	switch action {
	case CmdAdd:
		return b.addEnrichmentDomain(msg, listType, args, domains)
	case CmdRemove:
		return b.removeEnrichmentDomain(msg, listType, args, domains)
	case SubCmdClear:
		return []string{}, true
	default:
		// Treat unknown action as a domain to add
		allArgs := append([]string{action}, args...)

		return b.addEnrichmentDomain(msg, listType, allArgs, domains)
	}
}

func (b *Bot) addEnrichmentDomain(msg *tgbotapi.Message, listType string, args []string, domains []string) ([]string, bool) {
	if len(args) == 0 {
		b.reply(msg, fmt.Sprintf("Usage: <code>/enrichment domains %s &lt;domain&gt;</code>", html.EscapeString(listType)))

		return nil, false
	}

	domain := normalizeDomain(args[0])
	if domain == "" {
		b.reply(msg, "❌ Invalid domain.")

		return nil, false
	}

	for _, existing := range domains {
		if existing == domain {
			b.reply(msg, fmt.Sprintf("❌ Domain <code>%s</code> already exists in %s list.", html.EscapeString(domain), html.EscapeString(listType)))

			return nil, false
		}
	}

	return append(domains, domain), true
}

func (b *Bot) removeEnrichmentDomain(msg *tgbotapi.Message, listType string, args []string, domains []string) ([]string, bool) {
	if len(args) == 0 {
		b.reply(msg, fmt.Sprintf("Usage: <code>/enrichment domains %s remove &lt;domain&gt;</code>", html.EscapeString(listType)))

		return nil, false
	}

	domain := normalizeDomain(args[0])
	found := false
	updated := make([]string, 0, len(domains))

	for _, d := range domains {
		if d == domain {
			found = true

			continue
		}

		updated = append(updated, d)
	}

	if !found {
		b.reply(msg, fmt.Sprintf("❌ Domain <code>%s</code> not found in %s list.", html.EscapeString(domain), html.EscapeString(listType)))

		return nil, false
	}

	return updated, true
}

func (b *Bot) clearEnrichmentDomains(ctx context.Context, msg *tgbotapi.Message) {
	if err := b.database.SaveSettingWithHistory(ctx, SettingEnrichmentAllowDomains, []string{}, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error clearing allow domains: %s", html.EscapeString(err.Error())))

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, SettingEnrichmentDenyDomains, []string{}, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error clearing deny domains: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, "✅ All enrichment domain filters cleared.")
}

func (b *Bot) getEnrichmentDomainList(ctx context.Context, settingKey string) []string {
	var domains []string

	if err := b.database.GetSetting(ctx, settingKey, &domains); err != nil {
		return []string{}
	}

	return domains
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "www.")
	domain = strings.TrimSuffix(domain, "/")

	return domain
}

func (b *Bot) fetchEnrichmentStats(ctx context.Context) (*enrichmentStats, error) {
	queueStats, err := b.database.GetEnrichmentQueueStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching enrichment queue stats: %w", err)
	}

	cacheCount, err := b.database.CountEvidenceSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching evidence source count: %w", err)
	}

	totalMatches, err := b.database.CountItemEvidence(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching item evidence count: %w", err)
	}

	since := time.Now().Add(-time.Duration(DefaultEnrichmentRecentHours) * time.Hour)

	recentMatches, err := b.database.CountItemEvidenceSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("counting recent item evidence: %w", err)
	}

	dailyUsage, monthlyUsage, err := b.database.GetEnrichmentUsageStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching enrichment usage stats: %w", err)
	}

	return &enrichmentStats{
		queueStats:    queueStats,
		cacheCount:    cacheCount,
		totalMatches:  totalMatches,
		recentMatches: recentMatches,
		dailyUsage:    dailyUsage,
		monthlyUsage:  monthlyUsage,
	}, nil
}

func (b *Bot) renderEnrichmentStatus(stats *enrichmentStats) string {
	counts := map[string]int{
		db.EnrichmentStatusPending:    0,
		db.EnrichmentStatusProcessing: 0,
		db.EnrichmentStatusDone:       0,
		db.EnrichmentStatusError:      0,
	}

	for _, entry := range stats.queueStats {
		counts[entry.Status] = entry.Count
	}

	statusLabel := StatusDisabled
	if b.cfg.EnrichmentEnabled {
		statusLabel = StatusEnabled
	}

	var sb strings.Builder

	sb.WriteString("<b>Source Enrichment Status</b>\n\n")
	fmt.Fprintf(&sb, "Status: %s\n", statusLabel)

	b.renderEnrichmentProviders(&sb)

	sb.WriteString("\nEnrichment Queue:\n")
	fmt.Fprintf(&sb, "  pending: <code>%d</code>\n", counts[db.EnrichmentStatusPending])
	fmt.Fprintf(&sb, "  processing: <code>%d</code>\n", counts[db.EnrichmentStatusProcessing])
	fmt.Fprintf(&sb, "  done: <code>%d</code>\n", counts[db.EnrichmentStatusDone])
	fmt.Fprintf(&sb, "  error: <code>%d</code>\n", counts[db.EnrichmentStatusError])
	fmt.Fprintf(&sb, "\nEvidence cache: <code>%d</code>\n", stats.cacheCount)
	fmt.Fprintf(&sb, "Evidence (last %d hours): <code>%d</code>\n", DefaultEnrichmentRecentHours, stats.recentMatches)
	fmt.Fprintf(&sb, "Evidence total: <code>%d</code>\n", stats.totalMatches)

	b.renderBudgetUsage(&sb, stats)

	return sb.String()
}

func (b *Bot) renderBudgetUsage(sb *strings.Builder, stats *enrichmentStats) {
	dailyLimit := b.cfg.EnrichmentDailyLimit
	monthlyLimit := b.cfg.EnrichmentMonthlyLimit

	if dailyLimit <= 0 && monthlyLimit <= 0 {
		return
	}

	sb.WriteString("\nBudget Usage:\n")

	if dailyLimit > 0 {
		fmt.Fprintf(sb, "  today: <code>%d/%d</code>\n", stats.dailyUsage, dailyLimit)
	} else {
		fmt.Fprintf(sb, "  today: <code>%d</code>\n", stats.dailyUsage)
	}

	if monthlyLimit > 0 {
		fmt.Fprintf(sb, "  this month: <code>%d/%d</code>\n", stats.monthlyUsage, monthlyLimit)
	} else {
		fmt.Fprintf(sb, "  this month: <code>%d</code>\n", stats.monthlyUsage)
	}
}

func (b *Bot) renderEnrichmentProviders(sb *strings.Builder) {
	sb.WriteString("\nProviders:\n")

	yacyStatus := StatusDisabled
	if b.cfg.YaCyEnabled && b.cfg.YaCyBaseURL != "" {
		yacyStatus = StatusEnabled
	}

	fmt.Fprintf(sb, "  YaCy: %s\n", yacyStatus)

	gdeltStatus := StatusDisabled
	if b.cfg.GDELTEnabled {
		gdeltStatus = StatusEnabled
	}

	fmt.Fprintf(sb, "  GDELT: %s\n", gdeltStatus)
}

func (b *Bot) handleEnrichmentDebug(ctx context.Context, msg *tgbotapi.Message, args []string) {
	query, limit, ok := parseEnrichmentDebugArgs(args)
	if !ok {
		b.reply(msg, "Usage: <code>/enrichment debug &lt;text|item_id&gt; [limit]</code>")

		return
	}

	if limit > enrichmentDebugMaxLimit {
		limit = enrichmentDebugMaxLimit
	}

	if isUUIDString(query) {
		b.handleEnrichmentDebugItem(ctx, msg, query)

		return
	}

	if len([]rune(query)) < enrichmentDebugMinQueryLen {
		b.reply(msg, fmt.Sprintf("Query too short. Use at least %d characters.", enrichmentDebugMinQueryLen))

		return
	}

	results, err := b.database.SearchItemsByText(ctx, query, limit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error searching items: %s", html.EscapeString(err.Error())))

		return
	}

	if len(results) == 0 {
		b.reply(msg, "No matching items found.")

		return
	}

	b.reply(msg, renderEnrichmentSearchResults(query, results))
}

func parseEnrichmentDebugArgs(args []string) (string, int, bool) {
	if len(args) == 0 {
		return "", 0, false
	}

	limit := enrichmentDebugDefaultLimit

	if len(args) > 1 {
		last := args[len(args)-1]

		if parsed, err := strconv.Atoi(last); err == nil {
			limit = parsed
			args = args[:len(args)-1]
		}
	}

	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" || limit <= 0 {
		return "", 0, false
	}

	return query, limit, true
}

func isUUIDString(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func renderEnrichmentSearchResults(query string, results []db.ItemSearchResult) string {
	var sb strings.Builder

	sb.WriteString("🔎 <b>Enrichment Debug Search</b>\n\n")
	sb.WriteString(fmt.Sprintf("Query: <code>%s</code>\n", html.EscapeString(query)))
	sb.WriteString(fmt.Sprintf("Matches: <code>%d</code>\n\n", len(results)))

	for _, item := range results {
		name := formatChannelName(item.ChannelUsername, item.ChannelTitle)
		snippet := buildEnrichmentSnippet(item.Summary, item.Text, enrichmentDebugSummaryLimit)
		link := FormatLink(item.ChannelUsername, item.ChannelPeerID, item.MessageID, "Open message")

		sb.WriteString(fmt.Sprintf("• <code>%s</code> — <b>%s</b>\n", item.ID, html.EscapeString(name)))
		sb.WriteString(fmt.Sprintf("  %s | <code>%s</code>\n", link, item.TGDate.Format(DateTimeFormat)))

		if snippet != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", html.EscapeString(snippet)))
		}
	}

	sb.WriteString("\nUse <code>/enrichment debug &lt;item_id&gt;</code> to show routing and queries.")

	return sb.String()
}

func buildEnrichmentSnippet(summary, text string, limit int) string {
	content := strings.TrimSpace(summary)

	if content == "" {
		content = strings.TrimSpace(text)
	}

	if content == "" {
		return ""
	}

	content = htmlutils.StripHTMLTags(content)
	content = truncateAnnotationText(content, limit)
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", " ")

	return strings.TrimSpace(content)
}

func formatChannelName(username, title string) string {
	if username != "" {
		return "@" + strings.TrimPrefix(username, "@")
	}

	if title != "" {
		return title
	}

	return annotateUnknown
}

func (b *Bot) handleEnrichmentDebugItem(ctx context.Context, msg *tgbotapi.Message, itemID string) {
	item, err := b.database.GetItemDebugDetail(ctx, itemID)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching item: %s", html.EscapeString(err.Error())))

		return
	}

	if item == nil {
		b.reply(msg, "Item not found.")

		return
	}

	links, linksErr := b.database.GetLinksForMessage(ctx, item.RawMessageID)
	if linksErr != nil {
		b.logger.Debug().Err(linksErr).Msg("enrichment debug: links lookup failed")
	}

	targetLangs := b.getTargetLanguagesForItem(ctx, item)
	queries := b.generateQueriesForItem(item, links)
	queries = b.expandQueriesForDebug(ctx, queries, targetLangs)

	output := buildEnrichmentDebugOutput(item, links, targetLangs, queries, b.isTranslationEnabled())
	b.reply(msg, output)
}

func (b *Bot) expandQueriesForDebug(ctx context.Context, queries []enrichment.GeneratedQuery, targetLangs []string) []enrichment.GeneratedQuery {
	if !b.isTranslationEnabled() || b.llmClient == nil {
		return queries
	}

	model := b.cfg.TranslationModel
	if model == "" {
		model = b.cfg.LLMModel
	}

	translationClient := enrichment.NewTranslationAdapter(b.llmClient, model)
	// Pass nil for cache - debug doesn't need translation caching
	expander := enrichment.NewQueryExpander(translationClient, nil, b.logger)

	maxQueries := b.cfg.EnrichmentMaxQueriesPerItem
	if maxQueries <= 0 {
		maxQueries = 5
	}

	return expander.ExpandQueries(ctx, queries, targetLangs, maxQueries)
}

func (b *Bot) isTranslationEnabled() bool {
	return b.cfg.EnrichmentQueryTranslate && b.llmClient != nil
}

func (b *Bot) getTargetLanguagesForItem(ctx context.Context, item *db.ItemDebugDetail) []string {
	policy := parseEnrichmentLanguagePolicy(b.cfg.EnrichmentLanguagePolicy)
	router := enrichment.NewLanguageRouter(policy, b.database)

	return router.GetTargetLanguages(ctx, &db.EnrichmentQueueItem{
		Summary:            item.Summary,
		Topic:              item.Topic,
		ChannelTitle:       item.ChannelTitle,
		ChannelUsername:    item.ChannelUsername,
		ChannelDescription: item.ChannelDesc,
		ChannelID:          item.ChannelID,
	})
}

func (b *Bot) generateQueriesForItem(item *db.ItemDebugDetail, links []domain.ResolvedLink) []enrichment.GeneratedQuery {
	queryGenerator := enrichment.NewQueryGenerator()

	return queryGenerator.Generate(item.Summary, item.Text, item.Topic, item.ChannelTitle, links)
}

func buildEnrichmentDebugOutput(item *db.ItemDebugDetail, links []domain.ResolvedLink, targetLangs []string, queries []enrichment.GeneratedQuery, translationEnabled bool) string {
	var sb strings.Builder

	sb.WriteString("🔎 <b>Enrichment Debug</b>\n\n")
	writeEnrichmentDebugItemInfo(&sb, item)
	writeEnrichmentDebugContent(&sb, item)
	writeEnrichmentDebugRouting(&sb, targetLangs, links, queries)

	if translationEnabled {
		sb.WriteString("\n<i>Note: queries shown include translations. Provider execution runs in the worker.</i>")
	} else {
		sb.WriteString("\n<i>Note: translation disabled. Provider execution runs in the worker.</i>")
	}

	return sb.String()
}

func writeEnrichmentDebugItemInfo(sb *strings.Builder, item *db.ItemDebugDetail) {
	fmt.Fprintf(sb, fmtItemCode, item.ID)
	fmt.Fprintf(sb, fmtStatusCode, html.EscapeString(item.Status))
	fmt.Fprintf(sb, fmtScoresCode, item.RelevanceScore, item.ImportanceScore)

	if item.Topic != "" {
		fmt.Fprintf(sb, fmtTopicCode, html.EscapeString(item.Topic))
	}

	if item.Language != "" {
		fmt.Fprintf(sb, "Language: <code>%s</code>\n", html.EscapeString(item.Language))
	}

	if item.LanguageSource != "" {
		fmt.Fprintf(sb, "Language Source: <code>%s</code>\n", html.EscapeString(item.LanguageSource))
	}

	name := formatChannelName(item.ChannelUsername, item.ChannelTitle)
	link := FormatLink(item.ChannelUsername, item.ChannelPeerID, item.MessageID, fmtOpenMessage)
	fmt.Fprintf(sb, "Channel: <b>%s</b> (%s)\n", html.EscapeString(name), link)
	fmt.Fprintf(sb, fmtTimeCode, item.TGDate.Format(DateTimeFormat))
}

func writeEnrichmentDebugContent(sb *strings.Builder, item *db.ItemDebugDetail) {
	summary := buildEnrichmentSnippet(item.Summary, "", enrichmentDebugSummaryLimit)

	if summary != "" {
		sb.WriteString(fmtSummaryHdr)
		fmt.Fprintf(sb, annotateBlockquoteFmt, html.EscapeString(summary))
	}

	text := buildEnrichmentSnippet("", item.Text, enrichmentDebugTextLimit)
	if text == "" && strings.TrimSpace(item.PreviewText) != "" {
		text = buildEnrichmentSnippet("", item.PreviewText, enrichmentDebugTextLimit)
	}

	if text != "" {
		sb.WriteString(fmtTextHdr)
		fmt.Fprintf(sb, annotateBlockquoteFmt, html.EscapeString(text))
	}
}

func writeEnrichmentDebugRouting(sb *strings.Builder, targetLangs []string, links []domain.ResolvedLink, queries []enrichment.GeneratedQuery) {
	sb.WriteString("\nRouting:\n")

	if len(targetLangs) == 0 {
		sb.WriteString("• Target languages: <code>none</code>\n")
	} else {
		fmt.Fprintf(sb, "• Target languages: <code>%s</code>\n", html.EscapeString(strings.Join(targetLangs, ", ")))
	}

	fmt.Fprintf(sb, "• Links available: <code>%d</code>\n", len(links))

	if len(queries) == 0 {
		sb.WriteString("• Generated queries: <code>0</code>\n")
	} else {
		fmt.Fprintf(sb, "• Generated queries: <code>%d</code>\n", len(queries))

		for _, q := range queries {
			fmt.Fprintf(sb, "  • <code>%s</code> (%s)\n", html.EscapeString(q.Query), html.EscapeString(q.Language))
		}
	}
}

func parseEnrichmentLanguagePolicy(raw string) domain.LanguageRoutingPolicy {
	policy := domain.LanguageRoutingPolicy{}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		policy.Default = []string{"en"}

		return policy
	}

	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		policy.Default = []string{"en"}
		return policy
	}

	if isLanguagePolicyEmpty(policy) {
		policy.Default = []string{"en"}
	}

	return policy
}

func isLanguagePolicyEmpty(p domain.LanguageRoutingPolicy) bool {
	return len(p.Default) == 0 && len(p.Channel) == 0 && len(p.Context) == 0 && len(p.Topic) == 0
}
