package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
			"service": "auth-service",
			"version": version,
		})
	})

	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"service": "auth-service",
			"version": version,
		})
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// NOTE: This is intentional demo scaffolding. This service is a target monitored
		// by ZERAXIS — it is NOT a security component. Do not use in any real deployment.
		json.NewEncoder(w).Encode(map[string]string{
			"token": "mock-jwt-token",
		})
	})

	handler := logMiddleware(mux)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	slog.Info("Starting auth service", "port", port, "version", version)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		os.Exit(1)
	}
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("Request handled", "service", "auth-service", "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
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
