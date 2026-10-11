package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pavanvanish/zeraxis/engines/discovery/internal/models"
	"github.com/pavanvanish/zeraxis/engines/discovery/internal/repository"
)

const version = "0.1.0"

// Server provides the read-only discovery HTTP API.
// This API is intended for development/internal access only.
// It must not be exposed to untrusted networks.
type Server struct {
	repo   *repository.Postgres
	server *http.Server
}

// NewServer creates a new API server.
func NewServer(repo *repository.Postgres, port string) *Server {
	s := &Server{repo: repo}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/assets/", s.handleAssetByID)
	mux.HandleFunc("/api/v1/assets", s.handleAssets)
	mux.HandleFunc("/api/v1/relationships", s.handleRelationships)
	mux.HandleFunc("/api/v1/events", s.handleEvents)

	s.server = &http.Server{
		Addr:         ":" + port,
		Handler:      logMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

// Start starts the HTTP server (blocking).
func (s *Server) Start() error {
	slog.Info("Starting discovery API", "port", s.server.Addr, "version", version)
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
		"status":    "healthy",
		"service":   "discovery-engine",
		"version":   version,
		"database":  dbStatus,
	})
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.repo.GetAllAssets(r.Context())
	if err != nil {
		slog.Error("Failed to get assets", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve assets"})
		return
	}
	if assets == nil {
		assets = []models.Asset{}
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) handleAssetByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/v1/assets/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/assets/")
	if id == "" {
		s.handleAssets(w, r)
		return
	}

	asset, err := s.repo.GetAssetByID(r.Context(), id)
	if err != nil {
		slog.Error("Failed to get asset", "asset_id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve asset"})
		return
	}
	if asset == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "asset not found"})
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	rels, err := s.repo.GetRelationships(r.Context())
	if err != nil {
		slog.Error("Failed to get relationships", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve relationships"})
		return
	}
	if rels == nil {
		rels = []models.Relationship{}
	}
	writeJSON(w, http.StatusOK, rels)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := s.repo.GetEvents(r.Context(), limit)
	if err != nil {
		slog.Error("Failed to get events", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve events"})
		return
	}
	if events == nil {
		events = []models.DiscoveryEvent{}
	}
	writeJSON(w, http.StatusOK, events)
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
			"service", "discovery-engine",
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
