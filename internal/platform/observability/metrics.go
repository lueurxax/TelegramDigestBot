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
	}, []string{"provider", "result"})

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
)
