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

// Bullet rendering limits
const (
	defaultMaxBulletsPerItem  = 2  // Max bullets from a single item/cluster
	defaultMaxBulletsPerTopic = 20 // Max bullets per topic section
)

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

	// Step 1: Filter out low-importance bullets (post-dedup filtering)
	filteredBullets := filterBulletsByImportance(bullets, rc.settings.bulletMinImportance)
	if len(filteredBullets) == 0 {
		return "" // Fallback to summary mode
	}

	// Step 2: Limit bullets per item/cluster to avoid one story dominating
	limitedBullets := limitBulletsPerItem(filteredBullets, rc.settings.bulletMaxPerCluster)

	// Step 3: Group by topic with a higher limit
	groups := groupBulletsByTopic(limitedBullets, defaultMaxBulletsPerTopic)

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

// filterBulletsByImportance filters out bullets below the importance threshold.
// This allows post-dedup filtering since corroboration may boost importance.
func filterBulletsByImportance(bullets []db.BulletForDigest, minImportance float32) []db.BulletForDigest {
	if minImportance <= 0 {
		return bullets // No filtering if threshold is zero or negative
	}

	result := make([]db.BulletForDigest, 0, len(bullets))

	for _, b := range bullets {
		if b.ImportanceScore >= minImportance {
			result = append(result, b)
		}
	}

	return result
}

// limitBulletsPerItem limits the number of bullets from each item/cluster.
// This prevents a single story from dominating the digest.
func limitBulletsPerItem(bullets []db.BulletForDigest, maxPerItem int) []db.BulletForDigest {
	if maxPerItem <= 0 {
		maxPerItem = defaultMaxBulletsPerItem
	}

	itemCounts := make(map[string]int)
	result := make([]db.BulletForDigest, 0, len(bullets))

	for _, b := range bullets {
		if itemCounts[b.ItemID] < maxPerItem {
			result = append(result, b)
			itemCounts[b.ItemID]++
		}
	}

	return result
}

// groupBulletsByTopic groups bullets by topic with a limit per topic.
func groupBulletsByTopic(bullets []db.BulletForDigest, maxPerTopic int) []bulletGroup {
	if maxPerTopic <= 0 {
		maxPerTopic = defaultMaxBulletsPerTopic
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

// formatSingleBullet formats a single bullet point with optional expanded view link.
func (rc *digestRenderContext) formatSingleBullet(sb *strings.Builder, b db.BulletForDigest) {
	prefix := getImportancePrefix(b.ImportanceScore)
	sanitizedText := htmlutils.SanitizeHTML(b.Text)

	sb.WriteString(prefix)
	sb.WriteString(" ")
	sb.WriteString(BulletItemPrefix)
	sb.WriteString(sanitizedText)

	// Add corroboration count if multiple sources confirm this claim
	if b.SourceCount > 1 {
		fmt.Fprintf(sb, " <i>(%d sources)</i>", b.SourceCount)
	} else if rc.settings.bulletSourceAttribution {
		// Add single source attribution if enabled
		sb.WriteString(rc.formatBulletSource(b))
	}

	// Add expanded view link if enabled
	rc.appendBulletExpandLink(sb, b.ItemID)

	sb.WriteString("\n")
}

// appendBulletExpandLink adds an expanded view link for a bullet's source item.
func (rc *digestRenderContext) appendBulletExpandLink(sb *strings.Builder, itemID string) {
	if !rc.expandLinksEnabled || itemID == "" {
		return
	}

	token, err := rc.scheduler.expandLinkGenerator.Generate(itemID, ExpandedViewSystemUserID)
	if err != nil {
		return
	}

	fmt.Fprintf(sb, " <a href=\"%s/i/%s\">📖</a>", rc.expandBaseURL, token)
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
