package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

const (
	shutdownTimeout = 5 * time.Second
)

type Server struct {
	db     *db.DB
	port   int
	logger *zerolog.Logger
}

func NewServer(db *db.DB, port int, logger *zerolog.Logger) *Server {
	return &Server{
		db:     db,
		port:   port,
		logger: logger,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
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

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)

		defer cancel()

		_ = srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info().Int("port", s.port).Msg("Health check server starting")

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server error: %w", err)
	}

	return nil
}
