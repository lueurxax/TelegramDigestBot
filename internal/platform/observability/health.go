// Package observability provides health checks and metrics for the application.
//
// The Server exposes:
//   - /healthz: Liveness probe (always returns OK)
//   - /readyz: Readiness probe (checks database connectivity)
//   - /metrics: Prometheus metrics endpoint
//   - /i/*: Optional expanded view handler
//   - /research/*: Optional research dashboard handler
package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

const (
	shutdownTimeout      = 5 * time.Second
	readHeaderTimeout    = 10 * time.Second
	expandedViewPathBase = "/i/"
	researchPathBase     = "/research/"
)

type Server struct {
	db              *db.DB
	port            int
	logger          *zerolog.Logger
	expandedHandler http.Handler
	researchHandler http.Handler
}

func NewServer(db *db.DB, port int, logger *zerolog.Logger) *Server {
	return &Server{
		db:     db,
		port:   port,
		logger: logger,
	}
}

// NewServerWithExpanded creates a server with an optional expanded view handler.
func NewServerWithExpanded(db *db.DB, port int, expandedHandler http.Handler, logger *zerolog.Logger) *Server {
	return NewServerWithHandlers(db, port, expandedHandler, nil, logger)
}

// NewServerWithHandlers creates a server with optional expanded view and research handlers.
func NewServerWithHandlers(db *db.DB, port int, expandedHandler http.Handler, researchHandler http.Handler, logger *zerolog.Logger) *Server {
	return &Server{
		db:              db,
		port:            port,
		logger:          logger,
		expandedHandler: expandedHandler,
		researchHandler: researchHandler,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "OK")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.Pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, "DB error: %v", err)

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "OK")
	})

	mux.Handle("/metrics", promhttp.Handler())

	// Robots.txt to prevent indexing of expanded view pages
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /i/\nDisallow: /research/\n")
	})

	// Register expanded view handler if configured
	if s.expandedHandler != nil {
		mux.Handle(expandedViewPathBase, http.StripPrefix(expandedViewPathBase, s.expandedHandler))
	}

	// Register research handler if configured
	if s.researchHandler != nil {
		mux.HandleFunc("/research", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, researchPathBase, http.StatusMovedPermanently)
		})
		mux.Handle(researchPathBase, http.StripPrefix(researchPathBase, s.researchHandler))
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)

		defer cancel()

		//nolint:errcheck,contextcheck // shutdown in signal handler is best-effort, non-inherited context intentional
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info().Int("port", s.port).Msg("Health check server starting")

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server error: %w", err)
	}

	return nil
}
