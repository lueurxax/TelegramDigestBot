package bot

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	// DefaultEnrichmentRecentHours is the default lookback for recent evidence counts.
	DefaultEnrichmentRecentHours = 24
)

type enrichmentStats struct {
	queueStats    []db.EnrichmentQueueStat
	cacheCount    int
	totalMatches  int
	recentMatches int
	dailyUsage    int
	monthlyUsage  int
}

func (b *Bot) handleEnrichment(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.fetchEnrichmentStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error: %s", html.EscapeString(err.Error())))
		return
	}

	b.reply(msg, b.renderEnrichmentStatus(stats))
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
