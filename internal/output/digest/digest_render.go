package digest

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

// digestSettings holds all settings needed for building a digest
type digestSettings struct {
	topicsEnabled               bool
	freshnessDecayHours         int
	freshnessFloor              float32
	topicDiversityCap           float32
	minTopicCount               int
	editorEnabled               bool
	smartLLMModel               string
	consolidatedClustersEnabled bool
	editorDetailedItems         bool
	digestLanguage              string
	digestTone                  string
}

func (s *Scheduler) getDigestSettings(ctx context.Context, logger *zerolog.Logger) digestSettings {
	ds := digestSettings{
		topicsEnabled:       true,
		freshnessDecayHours: s.cfg.FreshnessDecayHours,
		freshnessFloor:      s.cfg.FreshnessFloor,
		topicDiversityCap:   s.cfg.TopicDiversityCap,
		minTopicCount:       s.cfg.MinTopicCount,
		editorDetailedItems: true,
	}

	s.loadDigestSettingsFromDB(ctx, logger, &ds)

	return ds
}

func (s *Scheduler) loadDigestSettingsFromDB(ctx context.Context, logger *zerolog.Logger, ds *digestSettings) {
	loadSetting := func(key string, target interface{}, logMsg string) {
		if err := s.database.GetSetting(ctx, key, target); err != nil {
			logger.Debug().Err(err).Msg(logMsg)
		}
	}

	loadSetting("topics_enabled", &ds.topicsEnabled, "could not get topics_enabled from DB")
	loadSetting("freshness_decay_hours", &ds.freshnessDecayHours, "could not get freshness_decay_hours from DB")
	loadSetting("freshness_floor", &ds.freshnessFloor, "could not get freshness_floor from DB")
	loadSetting("topic_diversity_cap", &ds.topicDiversityCap, "could not get topic_diversity_cap from DB")
	loadSetting("min_topic_count", &ds.minTopicCount, "could not get min_topic_count from DB")
	loadSetting("editor_enabled", &ds.editorEnabled, "could not get editor_enabled from DB")
	loadSetting(SettingSmartLLMModel, &ds.smartLLMModel, MsgCouldNotGetSmartLLMModel)
	loadSetting("consolidated_clusters_enabled", &ds.consolidatedClustersEnabled, "could not get consolidated_clusters_enabled from DB")
	loadSetting("editor_detailed_items", &ds.editorDetailedItems, "could not get editor_detailed_items from DB")
	loadSetting(SettingDigestLanguage, &ds.digestLanguage, MsgCouldNotGetDigestLanguage)
	loadSetting("digest_tone", &ds.digestTone, "could not get digest_tone from DB")
}

// clusterGroup groups clusters or items by importance level
type clusterGroup struct {
	clusters []db.ClusterWithItems
	items    []db.Item
}

// digestRenderContext holds all context needed for rendering a digest
type digestRenderContext struct {
	scheduler     *Scheduler
	llmClient     llm.Client
	settings      digestSettings
	items         []db.Item
	clusters      []db.ClusterWithItems
	start         time.Time
	end           time.Time
	seenSummaries map[string]bool
	logger        *zerolog.Logger
}

func (s *Scheduler) newRenderContext(_ context.Context, settings digestSettings, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, logger *zerolog.Logger) *digestRenderContext {
	return &digestRenderContext{
		scheduler:     s,
		llmClient:     s.llmClient,
		settings:      settings,
		items:         items,
		clusters:      clusters,
		start:         start,
		end:           end,
		seenSummaries: make(map[string]bool),
		logger:        logger,
	}
}

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

func (rc *digestRenderContext) buildHeaderSection(sb *strings.Builder) {
	header := rc.getHeader()

	sb.WriteString(DigestSeparatorLine)
	fmt.Fprintf(sb, "📰 <b>%s</b> • %s - %s\n", html.EscapeString(header), rc.start.Format(TimeFormatHourMinute), rc.end.Format(TimeFormatHourMinute))
	sb.WriteString(DigestSeparatorLine)
}

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

func (rc *digestRenderContext) generateNarrative(ctx context.Context, sb *strings.Builder) bool {
	if !rc.settings.editorEnabled || rc.settings.smartLLMModel == "" {
		return false
	}

	narrative, err := rc.llmClient.GenerateNarrative(ctx, rc.items, rc.settings.digestLanguage, rc.settings.smartLLMModel, rc.settings.digestTone)
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

func (rc *digestRenderContext) categorizeByImportance() (breaking, notable, also clusterGroup) {
	if rc.settings.topicsEnabled && len(rc.clusters) > 0 {
		return categorizeClusters(rc.clusters)
	}

	return categorizeItems(rc.items)
}

func categorizeClusters(clusters []db.ClusterWithItems) (breaking, notable, also clusterGroup) {
	for _, c := range clusters {
		maxImp := clusterMaxImportance(c)

		if maxImp >= ImportanceScoreBreaking {
			breaking.clusters = append(breaking.clusters, c)
		} else if maxImp >= ImportanceScoreNotable {
			notable.clusters = append(notable.clusters, c)
		} else {
			also.clusters = append(also.clusters, c)
		}
	}

	return breaking, notable, also
}

func clusterMaxImportance(c db.ClusterWithItems) float32 {
	maxImp := float32(0)

	for _, it := range c.Items {
		if it.ImportanceScore > maxImp {
			maxImp = it.ImportanceScore
		}
	}

	return maxImp
}

func categorizeItems(items []db.Item) (breaking, notable, also clusterGroup) {
	for _, it := range items {
		if it.ImportanceScore >= ImportanceScoreBreaking {
			breaking.items = append(breaking.items, it)
		} else if it.ImportanceScore >= ImportanceScoreNotable {
			notable.items = append(notable.items, it)
		} else {
			also.items = append(also.items, it)
		}
	}

	return breaking, notable, also
}

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
				formatted := rc.scheduler.formatItems(c.Items, true, rc.seenSummaries)
				if formatted != "" {
					hasContent = true

					groupSb.WriteString(formatted)
				}
			}
		}
	} else {
		formatted := rc.scheduler.formatItems(group.items, true, rc.seenSummaries)
		if formatted != "" {
			hasContent = true

			groupSb.WriteString(formatted)
		}
	}

	if hasContent {
		fmt.Fprintf(sb, "\n%s <b>%s</b>\n", emoji, title)
		sb.WriteString(groupSb.String())
	}
}

func (rc *digestRenderContext) renderMultiItemCluster(ctx context.Context, sb *strings.Builder, c db.ClusterWithItems) bool {
	if rc.settings.consolidatedClustersEnabled {
		return rc.renderConsolidatedCluster(ctx, sb, c)
	}

	return rc.renderRepresentativeCluster(sb, c)
}

func (rc *digestRenderContext) renderConsolidatedCluster(ctx context.Context, sb *strings.Builder, c db.ClusterWithItems) bool {
	model := rc.settings.smartLLMModel
	if model == "" {
		model = rc.scheduler.cfg.LLMModel
	}

	summary, err := rc.llmClient.SummarizeCluster(ctx, c.Items, rc.settings.digestLanguage, model, rc.settings.digestTone)
	if err != nil {
		rc.logger.Warn().Err(err).Str("cluster", c.Topic).Msg("failed to summarize cluster, falling back to detailed list")
		return rc.renderRepresentativeCluster(sb, c)
	}

	if summary == "" {
		return rc.renderRepresentativeCluster(sb, c)
	}

	summary = htmlutils.SanitizeHTML(summary)
	if rc.seenSummaries[summary] {
		return false
	}

	rc.seenSummaries[summary] = true

	sb.WriteString(htmlutils.ItemStart)

	if c.Topic != "" {
		emoji := topicEmojis[c.Topic]
		if emoji == "" {
			emoji = DefaultTopicEmoji
		}

		sb.WriteString(DigestTopicBorderTop)
		fmt.Fprintf(sb, "│ %s <b>%s</b> (%d)\n", emoji, strings.ToUpper(html.EscapeString(c.Topic)), len(c.Items))
		sb.WriteString(DigestTopicBorderBot)
	}

	fmt.Fprintf(sb, FormatPrefixSummary, getImportancePrefix(c.Items[0].ImportanceScore), summary)

	links := rc.collectSourceLinks(c.Items)
	if len(links) > 0 {
		fmt.Fprintf(sb, " <i>via %s</i>", strings.Join(links, DigestSourceSeparator))
	}

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n")

	return true
}

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

	if len(c.Items) > 1 {
		fmt.Fprintf(sb, " <i>(+%d related)</i>", len(c.Items)-1)
	}

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n\n")

	return true
}

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
