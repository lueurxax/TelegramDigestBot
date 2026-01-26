package expandedview

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metric label values.
const (
	StatusOK       = "200"
	StatusDenied   = "401"
	StatusNotFound = "404"
	StatusExpired  = "410"
	StatusLimited  = "429"
	StatusError    = "500"

	ReasonInvalidToken = "invalid_token"
	ReasonExpired      = "expired"
	ReasonNotAdmin     = "not_admin"
	ReasonRateLimited  = "rate_limited"

	ErrorTypeDB     = "db_error"
	ErrorTypeRender = "render_error"
)

var (
	// HitsTotal counts total requests by HTTP status code.
	HitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "expanded_view_hits_total",
		Help: "Total number of expanded view page hits",
	}, []string{"status"})

	// DeniedTotal counts denied requests by reason.
	DeniedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "expanded_view_denied_total",
		Help: "Total number of denied expanded view requests",
	}, []string{"reason"})

	// ErrorsTotal counts errors by type.
	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "expanded_view_errors_total",
		Help: "Total number of expanded view errors",
	}, []string{"type"})

	// LatencyHistogram measures request latency.
	LatencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "expanded_view_latency_seconds",
		Help:    "Latency of expanded view page rendering",
		Buckets: prometheus.DefBuckets,
	})
)
