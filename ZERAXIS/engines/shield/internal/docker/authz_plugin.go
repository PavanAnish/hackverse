package docker

import (
	"encoding/json"
	"net/http"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
)

type AuthZReq struct {
	User           string `json:"User,omitempty"`
	UserAuthNMethod string `json:"UserAuthNMethod,omitempty"`
	RequestMethod  string `json:"RequestMethod,omitempty"`
	RequestURI     string `json:"RequestURI,omitempty"`
	RequestBody    []byte `json:"RequestBody,omitempty"`
}

type AuthZResponse struct {
	Allow bool   `json:"Allow"`
	Msg   string `json:"Msg,omitempty"`
	Err   string `json:"Err,omitempty"`
}

type AuthZPlugin struct {
	evaluator *evaluator.OPAEvaluator
	server    *http.Server
}

func NewAuthZPlugin(addr string, eval *evaluator.OPAEvaluator) *AuthZPlugin {
	p := &AuthZPlugin{
		evaluator: eval,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/Plugin.Activate", p.handleActivate)
	mux.HandleFunc("/AuthZPlugin.AuthZReq", p.handleAuthZReq)
	mux.HandleFunc("/AuthZPlugin.AuthZRes", p.handleAuthZRes)
	
	p.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return p
}

func (p *AuthZPlugin) Start() error {
	return p.server.ListenAndServe()
}

func (p *AuthZPlugin) handleActivate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1.1+json")
	w.Write([]byte(`{"Implements": ["authz"]}`))
}

func (p *AuthZPlugin) handleAuthZReq(w http.ResponseWriter, r *http.Request) {
	var req AuthZReq
	json.NewDecoder(r.Body).Decode(&req)
	
	// Phase 8 minimal stub
	resp := AuthZResponse{
		Allow: true,
	}
	
	w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1.1+json")
	json.NewEncoder(w).Encode(resp)
}

func (p *AuthZPlugin) handleAuthZRes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1.1+json")
	json.NewEncoder(w).Encode(AuthZResponse{Allow: true})
}
