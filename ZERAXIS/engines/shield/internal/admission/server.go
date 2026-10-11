package admission

import (
	"context"
	"net/http"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

type Server struct {
	addr      string
	evaluator *evaluator.OPAEvaluator
	server    *http.Server
	mux       *http.ServeMux
}

func NewServer(addr string, evaluator *evaluator.OPAEvaluator) *Server {
	s := &Server{
		addr:      addr,
		evaluator: evaluator,
		mux:       http.NewServeMux(),
	}

	s.mux.HandleFunc("/v1/admission/evaluate", s.evaluateHandler)
	s.mux.HandleFunc("/health", s.healthHandler)

	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
