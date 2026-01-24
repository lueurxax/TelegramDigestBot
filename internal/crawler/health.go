package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	healthCheckTimeoutShort = 5 * time.Second
	healthCheckTimeoutLong  = 10 * time.Second
)

// Prometheus metrics for the crawler.
var (
	crawlerQueuePending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "crawler_queue_pending",
		Help: "Number of pending URLs in the crawl queue",
	})
	crawlerQueueProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "crawler_queue_processing",
		Help: "Number of URLs currently being processed",
	})
	crawlerQueueDone = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "crawler_queue_done",
		Help: "Number of successfully crawled URLs",
	})
	crawlerQueueError = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "crawler_queue_error",
		Help: "Number of URLs that failed to crawl",
	})
	crawlerURLsProcessedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawler_urls_processed_total",
		Help: "Total number of URLs processed by this crawler instance",
	})
	crawlerExtractionErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawler_extraction_errors_total",
		Help: "Total number of extraction errors",
	})
)

func init() {
	prometheus.MustRegister(
		crawlerQueuePending,
		crawlerQueueProcessing,
		crawlerQueueDone,
		crawlerQueueError,
		crawlerURLsProcessedTotal,
		crawlerExtractionErrorsTotal,
	)
}

// HealthServer provides health check endpoints for the crawler.
type HealthServer struct {
	crawler *Crawler
	port    int
	ready   atomic.Bool
	server  *http.Server
}

// NewHealthServer creates a new HealthServer.
func NewHealthServer(crawler *Crawler, port int) *HealthServer {
	hs := &HealthServer{
		crawler: crawler,
		port:    port,
	}
	hs.ready.Store(false)

	return hs
}

// SetReady marks the server as ready.
func (hs *HealthServer) SetReady(ready bool) {
	hs.ready.Store(ready)
}

// Start starts the health server.
func (hs *HealthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", hs.handleHealthz)
	mux.HandleFunc("/readyz", hs.handleReadyz)
	mux.HandleFunc("/stats", hs.handleStats)
	mux.Handle("/metrics", promhttp.Handler())

	hs.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", hs.port),
		Handler:           mux,
		ReadHeaderTimeout: healthCheckTimeoutShort,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), healthCheckTimeoutShort)
		defer cancel()

		_ = hs.server.Shutdown(shutdownCtx) //nolint:errcheck,contextcheck // Best-effort shutdown, must use new context
	}()

	if err := hs.server.ListenAndServe(); err != nil {
		return fmt.Errorf("start health server: %w", err)
	}

	return nil
}

// handleHealthz handles liveness probes.
func (hs *HealthServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte("ok")) //nolint:errcheck // Best-effort write
}

// handleReadyz handles readiness probes.
func (hs *HealthServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !hs.ready.Load() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}

	// Check Solr connectivity
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeoutShort)
	defer cancel()

	if err := hs.crawler.client.Ping(ctx); err != nil {
		http.Error(w, "solr unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte("ok")) //nolint:errcheck // Best-effort write
}

// handleStats returns queue statistics.
func (hs *HealthServer) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeoutLong)
	defer cancel()

	stats, err := hs.crawler.GetQueueStats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(stats) //nolint:errcheck,errchkjson // Best-effort encode
}

// UpdateQueueMetrics updates Prometheus metrics from queue stats.
func UpdateQueueMetrics(stats map[string]int) {
	if v, ok := stats["pending"]; ok {
		crawlerQueuePending.Set(float64(v))
	}

	if v, ok := stats["processing"]; ok {
		crawlerQueueProcessing.Set(float64(v))
	}

	if v, ok := stats["done"]; ok {
		crawlerQueueDone.Set(float64(v))
	}

	if v, ok := stats["error"]; ok {
		crawlerQueueError.Set(float64(v))
	}
}

// IncrementProcessed increments the processed URLs counter.
func IncrementProcessed() {
	crawlerURLsProcessedTotal.Inc()
}

// IncrementExtractionErrors increments the extraction errors counter.
func IncrementExtractionErrors() {
	crawlerExtractionErrorsTotal.Inc()
}
