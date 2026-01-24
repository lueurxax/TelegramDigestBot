package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	healthCheckTimeoutShort = 5 * time.Second
	healthCheckTimeoutLong  = 10 * time.Second
)

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
