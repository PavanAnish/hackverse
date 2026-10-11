package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"service": "gateway",
			"version": version,
		})
	})

	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"service": "gateway",
			"version": version,
		})
	})

	// Setup reverse proxies
	
	setupProxy(mux, "/api/auth/", "http://auth-service:8081")
	setupProxy(mux, "/api/products/", "http://product-service:8082")
	setupProxy(mux, "/api/payments/", "http://payment-service:8083")

	// Phase 7.7: Trusted Authorization Gateway for Correlation API
	setupSecureProxy(mux, "/api/v1/recommendations/", "http://correlation-engine:8094")


	// Middleware for logging
	handler := logMiddleware(mux)

	port := os.Getenv("PORT")
	if os.Getenv("API_INTERNAL_SECRET") == "" {
		slog.Error("API_INTERNAL_SECRET is required for gateway")
		os.Exit(1)
	}
	if port == "" {
		port = "8080"
	}

	slog.Info("Starting gateway service", "port", port, "version", version)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}

func setupProxy(mux *http.ServeMux, pathPrefix, targetURL string) {
	target, err := url.Parse(targetURL)
	if err != nil {
		slog.Error("Invalid target URL", "url", targetURL, "error", err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	
	// Strip prefix so /api/auth/health hits /health on the target
	mux.Handle(pathPrefix, http.StripPrefix(pathPrefix, proxy))
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		rw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(rw, r)
		
		slog.Info("Request handled",
			"service", "gateway",
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

func setupSecureProxy(mux *http.ServeMux, pathPrefix, targetURL string) {
	target, err := url.Parse(targetURL)
	if err != nil {
		slog.Error("Invalid target URL", "url", targetURL, "error", err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		
		// Authenticate and inject
		// In a real system, this would be IAP or OAuth. For the lab, we use Basic Auth.
		user, _, ok := req.BasicAuth()
		if ok {
			req.Header.Set("X-Forwarded-User", user)
		} else {
			req.Header.Set("X-Forwarded-User", "")
		}
		
		// Inject the internal secret
		secret := os.Getenv("API_INTERNAL_SECRET")
		// Always overwrite X-Internal-Secret to strip spoofed headers
		req.Header.Set("X-Internal-Secret", secret)
	}
	
	mux.Handle(pathPrefix, proxy)
}
