package research

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "research_api_requests_total",
		Help: "Total number of research API requests",
	}, []string{"route", "status"})

	latencyHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "research_api_latency_seconds",
		Help:    "Latency of research API requests",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})

	resultSizeGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "research_api_result_size",
		Help: "Result size for research API responses",
	}, []string{"route"})
)
