package admission

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

type EvaluateResponse struct {
	Allowed    bool   `json:"allowed"`
	Reason     string `json:"reason"`
	DecisionID string `json:"decision_id"`
}

func (s *Server) evaluateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Millisecond)
	defer cancel()

	var req evaluator.AdmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(EvaluateResponse{
			Allowed:    false,
			Reason:     "INVALID_REQUEST",
			DecisionID: uuid.New().String(),
		})
		return
	}

	decisionID := uuid.New().String()

	start := time.Now()
	dec, err := s.evaluator.Evaluate(ctx, req)
	
	if err != nil {
		log.Printf("audit: evaluate failed req_id=%s err=%v", decisionID, err)
		dec.Allowed = false
		dec.Reason = "EVAL_ERROR"
	} else if ctx.Err() == context.DeadlineExceeded {
		dec.Allowed = false
		dec.Reason = "TIMEOUT_FAIL_CLOSED"
	}

	log.Printf("audit: admission decision req_id=%s fingerprint=%s allowed=%v reason=%s latency=%v", decisionID, req.Fingerprint, dec.Allowed, dec.Reason, time.Since(start))

	resp := EvaluateResponse{
		Allowed:    dec.Allowed,
		Reason:     dec.Reason,
		DecisionID: decisionID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"OK"}`))
}
