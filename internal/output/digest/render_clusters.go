package digest

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// renderMultiItemCluster renders a cluster with multiple items.
func (rc *digestRenderContext) renderMultiItemCluster(ctx context.Context, sb *strings.Builder, c db.ClusterWithItems) bool {
	if rc.settings.consolidatedClustersEnabled {
		return rc.renderConsolidatedCluster(ctx, sb, c)
	}

	return rc.renderRepresentativeCluster(sb, c)
}

// renderConsolidatedCluster renders a cluster with an LLM-generated summary.
func (rc *digestRenderContext) renderConsolidatedCluster(ctx context.Context, sb *strings.Builder, c db.ClusterWithItems) bool {
	summary, ok := rc.findCachedClusterSummary(ctx, c.Items)
	if !ok {
		// Pass empty model to let the LLM registry handle task-specific model selection
		// via LLM_CLUSTER_MODEL env var or default task config
		evidence := rc.convertEvidenceForLLM(c.Items)
		generated, err := rc.llmClient.SummarizeClusterWithEvidence(ctx, c.Items, evidence, rc.settings.digestLanguage, "", rc.settings.digestTone)

		if err != nil || generated == "" {
			if err != nil {
				rc.logger.Warn().Err(err).Str("cluster", c.Topic).Msg("failed to summarize cluster, falling back to detailed list")
			}

			return rc.renderRepresentativeCluster(sb, c)
		}

		summary = htmlutils.SanitizeHTML(generated)
		rc.storeClusterSummaryCache(ctx, c.Items, summary)
	} else {
		summary = htmlutils.SanitizeHTML(summary)
	}

	if rc.seenSummaries[summary] {
		return false
	}

	rc.seenSummaries[summary] = true

	rc.renderConsolidatedSummary(sb, summary, c)

	return true
}

// renderConsolidatedSummary writes a consolidated cluster summary to the builder.
func (rc *digestRenderContext) renderConsolidatedSummary(sb *strings.Builder, summary string, c db.ClusterWithItems) {
	sb.WriteString(htmlutils.ItemStart)

	if c.Topic != "" {
		emoji := topicEmojis[c.Topic]
		if emoji == "" {
			emoji = DefaultTopicEmoji
		}

		sb.WriteString(DigestTopicBorderTop)
		fmt.Fprintf(sb, FormatTopicHeaderWithCount, emoji, strings.ToUpper(html.EscapeString(c.Topic)), len(c.Items))
		sb.WriteString(DigestTopicBorderBot)
	}

	fmt.Fprintf(sb, FormatPrefixSummary, getImportancePrefix(c.Items[0].ImportanceScore), summary)

	links := rc.collectSourceLinks(c.Items)
	if len(links) > 0 {
		fmt.Fprintf(sb, " <i>via %s</i>", strings.Join(links, DigestSourceSeparator))
	}

	if rc.factChecks != nil {
		if match, ok := findFactCheckMatch(c.Items, rc.factChecks); ok {
			sb.WriteString(formatFactCheckLine(match))
		}
	}

	if line := rc.scheduler.buildCorroborationLine(c.Items, c.Items[0]); line != "" {
		sb.WriteString(line)
	}

	rc.appendExplainabilityLine(sb, c.Items)

	rc.appendEvidenceLine(sb, c.Items)

	// Add expand link for the first (representative) item
	if len(c.Items) > 0 {
		rc.appendExpandLink(sb, c.Items[0].ID)
	}

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n")
}

// renderRepresentativeCluster renders a cluster using its representative item.
func (rc *digestRenderContext) renderRepresentativeCluster(sb *strings.Builder, c db.ClusterWithItems) bool {
	emoji := topicEmojis[c.Topic]
	if emoji == "" {
		emoji = DefaultTopicEmoji
	}

	representative := c.Items[0]
	if rc.seenSummaries[representative.Summary] {
		return false
	}

	rc.seenSummaries[representative.Summary] = true

	sb.WriteString(htmlutils.ItemStart)
	sb.WriteString(DigestTopicBorderTop)
	fmt.Fprintf(sb, "│ %s <b>%s</b>\n", emoji, strings.ToUpper(html.EscapeString(c.Topic)))
	sb.WriteString(DigestTopicBorderBot)

	sanitizedSummary := htmlutils.SanitizeHTML(representative.Summary)
	prefix := getImportancePrefix(representative.ImportanceScore)
	fmt.Fprintf(sb, FormatPrefixSummary, prefix, sanitizedSummary)

	links := rc.collectSourceLinks(c.Items)
	if len(links) > 0 {
		fmt.Fprintf(sb, DigestSourceVia, strings.Join(links, DigestSourceSeparator))
	}

	if rc.factChecks != nil {
		if match, ok := findFactCheckMatch(c.Items, rc.factChecks); ok {
			sb.WriteString(formatFactCheckLine(match))
		}
	}

	if line := rc.scheduler.buildCorroborationLine(c.Items, representative); line != "" {
		sb.WriteString(line)
	}

	rc.appendExplainabilityLine(sb, c.Items)

	rc.appendEvidenceLine(sb, c.Items)

	if len(c.Items) > 1 {
		fmt.Fprintf(sb, " <i>(+%d related)</i>", len(c.Items)-1)
	}

	// Add expand link for the representative item
	rc.appendExpandLink(sb, representative.ID)

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n\n")

	return true
}
