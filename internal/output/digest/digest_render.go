package digest

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/schedule"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
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
	othersAsNarrative           bool
}

const errInvalidScheduleTimezone = "invalid digest schedule timezone"

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
	loadSetting("others_as_narrative", &ds.othersAsNarrative, "could not get others_as_narrative from DB")
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
	displayStart  time.Time
	displayEnd    time.Time
	seenSummaries map[string]bool
	factChecks    map[string]db.FactCheckMatch
	evidence      map[string][]db.ItemEvidenceWithSource
	logger        *zerolog.Logger
}

func (s *Scheduler) newRenderContext(ctx context.Context, settings digestSettings, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, factChecks map[string]db.FactCheckMatch, evidence map[string][]db.ItemEvidenceWithSource, logger *zerolog.Logger) *digestRenderContext {
	displayStart := start
	displayEnd := end

	if loc, ok := s.resolveScheduleLocation(ctx, logger); ok {
		displayStart = start.In(loc)
		displayEnd = end.In(loc)
	}

	return &digestRenderContext{
		scheduler:     s,
		llmClient:     s.llmClient,
		settings:      settings,
		items:         items,
		clusters:      clusters,
		start:         start,
		end:           end,
		displayStart:  displayStart,
		displayEnd:    displayEnd,
		seenSummaries: make(map[string]bool),
		factChecks:    factChecks,
		evidence:      evidence,
		logger:        logger,
	}
}

func (s *Scheduler) resolveScheduleLocation(ctx context.Context, logger *zerolog.Logger) (*time.Location, bool) {
	var sched schedule.Schedule
	if err := s.database.GetSetting(ctx, schedule.SettingDigestSchedule, &sched); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_schedule for timezone")
		return nil, false
	}

	if sched.IsEmpty() {
		return nil, false
	}

	if err := sched.Validate(); err != nil {
		logger.Debug().Err(err).Msg(errInvalidScheduleTimezone)
		return nil, false
	}

	loc, err := sched.Location()
	if err != nil {
		logger.Debug().Err(err).Msg(errInvalidScheduleTimezone)
		return nil, false
	}

	return loc, true
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
	fmt.Fprintf(sb, "📰 <b>%s</b> • %s - %s\n", html.EscapeString(header), rc.displayStart.Format(TimeFormatHourMinute), rc.displayEnd.Format(TimeFormatHourMinute))
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

// convertEvidenceForLLM converts database evidence to LLM-compatible format.
func (rc *digestRenderContext) convertEvidenceForLLM(items []db.Item) llm.ItemEvidence {
	result := make(llm.ItemEvidence)

	for _, item := range items {
		if ev, ok := rc.evidence[item.ID]; ok && len(ev) > 0 {
			sources := make([]llm.EvidenceSource, 0, len(ev))

			for _, e := range ev {
				sources = append(sources, llm.EvidenceSource{
					URL:             e.Source.URL,
					Domain:          e.Source.Domain,
					Title:           e.Source.Title,
					AgreementScore:  e.AgreementScore,
					IsContradiction: e.IsContradiction,
				})
			}

			result[item.ID] = sources
		}
	}

	return result
}

func (rc *digestRenderContext) generateNarrative(ctx context.Context, sb *strings.Builder) bool {
	if !rc.settings.editorEnabled || rc.settings.smartLLMModel == "" {
		return false
	}

	evidence := rc.convertEvidenceForLLM(rc.items)

	narrative, err := rc.llmClient.GenerateNarrativeWithEvidence(ctx, rc.items, evidence, rc.settings.digestLanguage, rc.settings.smartLLMModel, rc.settings.digestTone)
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
				formatted := rc.scheduler.formatItems(c.Items, true, rc.seenSummaries, rc.factChecks)
				if formatted != "" {
					hasContent = true

					groupSb.WriteString(formatted)
				}
			}
		}
	} else {
		formatted := rc.scheduler.formatItems(group.items, true, rc.seenSummaries, rc.factChecks)
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

// renderOthersAsNarrative generates an LLM narrative summary for the "others" section instead of listing items individually.
func (rc *digestRenderContext) renderOthersAsNarrative(ctx context.Context, sb *strings.Builder, group clusterGroup, emoji, title string) bool {
	allItems := collectAllItems(group)
	if len(allItems) == 0 {
		return false
	}

	model := rc.getNarrativeModel()
	if model == "" {
		rc.renderGroup(ctx, sb, group, emoji, title)

		return true
	}

	// Generate narrative for "others" items with evidence context
	evidence := rc.convertEvidenceForLLM(allItems)
	narrative, err := rc.llmClient.SummarizeClusterWithEvidence(ctx, allItems, evidence, rc.settings.digestLanguage, model, rc.settings.digestTone)

	if err != nil || narrative == "" {
		if err != nil {
			rc.logger.Warn().Err(err).Msg("failed to generate others narrative, falling back to detailed list")
		}

		rc.renderGroup(ctx, sb, group, emoji, title)

		return true
	}

	rc.renderNarrativeSection(sb, narrative, emoji, title, allItems)

	return true
}

func (rc *digestRenderContext) getNarrativeModel() string {
	model := rc.settings.smartLLMModel
	if model == "" {
		model = rc.scheduler.cfg.LLMModel
	}

	return model
}

func collectAllItems(group clusterGroup) []db.Item {
	totalItems := len(group.items)
	for _, c := range group.clusters {
		totalItems += len(c.Items)
	}

	if totalItems == 0 {
		return nil
	}

	allItems := make([]db.Item, 0, totalItems)

	for _, c := range group.clusters {
		allItems = append(allItems, c.Items...)
	}

	allItems = append(allItems, group.items...)

	return allItems
}

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
}

func (rc *digestRenderContext) renderMultiItemCluster(ctx context.Context, sb *strings.Builder, c db.ClusterWithItems) bool {
	if rc.settings.consolidatedClustersEnabled {
		return rc.renderConsolidatedCluster(ctx, sb, c)
	}

	return rc.renderRepresentativeCluster(sb, c)
}

func (rc *digestRenderContext) renderConsolidatedCluster(ctx context.Context, sb *strings.Builder, c db.ClusterWithItems) bool {
	model := rc.getNarrativeModel()

	evidence := rc.convertEvidenceForLLM(c.Items)
	summary, err := rc.llmClient.SummarizeClusterWithEvidence(ctx, c.Items, evidence, rc.settings.digestLanguage, model, rc.settings.digestTone)

	if err != nil || summary == "" {
		if err != nil {
			rc.logger.Warn().Err(err).Str("cluster", c.Topic).Msg("failed to summarize cluster, falling back to detailed list")
		}

		return rc.renderRepresentativeCluster(sb, c)
	}

	summary = htmlutils.SanitizeHTML(summary)
	if rc.seenSummaries[summary] {
		return false
	}

	rc.seenSummaries[summary] = true

	rc.renderConsolidatedSummary(sb, summary, c)

	return true
}

func (rc *digestRenderContext) renderConsolidatedSummary(sb *strings.Builder, summary string, c db.ClusterWithItems) {
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

	if rc.factChecks != nil {
		if match, ok := findFactCheckMatch(c.Items, rc.factChecks); ok {
			sb.WriteString(formatFactCheckLine(match))
		}
	}

	if line := rc.scheduler.buildCorroborationLine(c.Items, c.Items[0]); line != "" {
		sb.WriteString(line)
	}

	rc.appendEvidenceLine(sb, c.Items)

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n")
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

	if rc.factChecks != nil {
		if match, ok := findFactCheckMatch(c.Items, rc.factChecks); ok {
			sb.WriteString(formatFactCheckLine(match))
		}
	}

	if line := rc.scheduler.buildCorroborationLine(c.Items, representative); line != "" {
		sb.WriteString(line)
	}

	rc.appendEvidenceLine(sb, c.Items)

	if len(c.Items) > 1 {
		fmt.Fprintf(sb, " <i>(+%d related)</i>", len(c.Items)-1)
	}

	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n\n")

	return true
}

func (rc *digestRenderContext) appendEvidenceLine(sb *strings.Builder, items []db.Item) {
	evidenceList := findEvidenceForItems(items, rc.evidence)
	if len(evidenceList) == 0 {
		return
	}

	// Determine tier from evidence count and average score
	tier := determineTierFromEvidence(evidenceList)
	if tier != "" {
		sb.WriteString(formatConfidenceTier(tier, len(evidenceList)))
	}

	// Append evidence bullets (Phase 2)
	for i, ev := range evidenceList {
		if i >= 3 { // Limit to top 3 evidence bullets
			break
		}

		fmt.Fprintf(sb, "\n    • %s", html.EscapeString(ev.Source.Title))

		if ev.Source.Domain != "" {
			fmt.Fprintf(sb, " <i>(%s)</i>", html.EscapeString(ev.Source.Domain))
		}
	}
}

func determineTierFromEvidence(evidenceList []db.ItemEvidenceWithSource) string {
	if len(evidenceList) == 0 {
		return ""
	}

	var totalScore float32

	for _, ev := range evidenceList {
		totalScore += ev.AgreementScore
	}

	avgScore := totalScore / float32(len(evidenceList))

	const (
		highTierMinSources = 2
		highTierMinScore   = 0.5
		mediumTierMinScore = 0.3
	)

	if len(evidenceList) >= highTierMinSources && avgScore >= highTierMinScore {
		return db.FactCheckTierHigh
	}

	if avgScore >= mediumTierMinScore {
		return db.FactCheckTierMedium
	}

	return db.FactCheckTierLow
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

func findEvidenceForItems(items []db.Item, evidence map[string][]db.ItemEvidenceWithSource) []db.ItemEvidenceWithSource {
	if evidence == nil {
		return nil
	}

	for _, item := range items {
		if item.ID == "" {
			continue
		}

		if ev, ok := evidence[item.ID]; ok && len(ev) > 0 {
			return ev
		}
	}

	return nil
}

func formatConfidenceTier(tier string, sourceCount int) string {
	if tier == "" {
		return ""
	}

	var emoji string

	switch tier {
	case db.FactCheckTierHigh:
		emoji = "✅"
	case db.FactCheckTierMedium:
		emoji = "🔵"
	case db.FactCheckTierLow:
		emoji = "⚪"
	default:
		return ""
	}

	return fmt.Sprintf("\n    ↳ <i>%s Corroborated (%d sources)</i>", emoji, sourceCount)
}
