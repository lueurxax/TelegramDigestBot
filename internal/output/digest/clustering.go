package digest

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/process/dedup"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// URL normalization constants.
const wwwPrefix = "www."

// Topic similarity threshold for Jaccard index.
const topicJaccardThreshold = 0.8

func (s *Scheduler) clusterItems(ctx context.Context, items []db.Item, start, end time.Time, logger *zerolog.Logger) error {
	return s.clusterItemsWithSource(ctx, items, start, end, db.ClusterSourceDigest, logger)
}

func (s *Scheduler) clusterItemsWithSource(ctx context.Context, items []db.Item, start, end time.Time, source string, logger *zerolog.Logger) error {
	if len(items) == 0 {
		return nil
	}

	items = s.limitClusterItems(items, logger)

	// Clean up old clusters for this window to prevent duplicates on retries
	if err := s.database.DeleteClustersForWindowAndSource(ctx, start, end, source); err != nil {
		logger.Error().Err(err).Msg("failed to delete old clusters")
	}

	minClusterSize := 2
	if source == db.ClusterSourceResearch {
		minClusterSize = 1
	}

	cfg := s.getClusteringConfig(ctx, logger)
	topicIndex, topicGroups := s.buildTopicGrouping(items)
	clusterCtx := &clusterBuildContext{
		topicIndex:  topicIndex,
		topicGroups: topicGroups,
		embeddings:  s.getEmbeddings(ctx, items, logger),
		evidenceMap: s.getEvidenceMap(ctx, items, cfg, logger),
		assigned:    make(map[string]bool),
		allItems:    items,
		cfg:         cfg,
	}

	for topic, groupItems := range clusterCtx.topicGroups {
		if err := s.processTopicGroup(ctx, topic, groupItems, clusterCtx, start, end, source, minClusterSize, logger); err != nil {
			return err
		}
	}

	return nil
}

type clusterBuildContext struct {
	topicIndex  map[string]string
	topicGroups map[string][]db.Item
	embeddings  map[string][]float32
	evidenceMap map[string][]db.ItemEvidenceWithSource
	assigned    map[string]bool
	allItems    []db.Item
	cfg         clusteringConfig
}

func (s *Scheduler) limitClusterItems(items []db.Item, logger *zerolog.Logger) []db.Item {
	if len(items) > ClusterMaxItemsLimit {
		logger.Warn().
			Int(LogFieldCount, len(items)).
			Int("limit", ClusterMaxItemsLimit).
			Msg("Too many items to cluster, limiting to first items")

		return items[:ClusterMaxItemsLimit]
	}

	return items
}

func (s *Scheduler) processTopicGroup(ctx context.Context, topic string, groupItems []db.Item, bc *clusterBuildContext, start, end time.Time, source string, minClusterSize int, logger *zerolog.Logger) error {
	for _, itemA := range groupItems {
		if bc.assigned[itemA.ID] {
			continue
		}

		clusterItemsList := s.findClusterItems(itemA, groupItems, bc.allItems, bc.assigned, bc.embeddings, bc.topicIndex, bc.evidenceMap, bc.cfg)
		bc.assigned[itemA.ID] = true

		clusterItemsList = s.validateClusterCoherence(clusterItemsList, bc, logger)
		if len(clusterItemsList) < minClusterSize {
			continue
		}

		if err := s.persistCluster(ctx, clusterItemsList, topic, bc.cfg, start, end, source, logger); err != nil {
			return err
		}
	}

	return nil
}

func (s *Scheduler) validateClusterCoherence(clusterItemsList []db.Item, bc *clusterBuildContext, logger *zerolog.Logger) []db.Item {
	coherence := s.calculateCoherence(clusterItemsList, bc.embeddings)

	if len(clusterItemsList) > 2 && coherence < bc.cfg.coherenceThreshold {
		logger.Debug().Float32("coherence", coherence).Int("size", len(clusterItemsList)).Msg("Rejecting cluster due to low coherence")

		for _, item := range clusterItemsList[1:] {
			bc.assigned[item.ID] = false
		}

		return clusterItemsList[:1]
	}

	return clusterItemsList
}

func (s *Scheduler) persistCluster(ctx context.Context, clusterItemsList []db.Item, topic string, cfg clusteringConfig, start, end time.Time, source string, logger *zerolog.Logger) error {
	s.sortClusterItems(clusterItemsList)

	logger.Debug().
		Int("cluster_size", len(clusterItemsList)).
		Str("representative", clusterItemsList[0].ID).
		Float32("rep_importance", clusterItemsList[0].ImportanceScore).
		Msg("Cluster representative selected")

	clusterTopic := s.generateClusterTopic(ctx, clusterItemsList, topic, cfg.digestLanguage)

	clusterID, err := s.database.CreateClusterWithSource(ctx, start, end, clusterTopic, source)
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	for _, it := range clusterItemsList {
		if err := s.database.AddToCluster(ctx, clusterID, it.ID); err != nil {
			logger.Error().Err(err).Msg("failed to add item to cluster")
		}
	}

	return nil
}

// ClusterItemsForResearch clusters items for research analytics without affecting digest output.
func (s *Scheduler) ClusterItemsForResearch(ctx context.Context, items []db.Item, start, end time.Time, logger *zerolog.Logger) error {
	return s.clusterItemsWithSource(ctx, items, start, end, db.ClusterSourceResearch, logger)
}

type clusteringConfig struct {
	similarityThreshold  float64
	crossTopicEnabled    bool
	crossTopicThreshold  float64
	coherenceThreshold   float32
	clusterWindow        time.Duration
	digestLanguage       string
	evidenceEnabled      bool
	evidenceBoost        float32
	evidenceMinAgreement float32
}

func (s *Scheduler) getClusteringConfig(ctx context.Context, logger *zerolog.Logger) clusteringConfig {
	cfg := clusteringConfig{
		similarityThreshold:  float64(s.cfg.ClusterSimilarityThreshold),
		crossTopicEnabled:    s.cfg.CrossTopicClusteringEnabled,
		crossTopicThreshold:  float64(s.cfg.CrossTopicSimilarityThreshold),
		coherenceThreshold:   float32(s.cfg.ClusterCoherenceThreshold),
		evidenceEnabled:      true,
		evidenceBoost:        s.cfg.EvidenceClusteringBoost,
		evidenceMinAgreement: s.cfg.EvidenceClusteringMinScore,
	}

	s.loadClusteringConfigFromDB(ctx, logger, &cfg)
	s.applyClusteringDefaults(&cfg)

	return cfg
}

func (s *Scheduler) loadClusteringConfigFromDB(ctx context.Context, logger *zerolog.Logger, cfg *clusteringConfig) {
	loadSetting := func(key string, target interface{}, logMsg string) {
		if err := s.database.GetSetting(ctx, key, target); err != nil {
			logger.Debug().Err(err).Msg(logMsg)
		}
	}

	loadSetting("cluster_similarity_threshold", &cfg.similarityThreshold, "could not get cluster_similarity_threshold from DB")
	loadSetting("cross_topic_clustering_enabled", &cfg.crossTopicEnabled, "could not get cross_topic_clustering_enabled from DB")
	loadSetting("cross_topic_similarity_threshold", &cfg.crossTopicThreshold, "could not get cross_topic_similarity_threshold from DB")

	var coherence float64

	loadSetting("cluster_coherence_threshold", &coherence, "could not get cluster_coherence_threshold from DB")

	if coherence > 0 {
		cfg.coherenceThreshold = float32(coherence)
	}

	var clusterWindowHours float64

	loadSetting("cluster_time_window_hours", &clusterWindowHours, "could not get cluster_time_window_hours from DB")

	if clusterWindowHours > 0 {
		cfg.clusterWindow = time.Duration(clusterWindowHours) * time.Hour
	}

	loadSetting(SettingDigestLanguage, &cfg.digestLanguage, MsgCouldNotGetDigestLanguage)
}

func (s *Scheduler) applyClusteringDefaults(cfg *clusteringConfig) {
	if cfg.similarityThreshold <= 0 {
		cfg.similarityThreshold = float64(s.cfg.ClusterSimilarityThreshold)
	}

	if cfg.crossTopicThreshold <= 0 {
		cfg.crossTopicThreshold = cfg.similarityThreshold
	}

	if cfg.coherenceThreshold <= 0 {
		cfg.coherenceThreshold = float32(ClusterDefaultCoherenceThreshold)
	}
}

func (s *Scheduler) buildTopicGrouping(items []db.Item) (map[string]string, map[string][]db.Item) {
	topicIndex := make(map[string]string)
	topicGroups := make(map[string][]db.Item)
	canonicalTopics := make([]string, 0)

	for _, item := range items {
		normalized := normalizeClusterTopic(item.Topic)

		canonical := canonicalizeTopic(normalized, canonicalTopics)
		if canonical != "" {
			if canonical == normalized {
				canonicalTopics = append(canonicalTopics, canonical)
			}
		}

		topicIndex[item.ID] = canonical
		topicGroups[canonical] = append(topicGroups[canonical], item)
	}

	return topicIndex, topicGroups
}

// getTopicIndex returns a mapping of item ID to normalized topic.
func (s *Scheduler) getTopicIndex(items []db.Item) map[string]string {
	idx, _ := s.buildTopicGrouping(items)
	return idx
}

// getTopicGroups returns items grouped by normalized topic.
func (s *Scheduler) getTopicGroups(items []db.Item) map[string][]db.Item {
	_, groups := s.buildTopicGrouping(items)
	return groups
}

func (s *Scheduler) getEmbeddings(ctx context.Context, items []db.Item, logger *zerolog.Logger) map[string][]float32 {
	embeddings := make(map[string][]float32)

	for _, item := range items {
		if len(item.Embedding) > 0 {
			embeddings[item.ID] = item.Embedding
			continue
		}

		emb, err := s.database.GetItemEmbedding(ctx, item.ID)
		if err != nil {
			logger.Warn().Str(logFieldItemID, item.ID).Err(err).Msg("failed to get embedding for item")
			continue
		}

		embeddings[item.ID] = emb
	}

	return embeddings
}

func (s *Scheduler) getEvidenceMap(ctx context.Context, items []db.Item, cfg clusteringConfig, logger *zerolog.Logger) map[string][]db.ItemEvidenceWithSource {
	if !cfg.evidenceEnabled {
		return nil
	}

	itemIDs := make([]string, len(items))
	for i, item := range items {
		itemIDs[i] = item.ID
	}

	evidenceMap, err := s.database.GetEvidenceForItems(ctx, itemIDs)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get evidence for items, proceeding without evidence boost")
		return nil
	}

	if len(evidenceMap) > 0 {
		logger.Debug().Int(LogFieldItemsWithEvidence, len(evidenceMap)).Msg("loaded evidence for clustering")
	}

	return evidenceMap
}

func (s *Scheduler) findClusterItems(itemA db.Item, groupItems, allItems []db.Item, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig) []db.Item {
	clusterItemsList := []db.Item{itemA}

	embA, okA := embeddings[itemA.ID]
	if !okA {
		return clusterItemsList
	}

	candidateItems := groupItems
	if cfg.crossTopicEnabled {
		candidateItems = allItems
	}

	topicA := topicIndex[itemA.ID]

	for _, itemB := range candidateItems {
		if s.shouldAddToCluster(itemA, itemB, topicA, assigned, embeddings, topicIndex, evidenceMap, cfg, embA) {
			clusterItemsList = append(clusterItemsList, itemB)
			assigned[itemB.ID] = true
		}
	}

	return clusterItemsList
}

func (s *Scheduler) shouldAddToCluster(itemA, itemB db.Item, topicA string, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig, embA []float32) bool {
	embB, topicB, ok := s.validateClusterCandidate(itemA, itemB, topicA, assigned, embeddings, topicIndex, cfg)
	if !ok {
		return false
	}

	threshold := getClusterThreshold(topicA, topicB, cfg)
	similarity := calculateBoostedSimilarity(itemA.ID, itemB.ID, embA, embB, evidenceMap, cfg)

	return similarity > float32(threshold)
}

func (s *Scheduler) validateClusterCandidate(itemA, itemB db.Item, topicA string, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, cfg clusteringConfig) ([]float32, string, bool) {
	if itemB.ID == itemA.ID || assigned[itemB.ID] {
		return nil, "", false
	}

	embB, okB := embeddings[itemB.ID]
	if !okB {
		return nil, "", false
	}

	topicB := topicIndex[itemB.ID]

	if !cfg.crossTopicEnabled && topicA != topicB {
		return nil, "", false
	}

	if cfg.clusterWindow > 0 && !withinClusterWindow(itemA.TGDate, itemB.TGDate, cfg.clusterWindow) {
		return nil, "", false
	}

	return embB, topicB, true
}

func getClusterThreshold(topicA, topicB string, cfg clusteringConfig) float64 {
	if topicA != topicB {
		return cfg.crossTopicThreshold
	}

	return cfg.similarityThreshold
}

func calculateBoostedSimilarity(itemAID, itemBID string, embA, embB []float32, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig) float32 {
	similarity := dedup.CosineSimilarity(embA, embB)

	if cfg.evidenceEnabled && evidenceMap != nil {
		boost := calculateEvidenceBoost(itemAID, itemBID, evidenceMap, cfg)
		similarity += boost
	}

	return similarity
}

// calculateEvidenceBoost calculates a similarity boost when two items share
// evidence sources with high agreement scores. This helps cluster items that
// are reporting on the same underlying story from different angles.
func calculateEvidenceBoost(itemAID, itemBID string, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig) float32 {
	evidenceA := evidenceMap[itemAID]
	evidenceB := evidenceMap[itemBID]

	if len(evidenceA) == 0 || len(evidenceB) == 0 {
		return 0
	}

	evidenceAURLs := buildEvidenceURLMap(evidenceA, cfg.evidenceMinAgreement)
	if len(evidenceAURLs) == 0 {
		return 0
	}

	maxAgreement := findMaxSharedAgreement(evidenceB, evidenceAURLs, cfg.evidenceMinAgreement)
	if maxAgreement == 0 {
		return 0
	}

	boost := maxAgreement * cfg.evidenceBoost
	if boost > cfg.evidenceBoost {
		boost = cfg.evidenceBoost
	}

	return boost
}

// buildEvidenceURLMap creates a map of normalized URLs to agreement scores for evidence
// that meets the minimum agreement threshold. URLs are normalized to treat www vs non-www as same.
func buildEvidenceURLMap(evidence []db.ItemEvidenceWithSource, minAgreement float32) map[string]float32 {
	urlMap := make(map[string]float32)

	for _, ev := range evidence {
		if ev.AgreementScore >= minAgreement {
			normalizedURL := normalizeURLForDedup(ev.Source.URL)
			urlMap[normalizedURL] = ev.AgreementScore
		}
	}

	return urlMap
}

// findMaxSharedAgreement finds the maximum agreement score among shared evidence
// sources between two items. URLs are normalized to treat www vs non-www as same.
func findMaxSharedAgreement(evidenceB []db.ItemEvidenceWithSource, evidenceAURLs map[string]float32, minAgreement float32) float32 {
	var maxAgreement float32

	for _, ev := range evidenceB {
		if ev.AgreementScore < minAgreement {
			continue
		}

		normalizedURL := normalizeURLForDedup(ev.Source.URL)
		if scoreA, ok := evidenceAURLs[normalizedURL]; ok {
			minScore := minFloat32(scoreA, ev.AgreementScore)
			if minScore > maxAgreement {
				maxAgreement = minScore
			}
		}
	}

	return maxAgreement
}

// normalizeURLForDedup normalizes a URL for deduplication purposes.
// Removes www. prefix from the host to treat www.example.com and example.com as the same.
func normalizeURLForDedup(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// Quick check for www. - if not present, return as-is
	if !strings.Contains(rawURL, wwwPrefix) {
		return rawURL
	}

	// Parse and normalize
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	parsed.Host = normalizeDomain(parsed.Host)

	return parsed.String()
}

// normalizeDomain removes www. prefix from domain for deduplication.
func normalizeDomain(domain string) string {
	return strings.TrimPrefix(strings.TrimPrefix(domain, wwwPrefix), "WWW.")
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}

	return b
}

func (s *Scheduler) sortClusterItems(clusterItemsList []db.Item) {
	sort.Slice(clusterItemsList, func(i, j int) bool {
		if clusterItemsList[i].ImportanceScore != clusterItemsList[j].ImportanceScore {
			return clusterItemsList[i].ImportanceScore > clusterItemsList[j].ImportanceScore
		}

		return len(clusterItemsList[i].Summary) > len(clusterItemsList[j].Summary)
	})
}

func (s *Scheduler) generateClusterTopic(ctx context.Context, clusterItemsList []db.Item, defaultTopic, digestLanguage string) string {
	if len(clusterItemsList) <= 1 {
		return defaultTopic
	}

	// Augment vague summaries with link context for better topic generation
	augmentedItems := s.augmentClusterItemsForTopic(ctx, clusterItemsList)

	// Pass empty model to let the LLM registry handle task-specific model selection
	// via LLM_CLUSTER_MODEL env var or default task config
	if s.llmClient == nil {
		return defaultTopic
	}

	if betterTopic, err := s.llmClient.GenerateClusterTopic(ctx, augmentedItems, digestLanguage, ""); err == nil && betterTopic != "" {
		return betterTopic
	}

	return defaultTopic
}

func (s *Scheduler) augmentClusterItemsForTopic(ctx context.Context, items []db.Item) []db.Item {
	augmentedItems := make([]db.Item, len(items))
	useLinks := strings.Contains(s.cfg.LinkEnrichmentScope, domain.ScopeTopic)

	for i, item := range items {
		augmentedItems[i] = item
		if useLinks && len(item.Summary) < domain.ShortMessageThreshold {
			augmentedItems[i].Summary = s.augmentItemSummaryWithLinks(ctx, item)
		}
	}

	return augmentedItems
}

func (s *Scheduler) augmentItemSummaryWithLinks(ctx context.Context, item db.Item) string {
	links, err := s.database.GetLinksForMessage(ctx, item.RawMessageID)
	if err != nil || len(links) == 0 {
		return item.Summary
	}

	var sb strings.Builder

	sb.WriteString(item.Summary)
	sb.WriteString(" (Context: ")

	for j, link := range links {
		if j > 0 {
			sb.WriteString(" | ")
		}

		title := link.Title
		if title == "" {
			title = link.Domain
		}

		sb.WriteString(title)

		if j >= 2 { // limit to 3 links context
			break
		}
	}

	sb.WriteString(")")

	return sb.String()
}

func (s *Scheduler) calculateCoherence(items []db.Item, embeddings map[string][]float32) float32 {
	if len(items) < 2 {
		return PerfectSimilarityScore
	}

	var sum float32

	var count int

	for i, itemI := range items {
		for _, itemJ := range items[i+1:] {
			embI, okI := embeddings[itemI.ID]
			embJ, okJ := embeddings[itemJ.ID]

			if okI && okJ {
				sum += dedup.CosineSimilarity(embI, embJ)
				count++
			}
		}
	}

	if count == 0 {
		return 0
	}

	return sum / float32(count)
}

func normalizeClusterTopic(topic string) string {
	normalized := strings.TrimSpace(strings.ToLower(topic))
	if normalized == "" {
		return DefaultTopic
	}

	if mapped, ok := topicSynonyms[normalized]; ok {
		return mapped
	}

	return strings.TrimSpace(cases.Title(language.English).String(normalized))
}

var topicSynonyms = map[string]string{
	"ukraine": "Ukraine",
	"украина": "Ukraine",
	"україна": "Ukraine",
	"russia":  "Russia",
	"россия":  "Russia",
	"росія":   "Russia",
	"cyprus":  "Cyprus",
	"кипр":    "Cyprus",
}

func canonicalizeTopic(topic string, canon []string) string {
	if topic == "" {
		return ""
	}

	for _, existing := range canon {
		if topicsSimilar(topic, existing) {
			return existing
		}
	}

	return topic
}

func topicsSimilar(a, b string) bool {
	if a == b {
		return true
	}

	aTokens := tokenizeTopic(a)

	bTokens := tokenizeTopic(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return false
	}

	intersection := 0

	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			intersection++
		}
	}

	union := len(aTokens) + len(bTokens) - intersection
	if union == 0 {
		return false
	}

	jaccard := float64(intersection) / float64(union)

	return jaccard >= topicJaccardThreshold
}

func tokenizeTopic(topic string) map[string]struct{} {
	out := make(map[string]struct{})

	for _, token := range strings.Fields(strings.ToLower(topic)) {
		token = strings.Trim(token, ".,:;!()[]{}\"'")
		if token != "" {
			out[token] = struct{}{}
		}
	}

	return out
}

func withinClusterWindow(a, b time.Time, window time.Duration) bool {
	if a.IsZero() || b.IsZero() {
		return true
	}

	diff := a.Sub(b)
	if diff < 0 {
		diff = -diff
	}

	return diff <= window
}
