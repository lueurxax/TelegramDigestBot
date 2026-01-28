// Package settings provides centralized setting key constants used across the application.
// All database setting keys should be defined here to avoid duplication and ensure consistency.
package settings

// Core digest settings
const (
	// TargetChatID is the Telegram chat ID where digests are sent.
	TargetChatID = "target_chat_id"
	// DigestWindow is the time window for digest collection.
	DigestWindow = "digest_window"
	// DigestLanguage is the target language for digest output.
	DigestLanguage = "digest_language"
	// DigestSchedule is the cron-like schedule for digest generation.
	DigestSchedule = "digest_schedule"
	// DigestScheduleAnchor is the anchor time for schedule alignment.
	DigestScheduleAnchor = "digest_schedule_anchor"
)

// Threshold settings
const (
	// RelevanceThreshold is the minimum relevance score for items.
	RelevanceThreshold = "relevance_threshold"
	// ImportanceThreshold is the minimum importance score for items.
	ImportanceThreshold = "importance_threshold"
)

// Filter settings
const (
	// FiltersAds enables ad filtering.
	FiltersAds = "filters_ads"
	// FiltersAdsKeywords contains the list of ad-related keywords.
	FiltersAdsKeywords = "filters_ads_keywords"
	// FiltersSkipForwards enables skipping forwarded messages.
	FiltersSkipForwards = "filters_skip_forwards"
)

// Discovery settings
const (
	// DiscoveryMinSeen is the minimum times a channel must be seen.
	DiscoveryMinSeen = "discovery_min_seen"
	// DiscoveryMinEngagement is the minimum engagement score.
	DiscoveryMinEngagement = "discovery_min_engagement"
	// DiscoveryDescriptionAllow contains allowed keywords in descriptions.
	DiscoveryDescriptionAllow = "discovery_description_allow"
	// DiscoveryDescriptionDeny contains denied keywords in descriptions.
	DiscoveryDescriptionDeny = "discovery_description_deny"
)

// LLM override settings
const (
	// LLMOverrideSummarize overrides the model for summarization tasks.
	LLMOverrideSummarize = "llm_override_summarize"
	// LLMOverrideCluster overrides the model for clustering tasks.
	LLMOverrideCluster = "llm_override_cluster"
	// LLMOverrideNarrative overrides the model for narrative generation.
	LLMOverrideNarrative = "llm_override_narrative"
	// LLMOverrideTopic overrides the model for topic generation.
	LLMOverrideTopic = "llm_override_topic"
	// LLMDailyBudget sets the daily token budget limit.
	LLMDailyBudget = "llm_daily_budget"
)

// Feature toggle settings
const (
	// EditorEnabled enables the editor mode.
	EditorEnabled = "editor_enabled"
	// TieredImportanceEnabled enables tiered importance display.
	TieredImportanceEnabled = "tiered_importance_enabled"
	// VisionRoutingEnabled enables vision-based routing.
	VisionRoutingEnabled = "vision_routing_enabled"
	// ConsolidatedClustersEnabled enables consolidated cluster display.
	ConsolidatedClustersEnabled = "consolidated_clusters_enabled"
	// EditorDetailedItems enables detailed item display in editor.
	EditorDetailedItems = "editor_detailed_items"
	// OthersAsNarrative renders low-priority items as narrative.
	OthersAsNarrative = "others_as_narrative"
)

// Cover image settings
const (
	// DigestCoverImage enables cover images for digests.
	DigestCoverImage = "digest_cover_image"
	// DigestAICover enables AI-generated covers.
	DigestAICover = "digest_ai_cover"
	// DigestInlineImages enables inline images in digest.
	DigestInlineImages = "digest_inline_images"
)

// Link processing settings
const (
	// MaxLinksPerMessage limits links processed per message.
	MaxLinksPerMessage = "max_links_per_message"
	// LinkCacheTTL is the TTL for web link cache.
	LinkCacheTTL = "link_cache_ttl"
	// TgLinkCacheTTL is the TTL for Telegram link cache.
	TgLinkCacheTTL = "tg_link_cache_ttl"
)

// Enrichment settings
const (
	// EnrichmentAllowDomains contains allowed domains for enrichment.
	EnrichmentAllowDomains = "enrichment_allow_domains"
	// EnrichmentDenyDomains contains denied domains for enrichment.
	EnrichmentDenyDomains = "enrichment_deny_domains"
)

// Prompt template settings
const (
	// PromptActiveKeyFmt is the format for active prompt key.
	PromptActiveKeyFmt = "prompt:%s:active"
	// PromptKeyFmt is the format for prompt version key.
	PromptKeyFmt = "prompt:%s:%s"
)

// Weekly task tracking settings
const (
	// WeeklyWeightUpdateRun tracks last weight update run.
	WeeklyWeightUpdateRun = "weekly_weight_update_run"
	// WeeklyRelevanceUpdateRun tracks last relevance update run.
	WeeklyRelevanceUpdateRun = "weekly_relevance_update_run"
	// WeeklyThresholdTuningRun tracks last threshold tuning run.
	WeeklyThresholdTuningRun = "weekly_threshold_tuning_run"
	// WeeklyRatingStatsUpdateRun tracks last rating stats update run.
	WeeklyRatingStatsUpdateRun = "weekly_rating_stats_update_run"
)
