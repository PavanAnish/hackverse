package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const version = "0.1.0"

// StatsProvider returns engine statistics.
type StatsProvider interface {
	Stats() map[string]int64
}

// Server provides the runtime engine HTTP API.
type Server struct {
	repo   interface{ Ping() error }
	stats  StatsProvider
	path   string
	server *http.Server
}

// NewServer creates a new API server.
func NewServer(repo interface{ Ping() error }, stats StatsProvider, path string, port string) *Server {
	s := &Server{repo: repo, stats: stats, path: path}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/info", s.handleInfo)

	s.server = &http.Server{
		Addr:         ":" + port,
		Handler:      logMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	slog.Info("Starting runtime API", "port", s.server.Addr, "version", version)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbStatus := "connected"
	if err := s.repo.Ping(); err != nil {
		dbStatus = "disconnected"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "healthy",
		"service":  "runtime-engine",
		"version":  version,
		"database": dbStatus,
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"service":           "runtime-engine",
		"version":           version,
		"falco_events_path": s.path,
	}
	if s.stats != nil {
		for k, v := range s.stats.Stats() {
			info[k] = v
		}
	}
	writeJSON(w, http.StatusOK, info)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("Request handled",
			"service", "runtime-engine",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
