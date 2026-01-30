package digest

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// bulletGroup groups bullets by topic for rendering.
type bulletGroup struct {
	topic   string
	bullets []db.BulletForDigest
}

// formatBullets formats bullets for display instead of summaries.
// Returns empty string if no bullets are available, allowing fallback to summary mode.
func (rc *digestRenderContext) formatBullets(ctx context.Context, items []db.Item) string {
	if len(items) == 0 {
		return ""
	}

	itemIDs := extractItemIDs(items)

	bullets, err := rc.scheduler.database.GetBulletsForDigest(ctx, itemIDs)
	if err != nil || len(bullets) == 0 {
		return "" // Fallback to summary mode
	}

	groups := groupBulletsByTopic(bullets, rc.settings.bulletMaxPerCluster)

	var sb strings.Builder

	for _, g := range groups {
		rc.formatBulletGroup(&sb, g)
	}

	return sb.String()
}

// extractItemIDs extracts IDs from a slice of items.
func extractItemIDs(items []db.Item) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}

	return ids
}

// groupBulletsByTopic groups bullets by topic and limits per group.
func groupBulletsByTopic(bullets []db.BulletForDigest, maxPerTopic int) []bulletGroup {
	if maxPerTopic <= 0 {
		maxPerTopic = 5 // Default limit
	}

	topicToIdx := make(map[string]int)

	var groups []bulletGroup

	for _, b := range bullets {
		topic := b.Topic
		if topic == "" {
			topic = DefaultTopic
		}

		idx, seen := topicToIdx[topic]
		if !seen {
			topicToIdx[topic] = len(groups)
			groups = append(groups, bulletGroup{
				topic:   topic,
				bullets: []db.BulletForDigest{b},
			})
		} else if len(groups[idx].bullets) < maxPerTopic {
			groups[idx].bullets = append(groups[idx].bullets, b)
		}
	}

	return groups
}

// formatBulletGroup formats a group of bullets with the same topic.
func (rc *digestRenderContext) formatBulletGroup(sb *strings.Builder, g bulletGroup) {
	emoji := topicEmojis[g.topic]
	if emoji == "" {
		emoji = DefaultTopicEmoji
	}

	// Write topic header
	sb.WriteString(htmlutils.ItemStart)
	sb.WriteString(DigestTopicBorderTop)
	fmt.Fprintf(sb, FormatTopicHeaderWithCount, emoji, strings.ToUpper(html.EscapeString(g.topic)), len(g.bullets))
	sb.WriteString(DigestTopicBorderBot)

	// Write each bullet
	for _, b := range g.bullets {
		rc.formatSingleBullet(sb, b)
	}

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n")
}

// formatSingleBullet formats a single bullet point.
func (rc *digestRenderContext) formatSingleBullet(sb *strings.Builder, b db.BulletForDigest) {
	prefix := getImportancePrefix(b.ImportanceScore)
	sanitizedText := htmlutils.SanitizeHTML(b.Text)

	sb.WriteString(prefix)
	sb.WriteString(" ")
	sb.WriteString(BulletItemPrefix)
	sb.WriteString(sanitizedText)

	// Add source attribution if enabled
	if rc.settings.bulletSourceAttribution {
		sb.WriteString(rc.formatBulletSource(b))
	}

	sb.WriteString("\n")
}

// formatBulletSource formats the source attribution for a bullet.
func (rc *digestRenderContext) formatBulletSource(b db.BulletForDigest) string {
	if rc.settings.bulletSourceFormat == BulletSourceFormatCompact {
		return formatCompactSource(b)
	}

	return formatFullSource(b)
}

// formatCompactSource returns a compact source attribution (emoji + initial).
func formatCompactSource(b db.BulletForDigest) string {
	initial := getChannelInitial(b.SourceChannelTitle, b.SourceChannel)

	return fmt.Sprintf(" %s%s", BulletSourceEmoji, initial)
}

// formatFullSource returns a full source attribution (via @channel).
func formatFullSource(b db.BulletForDigest) string {
	if b.SourceChannel != "" {
		return fmt.Sprintf(" <i>(via @%s)</i>", html.EscapeString(b.SourceChannel))
	}

	if b.SourceChannelTitle != "" {
		return fmt.Sprintf(" <i>(via %s)</i>", html.EscapeString(b.SourceChannelTitle))
	}

	return ""
}

// getChannelInitial returns the first character of the channel name.
func getChannelInitial(title, channel string) string {
	if title != "" {
		return string([]rune(title)[0])
	}

	if channel != "" {
		return string([]rune(channel)[0])
	}

	return "?"
}
