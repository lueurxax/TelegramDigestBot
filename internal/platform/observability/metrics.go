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
)
