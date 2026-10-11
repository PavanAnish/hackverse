package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/repository"
)

type Server struct {
	repo   *repository.Postgres
	server *http.Server
}

func NewServer(repo *repository.Postgres, port string) *Server {
	s := &Server{repo: repo}
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/images/", s.handleImagesPrefix)
	mux.HandleFunc("/api/v1/images", s.handleImages)
	mux.HandleFunc("/api/v1/policy-decisions", s.handlePolicyDecisions)
	mux.HandleFunc("/api/v1/assets/", s.handleAssetsPrefix)
	mux.HandleFunc("/api/v1/scans", s.handleScans)

	s.server = &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) Start() error {
	slog.Info("Starting supply chain API", "port", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	if err := s.repo.Ping(); err != nil {
		status = "degraded"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": status, "database": status})
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.repo.GetImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(images)
}

func (s *Server) handleImagesPrefix(w http.ResponseWriter, r *http.Request) {
	// Simple router for /api/v1/images/{id}/*
	path := r.URL.Path[len("/api/v1/images/"):]
	parts := stringSplitNonEmpty(path, '/')
	if len(parts) == 0 {
		s.handleImages(w, r)
		return
	}

	imageID := parts[0]
	if len(parts) == 1 {
		// Just the image ID (not fully implemented in repo for brevity, falling back)
		http.Error(w, "Not implemented", http.StatusNotImplemented)
		return
	}

	switch parts[1] {
	case "sbom":
		s.handleSBOM(w, r, imageID)
	case "vulnerabilities":
		s.handleVulnerabilities(w, r, imageID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSBOM(w http.ResponseWriter, r *http.Request, imageID string) {
	sboms, err := s.repo.GetSBOMByImage(r.Context(), imageID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sboms)
}

func (s *Server) handleVulnerabilities(w http.ResponseWriter, r *http.Request, imageID string) {
	vulns, err := s.repo.GetVulnerabilitiesByImage(r.Context(), imageID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vulns)
}

func (s *Server) handlePolicyDecisions(w http.ResponseWriter, r *http.Request) {
	decisions, err := s.repo.GetPolicyDecisions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(decisions)
}

func (s *Server) handleScans(w http.ResponseWriter, r *http.Request) {
	scans, err := s.repo.GetScans(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scans)
}

func (s *Server) handleAssetsPrefix(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/assets/"):]
	parts := stringSplitNonEmpty(path, '/')
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	assetID := parts[0]
	if parts[1] == "supply-chain" {
		data, err := s.repo.GetAssetSupplyChain(r.Context(), assetID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	} else {
		http.NotFound(w, r)
	}
}

func stringSplitNonEmpty(s string, sep rune) []string {
	var parts []string
	var current string
	for _, c := range s {
		if c == sep {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
