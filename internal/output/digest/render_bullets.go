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
	defaultMaxBulletsPerCluster = 2  // Max bullets from a single cluster
	defaultMaxBulletsPerTopic   = 20 // Max bullets per topic section
	minBulletsForBulletMode     = 3  // Minimum bullets to keep bullet mode active
	minBulletCoverageRatio      = 0.5
)

type bulletTier struct {
	label    string
	emoji    string
	minScore float32
}

var bulletTiers = []bulletTier{
	{label: "Breaking", emoji: EmojiBreaking, minScore: ImportanceScoreBreaking},
	{label: "Notable", emoji: EmojiNotable, minScore: ImportanceScoreNotable},
	{label: "Standard", emoji: EmojiStandard, minScore: ImportanceScoreStandard},
	{label: "Minor", emoji: EmojiBullet, minScore: -1},
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

	// Step 1: Filter out low-importance bullets (post-dedup filtering)
	filteredBullets := filterBulletsByImportance(bullets, rc.settings.bulletMinImportance)
	if len(filteredBullets) == 0 {
		return "" // Fallback to summary mode
	}

	// Step 2: Limit bullets per cluster to avoid one story dominating
	clusterIndex := buildItemClusterIndex(rc.clusters)
	limitedBullets := limitBulletsPerCluster(filteredBullets, rc.settings.bulletMaxPerCluster, clusterIndex)

	dedupedBullets := dedupeBulletsByText(limitedBullets)

	if !hasSufficientBulletCoverage(len(items), len(dedupedBullets)) {
		return "" // Fallback to summary mode when bullet coverage is too low
	}

	// Step 3: Group by tier, then topic
	tiers := groupBulletsByTier(dedupedBullets)

	var sb strings.Builder

	for idx, tier := range bulletTiers {
		tierBullets := tiers[idx]
		if len(tierBullets) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf(FormatSectionHeader, tier.emoji, tier.label))

		groups := groupBulletsByTopic(tierBullets, defaultMaxBulletsPerTopic)
		for _, g := range groups {
			rc.formatBulletGroup(&sb, g)
		}
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

// hasSufficientBulletCoverage checks if bullet mode should be used.
func hasSufficientBulletCoverage(itemCount, bulletCount int) bool {
	if itemCount < minBulletsForBulletMode {
		return true // Small digests don't need coverage check
	}

	coverage := float32(bulletCount) / float32(itemCount)

	return bulletCount >= minBulletsForBulletMode && coverage >= minBulletCoverageRatio
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

// limitBulletsPerCluster limits the number of bullets from each cluster.
// This prevents a single story from dominating the digest.
func limitBulletsPerCluster(bullets []db.BulletForDigest, maxPerCluster int, clusterIndex map[string]string) []db.BulletForDigest {
	if maxPerCluster <= 0 {
		maxPerCluster = defaultMaxBulletsPerCluster
	}

	clusterCounts := make(map[string]int)
	result := make([]db.BulletForDigest, 0, len(bullets))

	for _, b := range bullets {
		clusterID := clusterIndex[b.ItemID]
		if clusterID == "" {
			clusterID = b.ItemID
		}

		if clusterCounts[clusterID] < maxPerCluster {
			result = append(result, b)
			clusterCounts[clusterID]++
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

func groupBulletsByTier(bullets []db.BulletForDigest) map[int][]db.BulletForDigest {
	tiered := make(map[int][]db.BulletForDigest, len(bulletTiers))

	for _, b := range bullets {
		idx := tierIndex(b.ImportanceScore)
		tiered[idx] = append(tiered[idx], b)
	}

	return tiered
}

func dedupeBulletsByText(bullets []db.BulletForDigest) []db.BulletForDigest {
	if len(bullets) <= 1 {
		return bullets
	}

	seen := make(map[string]bool, len(bullets))
	deduped := make([]db.BulletForDigest, 0, len(bullets))

	for _, b := range bullets {
		key := normalizeBulletText(b.Text)
		if key == "" {
			continue
		}

		if seen[key] {
			continue
		}

		seen[key] = true

		deduped = append(deduped, b)
	}

	return deduped
}

func normalizeBulletText(text string) string {
	if text == "" {
		return ""
	}

	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return ""
	}

	return strings.Join(strings.Fields(normalized), " ")
}

func tierIndex(score float32) int {
	for i, tier := range bulletTiers {
		if score >= tier.minScore {
			return i
		}
	}

	return len(bulletTiers) - 1
}

func buildItemClusterIndex(clusters []db.ClusterWithItems) map[string]string {
	index := make(map[string]string)

	for _, cluster := range clusters {
		for _, item := range cluster.Items {
			index[item.ID] = cluster.ID
		}
	}

	return index
}

// formatBulletGroup formats a group of bullets with the same topic.
func (rc *digestRenderContext) formatBulletGroup(sb *strings.Builder, g bulletGroup) {
	emoji := topicEmojis[g.topic]
	if emoji == "" {
		emoji = DefaultTopicEmoji
	}

	// Write compact topic header for mobile-friendly output
	sb.WriteString(htmlutils.ItemStart)
	fmt.Fprintf(sb, "%s <b>%s</b> (%d)\n", emoji, strings.ToUpper(html.EscapeString(g.topic)), len(g.bullets))

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

	var sourceParts []string

	if b.SourceCount > 1 {
		sourceParts = append(sourceParts, fmt.Sprintf("<i>(%d sources)</i>", b.SourceCount))
	} else if rc.settings.bulletSourceAttribution {
		source := strings.TrimSpace(rc.formatBulletSource(b))
		if source != "" {
			sourceParts = append(sourceParts, source)
		}
	}

	if rc.expandLinksEnabled && b.ItemID != "" {
		token, err := rc.scheduler.expandLinkGenerator.Generate(b.ItemID, ExpandedViewSystemUserID)
		if err == nil {
			sourceParts = append(sourceParts, fmt.Sprintf("<a href=\"%s/i/%s\">📖</a>", rc.expandBaseURL, token))
		}
	}

	if len(sourceParts) > 0 {
		sb.WriteString("\n    ↳ ")
		sb.WriteString(strings.Join(sourceParts, " "))
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
