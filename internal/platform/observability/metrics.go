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

	DigestsPosted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "digest_posts_total",
		Help: "The total number of digests posted",
	}, []string{"status"})

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
)
