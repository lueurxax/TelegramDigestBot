package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MessagesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_messages_ingested_total",
		Help: "The total number of ingested messages",
	}, []string{"channel"})

	ReaderMessagesSeen = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_messages_seen_total",
		Help: "The total number of messages seen by the reader",
	}, []string{"channel"})

	PipelineProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_pipeline_processed_total",
		Help: "The total number of messages processed by the pipeline",
	}, []string{"status"})

	LLMRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_llm_request_duration_seconds",
		Help:    "Duration of LLM requests",
		Buckets: prometheus.DefBuckets,
	}, []string{"model"})

	PipelineBacklog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_pipeline_backlog_size",
		Help: "Number of unprocessed messages in the database",
	})

	PipelineBacklogOldestAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_pipeline_backlog_oldest_age_seconds",
		Help: "Age in seconds of the oldest unprocessed message in the pipeline",
	})

	PipelineMessageAgeSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_pipeline_message_age_seconds",
		Help:    "Age of messages when pipeline processing starts",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400, 172800, 604800},
	})

	PipelineMessageAgeSecondsByKind = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_pipeline_message_age_seconds_by_kind",
		Help:    "Age of messages when pipeline processing starts by kind (native or forwarded)",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400, 172800, 604800},
	}, []string{"kind"})

	PipelineBatchDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_pipeline_batch_duration_seconds",
		Help:    "Duration in seconds to process a pipeline batch",
		Buckets: []float64{1, 2, 5, 10, 20, 30, 60, 120, 300},
	})

	AnnotationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_item_annotations_total",
		Help: "Total number of item annotations by rating",
	}, []string{"rating"})

	AnnotationRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_item_annotation_requests_total",
		Help: "Total number of annotation API requests by status",
	}, []string{"status"})

	AnnotationBatchTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_item_annotation_batch_total",
		Help: "Total number of annotation batch submissions",
	})

	AnnotationRequestDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_item_annotation_request_duration_seconds",
		Help:    "Duration of annotation API requests in seconds",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})

	DigestsPosted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_posts_total",
		Help: "The total number of digests posted",
	}, []string{"status"})

	DigestTimeToDigestSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_time_to_digest_seconds",
		Help:    "Time from message timestamp to digest inclusion",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400},
	})

	DigestAverageImportance = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_average_importance",
		Help: "Average importance score for items in a digest window",
	})

	DigestAverageRelevance = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_average_relevance",
		Help: "Average relevance score for items in a digest window",
	})

	DigestReadyItems = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_ready_items",
		Help: "Number of items selected for a digest window",
	})

	DropsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_drops_total",
		Help: "Total number of dropped messages by reason",
	}, []string{"reason"})

	LinkContextUsedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_link_context_used_total",
		Help: "Total number of items that used resolved link context in summarization",
	})

	LinkLanguageQueriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_link_language_queries_total",
		Help: "Total number of enrichment queries generated from link language content",
	})

	CanonicalSourceDetectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_canonical_source_detected_total",
		Help: "Total number of items with a trusted canonical source detected",
	})

	ItemRatingsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_item_ratings_total",
		Help: "Total number of item ratings by rating value",
	}, []string{"rating"})

	AnnotationBiasAppliedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_annotation_bias_applied_total",
		Help: "Total number of items adjusted by annotation-driven channel bias",
	}, []string{"channel"})

	IrrelevantSimilarityHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_irrelevant_similarity_hits_total",
		Help: "Total number of items with similarity to irrelevant-rated items",
	})

	IrrelevantSimilarityRejectsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_irrelevant_similarity_rejects_total",
		Help: "Total number of items rejected due to irrelevant similarity",
	})

	IrrelevantSimilarityScore = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_irrelevant_similarity_score",
		Help:    "Similarity scores for items matched against irrelevant ratings",
		Buckets: []float64{0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.92, 0.94, 0.96, 0.98, 1.0},
	})

	UncertaintyFlaggedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_uncertainty_flagged_total",
		Help: "Total number of items flagged as needs-review",
	})

	LowReliabilityBadgeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_low_reliability_badge_total",
		Help: "Total number of items marked with low-reliability badges",
	})

	LowSignalRate = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_low_signal_rate",
		Help: "Estimated low-signal rate in recent windows",
	})

	BulletabilityDecisionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_bulletability_decision_total",
		Help: "Total number of bulletability decisions by result and source",
	}, []string{"result", "source"})

	BulletabilityScore = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_bulletability_score",
		Help:    "Deterministic bulletability score distribution",
		Buckets: []float64{0.1, 0.2, 0.35, 0.5, 0.65, 0.8, 1.0},
	})

	BulletDedupBeforeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_bullet_dedup_before_total",
		Help: "Total number of bullets before extraction-time deduplication",
	})

	BulletDedupAfterTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_bullet_dedup_after_total",
		Help: "Total number of bullets after extraction-time deduplication",
	})

	BulletSingleModeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_bullet_single_mode_total",
		Help: "Total number of messages forced into single-bullet extraction mode",
	})

	DiscoveryPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_discovery_pending",
		Help: "Current number of pending channel discoveries",
	})

	DiscoveryActionable = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_discovery_actionable",
		Help: "Current number of actionable channel discoveries",
	})

	DiscoveryApprovalRate = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "digest_discovery_approval_rate",
		Help: "Approval rate for discoveries (added / (added + rejected))",
	})

	DiscoveryApprovedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_discovery_approved_total",
		Help: "Total number of approved discoveries",
	})

	DiscoveryRejectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_discovery_rejected_total",
		Help: "Total number of rejected discoveries",
	})

	FactCheckRequestDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_factcheck_request_duration_seconds",
		Help:    "Duration of fact check API requests",
		Buckets: prometheus.DefBuckets,
	})

	FactCheckRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_factcheck_requests_total",
		Help: "Total number of fact check requests",
	}, []string{"result"})

	FactCheckMatches = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_factcheck_matches_total",
		Help: "Total number of items with fact check matches",
	})

	FactCheckCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_factcheck_cache_hits_total",
		Help: "Total number of fact check cache hits",
	})

	FactCheckCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_factcheck_cache_misses_total",
		Help: "Total number of fact check cache misses",
	})

	// Enrichment metrics (Phase 2)
	EnrichmentRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_enrichment_request_duration_seconds",
		Help:    "Duration of enrichment provider requests",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider"})

	EnrichmentRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_enrichment_requests_total",
		Help: "Total number of enrichment requests",
	}, []string{"provider", "result", "language"})

	EnrichmentMatches = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_enrichment_matches_total",
		Help: "Total number of items with evidence matches",
	})

	EnrichmentCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_enrichment_cache_hits_total",
		Help: "Total number of enrichment cache hits",
	})

	EnrichmentCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "digest_enrichment_cache_misses_total",
		Help: "Total number of enrichment cache misses",
	})

	EnrichmentCircuitBreakerOpens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_enrichment_cb_opens_total",
		Help: "Total number of times circuit breaker opened",
	}, []string{"provider"})

	EnrichmentCorroborationScore = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "digest_enrichment_corroboration_score",
		Help:    "Distribution of corroboration scores for enriched items",
		Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	})

	CorroborationCoverage = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_corroboration_coverage_total",
		Help: "Total number of items with and without channel corroboration",
	}, []string{"has_corroboration"})

	// Search result metrics - track how many results each provider returns
	EnrichmentSearchResults = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_enrichment_search_results",
		Help:    "Distribution of search result counts per query by provider",
		Buckets: []float64{0, 1, 2, 5, 10, 20, 50, 100},
	}, []string{"provider"})

	EnrichmentSearchResultsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_enrichment_search_results_total",
		Help: "Total number of search results returned by provider",
	}, []string{"provider"})

	EnrichmentSearchZeroResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_enrichment_search_zero_results_total",
		Help: "Total number of searches that returned zero results",
	}, []string{"provider"})

	// LLM token usage metrics (Phase 3)
	LLMTokensPrompt = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_llm_tokens_prompt_total",
		Help: "Total number of prompt tokens used",
	}, []string{"provider", "model", "task"})

	LLMTokensCompletion = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_llm_tokens_completion_total",
		Help: "Total number of completion tokens used",
	}, []string{"provider", "model", "task"})

	LLMRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_llm_requests_total",
		Help: "Total number of LLM requests",
	}, []string{"provider", "model", "task", "status"})

	// LLM fallback and circuit breaker metrics
	LLMFallbacks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_llm_fallbacks_total",
		Help: "Total number of LLM fallback events",
	}, []string{"from_provider", "to_provider", "task"})

	LLMCircuitBreakerOpens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_llm_circuit_breaker_opens_total",
		Help: "Total number of times LLM circuit breaker opened",
	}, []string{"provider"})

	LLMCircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_llm_circuit_breaker_state",
		Help: "Current state of LLM circuit breaker (0=closed, 1=open)",
	}, []string{"provider"})

	// LLM latency by provider and task
	LLMRequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_llm_request_latency_seconds",
		Help:    "Latency of LLM requests by provider and task",
		Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
	}, []string{"provider", "model", "task"})

	// LLM estimated costs (in millicents to avoid floating point issues)
	LLMEstimatedCost = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_llm_estimated_cost_millicents_total",
		Help: "Estimated LLM cost in millicents (0.001 cents)",
	}, []string{"provider", "model", "task"})

	// LLM provider availability
	LLMProviderAvailable = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_llm_provider_available",
		Help: "Whether LLM provider is currently available (0=no, 1=yes)",
	}, []string{"provider"})

	// Embedding metrics
	EmbeddingRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_embedding_requests_total",
		Help: "Total number of embedding requests",
	}, []string{"provider", "model", "status"})

	EmbeddingTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_embedding_tokens_total",
		Help: "Total number of tokens processed for embeddings",
	}, []string{"provider", "model"})

	EmbeddingLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_embedding_latency_seconds",
		Help:    "Latency of embedding requests by provider",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10},
	}, []string{"provider", "model"})

	EmbeddingEstimatedCost = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_embedding_estimated_cost_millicents_total",
		Help: "Estimated embedding cost in millicents (0.001 cents)",
	}, []string{"provider", "model"})

	EmbeddingProviderAvailable = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_embedding_provider_available",
		Help: "Whether embedding provider is currently available (0=no, 1=yes)",
	}, []string{"provider"})

	EmbeddingFallbacks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_embedding_fallbacks_total",
		Help: "Total number of embedding fallback events",
	}, []string{"from_provider", "to_provider"})

	// Link seeding metrics (Telegram → crawler queue)
	LinkSeedExtracted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "link_seed_extracted_total",
		Help: "Total number of links extracted from Telegram messages",
	})

	LinkSeedEnqueued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "link_seed_enqueued_total",
		Help: "Total number of links successfully enqueued for crawling",
	})

	LinkSeedSkipped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "link_seed_skipped_total",
		Help: "Total number of links skipped during seeding",
	}, []string{"reason"})

	LinkSeedErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "link_seed_errors_total",
		Help: "Total number of errors during link seeding",
	})

	CrawlerQueuePending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crawler_queue_pending",
		Help: "Current number of pending URLs in the crawler queue",
	})

	// Telegram Reader metrics
	ReaderFloodWaitSecondsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_flood_wait_seconds_total",
		Help: "Total time in seconds spent waiting for Telegram flood control",
	}, []string{"channel"})

	ReaderFloodWaitCountTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_flood_wait_total",
		Help: "Total number of Telegram flood wait events",
	}, []string{"channel"})

	ReaderFetchRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_fetch_requests_total",
		Help: "Total number of history fetch requests to Telegram",
	}, []string{"channel", "status"})

	ReaderHistoryBatchSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_history_batch_size",
		Help: "Number of messages returned in a history fetch",
	}, []string{"channel"})

	ReaderHistoryNewMessages = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_history_new_messages",
		Help: "Number of messages in a history fetch newer than the last seen ID",
	}, []string{"channel"})

	ReaderHistoryMaxID = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_history_max_id",
		Help: "Max message ID seen in a history fetch",
	}, []string{"channel"})

	ReaderHistoryMinID = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_history_min_id",
		Help: "Min message ID seen in a history fetch",
	}, []string{"channel"})

	ReaderReplayOnlyFetchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_replay_only_fetch_total",
		Help: "Total number of history fetches that contained no new messages",
	}, []string{"channel"})

	ReaderMessageAgeSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_reader_message_age_seconds",
		Help:    "Age of ingested messages at time of collection",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400, 172800, 604800},
	}, []string{"channel"})

	ReaderIngestLagSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "digest_reader_ingest_lag_seconds",
		Help:    "Lag between message timestamp and ingestion time",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400, 172800, 604800},
	}, []string{"channel"})

	ReaderBackfillTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_backfill_total",
		Help: "Total number of ingested messages older than the backfill threshold",
	}, []string{"channel"})

	ReaderReplayTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_replay_total",
		Help: "Total number of ingested messages with IDs at or below last seen",
	}, []string{"channel"})

	ReaderForwardedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_reader_forwarded_total",
		Help: "Total number of forwarded messages ingested",
	}, []string{"channel"})

	ReaderBackfillRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_backfill_ratio",
		Help: "Share of ingested messages considered backfill within the last batch",
	}, []string{"channel"})

	ReaderReplayRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_replay_ratio",
		Help: "Share of ingested messages with IDs at or below last seen within the last batch",
	}, []string{"channel"})

	ReaderForwardedRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "digest_reader_forwarded_ratio",
		Help: "Share of ingested messages that are forwarded within the last batch",
	}, []string{"channel"})
)
