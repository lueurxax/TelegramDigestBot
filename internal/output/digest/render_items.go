package digest

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// summaryGroup groups items with the same summary.
type summaryGroup struct {
	summary         string
	items           []db.Item
	importanceScore float32
}

// formatItems formats a list of items for display.
func (rc *digestRenderContext) formatItems(items []db.Item, includeTopic bool) string {
	return rc.formatItemsWithContext(context.Background(), items, includeTopic)
}

// formatItemsWithContext formats a list of items for display with context support.
func (rc *digestRenderContext) formatItemsWithContext(ctx context.Context, items []db.Item, includeTopic bool) string {
	if len(items) == 0 {
		return ""
	}

	// Try bullet mode if enabled
	if rc.settings.bulletModeEnabled {
		if bulletOutput := rc.formatBullets(ctx, items); bulletOutput != "" {
			return bulletOutput
		}
		// Fallback to summary mode if no bullets available
	}

	groups := groupItemsBySummary(items, rc.seenSummaries)

	var sb strings.Builder

	for _, g := range groups {
		rc.seenSummaries[g.summary] = true
		rc.formatSummaryGroup(&sb, g, includeTopic)
	}

	return sb.String()
}

// groupItemsBySummary groups items by their summary text.
func groupItemsBySummary(items []db.Item, seenSummaries map[string]bool) []summaryGroup {
	var groups []summaryGroup

	summaryToIdx := make(map[string]int)

	for _, item := range items {
		if seenSummaries[item.Summary] {
			continue
		}

		idx, seen := summaryToIdx[item.Summary]
		if !seen {
			summaryToIdx[item.Summary] = len(groups)
			groups = append(groups, summaryGroup{
				summary:         item.Summary,
				items:           []db.Item{item},
				importanceScore: item.ImportanceScore,
			})
		} else {
			groups[idx].items = append(groups[idx].items, item)
			if item.ImportanceScore > groups[idx].importanceScore {
				groups[idx].importanceScore = item.ImportanceScore
			}
		}
	}

	return groups
}

// formatSummaryGroup formats a group of items with the same summary.
func (rc *digestRenderContext) formatSummaryGroup(sb *strings.Builder, g summaryGroup, includeTopic bool) {
	sanitizedSummary := htmlutils.SanitizeHTML(g.summary)
	prefix := getImportancePrefix(g.importanceScore)
	lowReliability := rc.isLowReliabilityGroup(g.items)

	if lowReliability {
		observability.LowReliabilityBadgeTotal.Inc()
	}

	sb.WriteString(htmlutils.ItemStart)
	sb.WriteString(formatSummaryLine(g, includeTopic, prefix, sanitizedSummary, lowReliability))
	fmt.Fprintf(sb, DigestSourceVia, strings.Join(rc.formatItemLinks(g.items), DigestSourceSeparator))

	if rc.factChecks != nil {
		if match, ok := findFactCheckMatch(g.items, rc.factChecks); ok {
			sb.WriteString(formatFactCheckLine(match))
		}
	}

	if len(g.items) > 0 {
		if line := rc.scheduler.buildCorroborationLine(g.items, g.items[0]); line != "" {
			sb.WriteString(line)
		}
	}

	rc.appendExplainabilityLine(sb, g.items)

	// Append evidence bullets (Phase 2)
	rc.appendEvidenceLine(sb, g.items)

	// Add expand link for the first item in the group
	if len(g.items) > 0 {
		rc.appendExpandLink(sb, g.items[0].ID)
	}

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n")
}

// formatSummaryLine formats the summary line with optional topic.
func formatSummaryLine(g summaryGroup, includeTopic bool, prefix, sanitizedSummary string, lowReliability bool) string {
	if lowReliability {
		prefix += " ⚠️"
	}

	if !includeTopic || g.items[0].Topic == "" {
		return fmt.Sprintf(FormatPrefixSummary, prefix, sanitizedSummary)
	}

	emoji := topicEmojis[g.items[0].Topic]
	if emoji == "" {
		emoji = EmojiBullet
	} else {
		emoji += " " + EmojiBullet
	}

	return fmt.Sprintf("%s %s <b>%s</b>: %s", prefix, emoji, html.EscapeString(g.items[0].Topic), sanitizedSummary)
}

// formatItemLinks formats links for a list of items.
func (rc *digestRenderContext) formatItemLinks(items []db.Item) []string {
	links := make([]string, 0, len(items))

	for _, item := range items {
		label := formatItemLabel(item)
		links = append(links, rc.scheduler.formatLink(item, label))
	}

	return links
}

// formatItemLabel returns the display label for an item.
func formatItemLabel(item db.Item) string {
	if item.SourceChannel != "" {
		return "@" + item.SourceChannel
	}

	if item.SourceChannelTitle != "" {
		return item.SourceChannelTitle
	}

	return DefaultSourceLabel
}

// collectSourceLinks collects source links for a list of items.
func (rc *digestRenderContext) collectSourceLinks(items []db.Item) []string {
	links := make([]string, 0, len(items))

	for _, item := range items {
		label := item.SourceChannel
		if label != "" {
			label = "@" + label
		}

		if label == "" {
			label = item.SourceChannelTitle
		}

		if label == "" {
			label = DefaultSourceLabel
		}

		links = append(links, rc.scheduler.formatLink(item, label))
	}

	return links
}

// findFactCheckMatch finds a fact-check match for a list of items.
func findFactCheckMatch(items []db.Item, factChecks map[string]db.FactCheckMatch) (db.FactCheckMatch, bool) {
	for _, item := range items {
		if item.ID == "" {
			continue
		}

		match, ok := factChecks[item.ID]
		if ok {
			return match, true
		}
	}

	return db.FactCheckMatch{}, false
}

// formatFactCheckLine formats the fact-check line.
func formatFactCheckLine(match db.FactCheckMatch) string {
	if match.URL == "" {
		return ""
	}

	label := "Fact-check"
	if match.Publisher != "" {
		label = match.Publisher
	}

	return fmt.Sprintf("\n    ↳ <i>Related fact-check: <a href=\"%s\">%s</a></i>", html.EscapeString(match.URL), html.EscapeString(label))
}
