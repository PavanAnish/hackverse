package main
import ("database/sql"; "encoding/json"; "log/slog"; "net/http"; "os"; "time"; _ "github.com/lib/pq")
const version = "0.1.0"
var db *sql.DB
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" { dbURL = "postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable" }
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil { slog.Error("Failed to open database", "error", err) }

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := "healthy"
		if err := db.Ping(); err != nil { status = "degraded"; slog.Error("DB ping failed", "err", err) }
		w.Header().Set("Content-Type", "application/json")
		if status != "healthy" { w.WriteHeader(http.StatusServiceUnavailable) }
		json.NewEncoder(w).Encode(map[string]string{"status": status, "service": "product-service"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" { http.NotFound(w, r); return }
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 1, "name": "Secure Gateway", "price": 99.99}})
	})
	handler := logMiddleware(mux)
	port := os.Getenv("PORT"); if port == "" { port = "8082" }
	slog.Info("Starting product service", "port", port)
	http.ListenAndServe(":"+port, handler)
}
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now(); rw := &responseWriter{w, http.StatusOK}; next.ServeHTTP(rw, r)
		slog.Info("Request handled", "service", "product-service", "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}
type responseWriter struct { http.ResponseWriter; status int }
func (rw *responseWriter) WriteHeader(code int) { rw.status = code; rw.ResponseWriter.WriteHeader(code) }
