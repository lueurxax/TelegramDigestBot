package digest

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// getHeader returns the localized header text.
func (rc *digestRenderContext) getHeader() string {
	header := "Digest for"

	switch strings.ToLower(rc.settings.digestLanguage) {
	case "ru":
		header = "Дайджест за"
	case "de":
		header = "Digest für"
	case "es":
		header = "Resumen para"
	case "fr":
		header = "Résumé pour"
	case "it":
		header = "Riassunto per"
	}

	return header
}

// getSectionTitles returns the localized section titles.
func (rc *digestRenderContext) getSectionTitles() (breaking, notable, also string) {
	breaking = "Breaking"
	notable = "Notable"
	also = "Also"

	switch strings.ToLower(rc.settings.digestLanguage) {
	case "ru":
		breaking = "Срочно"
		notable = "Важное"
		also = "Остальное"
	case "de":
		breaking = "Eilmeldung"
		notable = "Wichtig"
		also = "Weiteres"
	case "es":
		breaking = "Última hora"
		notable = "Destacado"
		also = "Otros"
	case "fr":
		breaking = "Flash info"
		notable = "Important"
		also = "Autres"
	case "it":
		breaking = "Ultime notizie"
		notable = "Importante"
		also = "Altro"
	}

	return breaking, notable, also
}

// buildHeaderSection builds the digest header.
func (rc *digestRenderContext) buildHeaderSection(sb *strings.Builder) {
	header := rc.getHeader()

	sb.WriteString(DigestSeparatorLine)
	fmt.Fprintf(sb, "📰 <b>%s</b> • %s - %s\n", html.EscapeString(header), rc.displayStart.Format(TimeFormatHourMinute), rc.displayEnd.Format(TimeFormatHourMinute))
	sb.WriteString(DigestSeparatorLine)
}

// buildMetadataSection builds the digest metadata line.
func (rc *digestRenderContext) buildMetadataSection(sb *strings.Builder) {
	uniqueChannels := make(map[string]bool)

	for _, item := range rc.items {
		uniqueChannels[item.SourceChannel] = true
	}

	topicCount := 0
	if rc.settings.topicsEnabled {
		topicCount = len(rc.clusters)
		if topicCount == 0 {
			topicCount = countDistinctTopics(rc.items)
		}
	}

	fmt.Fprintf(sb, "📊 <i>%d items from %d channels | %d topics</i>\n\n", len(rc.items), len(uniqueChannels), topicCount)
}

// generateNarrative generates the editor-in-chief narrative.
func (rc *digestRenderContext) generateNarrative(ctx context.Context, sb *strings.Builder) bool {
	if !rc.settings.editorEnabled {
		return false
	}

	evidence := rc.convertEvidenceForLLM(rc.items)

	// Pass empty model to let the LLM registry handle task-specific model selection
	// via LLM_NARRATIVE_MODEL env var or default task config
	narrative, err := rc.llmClient.GenerateNarrativeWithEvidence(ctx, rc.items, evidence, rc.settings.digestLanguage, "", rc.settings.digestTone)
	if err != nil {
		rc.logger.Warn().Err(err).Msg("Editor-in-Chief narrative generation failed")
		return false
	}

	if narrative == "" {
		return false
	}

	sb.WriteString("<blockquote>\n")
	sb.WriteString("📝 <b>Overview</b>\n\n")
	sb.WriteString(htmlutils.SanitizeHTML(narrative))
	sb.WriteString("\n</blockquote>\n")

	if rc.settings.editorDetailedItems {
		sb.WriteString("\n─────────────────────\n<b>📋 Detailed items:</b>\n\n")
	}

	return true
}

// renderGroup renders a group of items or clusters.
func (rc *digestRenderContext) renderGroup(ctx context.Context, sb *strings.Builder, group clusterGroup, emoji, title string) {
	if len(group.clusters) == 0 && len(group.items) == 0 {
		return
	}

	var groupSb strings.Builder

	hasContent := false

	if len(group.clusters) > 0 {
		for _, c := range group.clusters {
			if len(c.Items) > 1 {
				rendered := rc.renderMultiItemCluster(ctx, &groupSb, c)
				if rendered {
					hasContent = true
				}
			} else {
				formatted := rc.formatItems(c.Items, true)
				if formatted != "" {
					hasContent = true

					groupSb.WriteString(formatted)
				}
			}
		}
	} else {
		formatted := rc.formatItems(group.items, true)
		if formatted != "" {
			hasContent = true

			groupSb.WriteString(formatted)
		}
	}

	if hasContent {
		fmt.Fprintf(sb, FormatSectionHeader, emoji, title)
		sb.WriteString(groupSb.String())
	}
}

// renderOthersAsNarrative generates an LLM narrative summary for the "others" section.
func (rc *digestRenderContext) renderOthersAsNarrative(ctx context.Context, sb *strings.Builder, group clusterGroup, emoji, title string) bool {
	allItems := collectAllItems(group)
	if len(allItems) == 0 {
		return false
	}

	narrative, ok := rc.findCachedClusterSummary(ctx, allItems)
	if !ok {
		// Generate narrative for "others" items with evidence context
		// Pass empty model to let the LLM registry handle task-specific model selection
		// via LLM_CLUSTER_MODEL env var or default task config
		evidence := rc.convertEvidenceForLLM(allItems)
		generated, err := rc.llmClient.SummarizeClusterWithEvidence(ctx, allItems, evidence, rc.settings.digestLanguage, "", rc.settings.digestTone)

		if err != nil || generated == "" {
			if err != nil {
				rc.logger.Warn().Err(err).Msg("failed to generate others narrative, falling back to detailed list")
			}

			rc.renderGroup(ctx, sb, group, emoji, title)

			return true
		}

		narrative = htmlutils.SanitizeHTML(generated)
		rc.storeClusterSummaryCache(ctx, allItems, narrative)
	} else {
		narrative = htmlutils.SanitizeHTML(narrative)
	}

	rc.renderNarrativeSection(sb, narrative, emoji, title, allItems)

	return true
}

// renderNarrativeSection renders a narrative section with metadata.
func (rc *digestRenderContext) renderNarrativeSection(sb *strings.Builder, narrative, emoji, title string, allItems []db.Item) {
	fmt.Fprintf(sb, FormatSectionHeader, emoji, title)
	sb.WriteString(htmlutils.SanitizeHTML(narrative))
	sb.WriteString("\n")

	if rc.factChecks != nil {
		if match, ok := findFactCheckMatch(allItems, rc.factChecks); ok {
			sb.WriteString(formatFactCheckLine(match))
		}
	}

	if line := rc.scheduler.buildCorroborationLine(allItems, allItems[0]); line != "" {
		sb.WriteString(line)
	}

	rc.appendExplainabilityLine(sb, allItems)

	rc.appendEvidenceLine(sb, allItems)

	// Add expand links for all items in the narrative
	rc.appendExpandLinksForItems(sb, allItems)
}

// buildContextSection builds the background context section.
func (rc *digestRenderContext) buildContextSection(sb *strings.Builder) {
	backgroundSources := make(map[string]db.EvidenceSource)

	for _, sources := range rc.evidence {
		for _, es := range sources {
			// Heuristic for background info: Wikipedia is the primary source for context
			if strings.Contains(strings.ToLower(es.Source.Domain), "wikipedia.org") {
				if _, seen := backgroundSources[es.Source.URL]; !seen {
					backgroundSources[es.Source.URL] = es.Source
				}
			}
		}
	}

	if len(backgroundSources) == 0 {
		return
	}

	sb.WriteString("\n<b>📖 Контекст</b>\n")

	// Sort URLs for stable output
	urls := make([]string, 0, len(backgroundSources))
	for url := range backgroundSources {
		urls = append(urls, url)
	}

	sort.Strings(urls)

	for _, url := range urls {
		src := backgroundSources[url]
		title := src.Title

		if title == "" {
			title = src.Domain
		}

		fmt.Fprintf(sb, "• <a href=\"%s\">%s</a> (%s)\n", html.EscapeString(src.URL), html.EscapeString(title), html.EscapeString(src.Domain))
	}
}
