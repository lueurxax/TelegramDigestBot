package bot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

type factCheckStats struct {
	queueStats    []db.FactCheckQueueStat
	cacheCount    int
	totalMatches  int
	recentMatches int
	matches       []db.FactCheckMatch
}

func (b *Bot) handleFactCheck(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	limit, ok := parseFactCheckArgs(args)
	if !ok {
		b.reply(msg, "Usage: <code>/factcheck [limit]</code>")

		return
	}

	stats, err := b.fetchFactCheckStats(ctx, limit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, b.renderFactCheckStatus(stats, limit))
}

func (b *Bot) fetchFactCheckStats(ctx context.Context, limit int) (*factCheckStats, error) {
	queueStats, err := b.database.GetFactCheckQueueStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching fact check queue stats: %w", err)
	}

	cacheCount, err := b.database.GetFactCheckCacheCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching fact check cache count: %w", err)
	}

	totalMatches, err := b.database.CountFactCheckMatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching fact check matches: %w", err)
	}

	since := time.Now().Add(-time.Duration(DefaultFactCheckRecentHours) * time.Hour)

	recentMatches, err := b.database.CountFactCheckMatchesSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("counting recent fact check matches: %w", err)
	}

	matches, err := b.database.GetRecentFactCheckMatches(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("getting recent fact check matches: %w", err)
	}

	return &factCheckStats{
		queueStats:    queueStats,
		cacheCount:    cacheCount,
		totalMatches:  totalMatches,
		recentMatches: recentMatches,
		matches:       matches,
	}, nil
}

func (b *Bot) renderFactCheckStatus(stats *factCheckStats, limit int) string {
	counts := map[string]int{
		db.FactCheckStatusPending:    0,
		db.FactCheckStatusProcessing: 0,
		db.FactCheckStatusDone:       0,
		db.FactCheckStatusError:      0,
	}

	for _, entry := range stats.queueStats {
		counts[entry.Status] = entry.Count
	}

	statusLabel := StatusDisabled
	if b.cfg.FactCheckGoogleEnabled && b.cfg.FactCheckGoogleAPIKey != "" {
		statusLabel = StatusEnabled
	}

	var sb strings.Builder

	sb.WriteString("📊 <b>Fact Check Status</b>\n\n")
	sb.WriteString(fmt.Sprintf("• <b>Status:</b> %s\n", statusLabel))
	sb.WriteString("\nQueue:\n")
	sb.WriteString(fmt.Sprintf("• pending: <code>%d</code>\n", counts[db.FactCheckStatusPending]))
	sb.WriteString(fmt.Sprintf("• processing: <code>%d</code>\n", counts[db.FactCheckStatusProcessing]))
	sb.WriteString(fmt.Sprintf("• done: <code>%d</code>\n", counts[db.FactCheckStatusDone]))
	sb.WriteString(fmt.Sprintf("• error: <code>%d</code>\n", counts[db.FactCheckStatusError]))
	sb.WriteString(fmt.Sprintf("\nCache entries: <code>%d</code>\n", stats.cacheCount))
	sb.WriteString(fmt.Sprintf("Matches (last %d hours): <code>%d</code>\n", DefaultFactCheckRecentHours, stats.recentMatches))
	sb.WriteString(fmt.Sprintf("Matches total: <code>%d</code>\n", stats.totalMatches))

	if len(stats.matches) == 0 {
		sb.WriteString("\nNo fact check matches recorded yet.\n")

		return sb.String()
	}

	sb.WriteString("\nRecent matches:\n")
	b.renderRecentMatches(&sb, stats.matches)

	return sb.String()
}

func (b *Bot) renderRecentMatches(sb *strings.Builder, matches []db.FactCheckMatch) {
	for _, match := range matches {
		publisher := match.Publisher
		if publisher == "" {
			publisher = discoveryUnknown
		}

		rating := match.Rating
		if rating == "" {
			rating = "unrated"
		}

		fmt.Fprintf(sb, "• <b>%s</b> - <i>%s</i> (<code>%s</code>)\n",
			html.EscapeString(publisher),
			html.EscapeString(rating),
			match.MatchedAt.Format(DateTimeFormat))

		if match.URL != "" {
			fmt.Fprintf(sb, "  <a href=\"%s\">Fact check link</a>\n", html.EscapeString(match.URL))
		}

		claim := strings.TrimSpace(match.Claim)
		if claim != "" {
			claim = truncateFactCheckClaim(claim, FactCheckClaimLimit)
			fmt.Fprintf(sb, "  Claim: %s\n", html.EscapeString(claim))
		}
	}
}

func parseFactCheckArgs(args []string) (int, bool) {
	if len(args) == 0 {
		return DefaultFactCheckLimit, true
	}

	if len(args) > 1 {
		return 0, false
	}

	limit, err := strconv.Atoi(args[0])
	if err != nil || limit <= 0 {
		return 0, false
	}

	return limit, true
}

func truncateFactCheckClaim(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit]) + "..."
}
