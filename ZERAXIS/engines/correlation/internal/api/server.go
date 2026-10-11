// Package api provides the Phase 6.2 read-only HTTP API for the Correlation Engine.
// All endpoints are GET-only. No mutation endpoints exist.
// No risk, attack-chain, or recommendation endpoints exist in Phase 6.2.
package api

import (
	"os"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/approvals"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/correlator"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"
)

// Server is the HTTP API server for Phase 6.2.
type Server struct {
	repo       *repository.Repository
	correlator *correlator.Correlator
	approvalSvc *approvals.Service
	mux        *http.ServeMux
	port       string
}

// New constructs the API server.
func New(repo *repository.Repository, c *correlator.Correlator, approvalSvc *approvals.Service, port string) *Server {
	s := &Server{
		repo:       repo,
		correlator: c,
		approvalSvc: approvalSvc,
		mux:        http.NewServeMux(),
		port:       port,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Health
	s.mux.HandleFunc("/health", s.handleHealth)

	// Incident listing
	s.mux.HandleFunc("/api/v1/incidents", s.handleListIncidents)

	// Incident detail routing — dispatches to sub-handlers based on trailing path
	s.mux.HandleFunc("/api/v1/incidents/", s.handleIncidentRouter)

	s.mux.HandleFunc("/api/v1/recommendations/", s.handleRecommendationRouter)
}

// Start starts the HTTP server in a goroutine.
func (s *Server) Start() {
	go func() {
		slog.Info("Correlation API listening", "port", s.port)
		if err := http.ListenAndServe(":"+s.port, s.mux); err != nil && err != http.ErrServerClosed {
			slog.Error("API server error", "error", err)
		}
	}()
}

// ====== Health ======

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dbStatus := "connected"
	if err := s.repo.Ping(); err != nil {
		dbStatus = "error: " + err.Error()
	}
	m := s.correlator.Metrics()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"service":           "correlation-engine",
		"status":            "healthy",
		"version":           "0.1.0",
		"database":          dbStatus,
		"incidents_created": m.IncidentsCreated,
		"incidents_updated": m.IncidentsUpdated,
	})
}

// ====== List Incidents ======

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	incidents, err := s.repo.ListIncidents(r.Context(), status, limit, offset)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("List incidents failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incidents": incidents,
		"count":     len(incidents),
	})
}

// ====== Incident Router ======

// handleIncidentRouter dispatches /api/v1/incidents/{id}[/sub-path]
func (s *Server) handleIncidentRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /api/v1/incidents/{id}[/timeline]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/incidents/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing incident id", http.StatusBadRequest)
		return
	}
	incidentID := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}

	switch subPath {
	case "":
		s.handleGetIncident(w, r, incidentID)
	case "timeline":
		s.handleGetTimeline(w, r, incidentID)
	case "risk":
		s.handleGetRisk(w, r, incidentID)
	case "attack-chain":
		s.handleGetAttackChain(w, r, incidentID)
	case "recommendations":
		s.handleGetRecommendations(w, r, incidentID)
	default:
		// Phase 6.4 endpoints (recommendations) are not implemented here.
		http.Error(w, "endpoint not implemented in Phase 6.3", http.StatusNotFound)
	}
}

// ====== GET /api/v1/incidents/{id}/risk ======

func (s *Server) handleGetRisk(w http.ResponseWriter, r *http.Request, id string) {
	inc, err := s.repo.GetIncidentByID(r.Context(), id)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("GetIncident DB error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inc == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Phase 6.3: Recalculate deterministic risk on demand to apply time-based decay
	if err := s.correlator.RecalculateIncidentRisk(r.Context(), inc, time.Now().UTC()); err != nil {
		slog.Error("Recalculate risk error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incident_id":        inc.IncidentID,
		"risk_model_version": inc.RiskModelVersion,
		"static_risk":        inc.StaticRisk,
		"dynamic_risk":       inc.DynamicRisk,
		"combined_risk":      inc.CombinedRisk,
		"risk_level":         inc.RiskLevel,
		"confidence":         inc.Confidence,
		"calculated_at":      inc.CalculatedAt,
	})
}

// ====== GET /api/v1/incidents/{id}/attack-chain ======

func (s *Server) handleGetAttackChain(w http.ResponseWriter, r *http.Request, id string) {
	inc, err := s.repo.GetIncidentByID(r.Context(), id)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("GetIncident DB error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inc == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Phase 6.3: Recalculate deterministic attack chain state on demand
	if err := s.correlator.RecalculateIncidentRisk(r.Context(), inc, time.Now().UTC()); err != nil {
		slog.Error("Recalculate attack chain error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var evidence map[string]interface{}
	if len(inc.AttackChainEvidence) > 0 {
		_ = json.Unmarshal(inc.AttackChainEvidence, &evidence)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incident_id":        inc.IncidentID,
		"current_state":      inc.AttackChainState,
		"evidence":           evidence,
		"calculated_at":      inc.CalculatedAt,
		"confidence":         inc.Confidence,
	})
}

// ====== GET /api/v1/incidents/{id} ======

func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request, id string) {
	inc, err := s.repo.GetIncidentByID(r.Context(), id)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("GetIncident DB error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inc == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

// ====== GET /api/v1/incidents/{id}/timeline ======

type TimelineEvent struct {
	Timestamp   time.Time   `json:"timestamp"`
	EventType   string      `json:"event_type"`
	EventID     string      `json:"event_id,omitempty"`
	FindingID   string      `json:"finding_id,omitempty"`
	IncidentID  string      `json:"incident_id,omitempty"`
	Severity    string      `json:"severity,omitempty"`
	Details     interface{} `json:"details,omitempty"`
}

func (s *Server) handleGetTimeline(w http.ResponseWriter, r *http.Request, id string) {
	timeline, err := s.repo.GetIncidentTimeline(r.Context(), id)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("GetIncidentTimeline DB error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if timeline == nil {
		// Verify if incident exists
		inc, err2 := s.repo.GetIncidentByID(r.Context(), id)
		if err2 != nil || inc == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incident_id": id,
		"timeline": timeline,
	})
}


// ====== GET /api/v1/incidents/{id}/recommendations ======

func (s *Server) handleGetRecommendations(w http.ResponseWriter, r *http.Request, id string) {
	inc, err := s.repo.GetIncidentByID(r.Context(), id)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("GetIncident DB error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if inc == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	recs, err := s.repo.GetIncidentRecommendations(r.Context(), id)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		slog.Error("GetRecommendations DB error", "incident_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incident_id":     id,
		"recommendations": recs,
	})
}

// ====== Helpers ======

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("JSON encode error", "error", err)
	}
}

// ErrNotFound sentinel for repository
var _ = sql.ErrNoRows


// ====== Recommendation Router ======

func (s *Server) handleRecommendationRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/recommendations/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	recID := parts[0]
	action := parts[1]

	
	// Phase 7.7: Identity Trust Boundary Enforcement
	// Prevent spoofing by verifying the request originated from the trusted gateway.
	expectedSecret := os.Getenv("API_INTERNAL_SECRET")
	if expectedSecret == "" {
		slog.Error("FATAL: API_INTERNAL_SECRET is required")
		os.Exit(1)
	}
	if r.Header.Get("X-Internal-Secret") != expectedSecret {
		http.Error(w, "Forbidden: Untrusted caller", http.StatusForbidden)
		return
	}

	approverID := r.Header.Get("X-Forwarded-User")
	if strings.TrimSpace(approverID) == "" {
		http.Error(w, "Unauthorized: missing identity", http.StatusUnauthorized)
		return
	}

	switch action {
	case "approve":
		execID, err := s.approvalSvc.ApproveAndPublish(r.Context(), recID, approverID)
		if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
			if strings.Contains(err.Error(), "only PROPOSED") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"execution_id": execID, "status": "APPROVED"})
	case "revoke":
		err := s.approvalSvc.Revoke(r.Context(), recID, approverID)
		if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
			if strings.Contains(err.Error(), "only APPROVED recommendations can be revoked") || strings.Contains(err.Error(), "authorization cannot be revoked") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "REVOKED"})
	case "abandon":
		err := s.approvalSvc.Abandon(r.Context(), recID, approverID)
		if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
			if strings.Contains(err.Error(), "only APPROVED recommendations can be abandoned") || strings.Contains(err.Error(), "only EXECUTING authorizations can be abandoned") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ABANDONED"})
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}


// ServeHTTP makes the Server implement http.Handler, useful for testing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleResolveInvestigation(w http.ResponseWriter, r *http.Request) {
	expectedSecret := os.Getenv("API_INTERNAL_SECRET")
	if expectedSecret == "" {
		slog.Error("FATAL: API_INTERNAL_SECRET is required")
		os.Exit(1)
	}
	if r.Header.Get("X-Internal-Secret") != expectedSecret {
		http.Error(w, "Forbidden: Untrusted caller", http.StatusForbidden)
		return
	}

	operatorID := r.Header.Get("X-Forwarded-User")
	if operatorID == "" {
		http.Error(w, "Missing X-Forwarded-User header", http.StatusUnauthorized)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	recID := parts[len(parts)-2]

	var req struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Outcome != "OUTCOME_MANUALLY_REMEDIATED" && req.Outcome != "OUTCOME_SAFE_TO_RETRY" {
		http.Error(w, "outcome must be OUTCOME_MANUALLY_REMEDIATED or OUTCOME_SAFE_TO_RETRY", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		http.Error(w, "reason is required", http.StatusBadRequest)
		return
	}

	err := s.approvalSvc.ResolveInvestigation(r.Context(), recID, operatorID, req.Outcome, req.Reason)
	if err != nil && err.Error() != "authorization committed but NATS transport unavailable" && err.Error() != "authorization committed but NATS dispatch failed" {
		if strings.Contains(err.Error(), "only MANUAL_INVESTIGATION_REQUIRED") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
