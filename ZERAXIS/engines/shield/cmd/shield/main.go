package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"

	"github.com/pavanvanish/zeraxis/engines/shield/internal/admission"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/cache"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/evaluator"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/k8s"
	"github.com/pavanvanish/zeraxis/engines/shield/internal/multicluster"
)

type evaluatorWrapper struct {
	eval *evaluator.OPAEvaluator
}

func (w *evaluatorWrapper) Evaluate(ctx context.Context, fingerprint string) (bool, string, error) {
	req := evaluator.AdmissionRequest{
		Fingerprint: fingerprint,
		ImageDigest: fingerprint, // for testing purposes
	}
	decision, err := w.eval.Evaluate(ctx, req)
	if err != nil {
		return false, "", err
	}
	return decision.Allowed, decision.Reason, nil
}

type cacheWrapper struct {
	matView *cache.MaterializedView
}

func (c *cacheWrapper) Invalidate(fingerprint string) error {
	return c.matView.Invalidate(context.Background(), fingerprint)
}

func main() {
	var help bool
	flag.BoolVar(&help, "help", false, "Show help message")
	flag.Parse()

	if help {
		fmt.Println("Shield Engine - ZERAXIS Security Policy and Break-Glass Server")
		fmt.Println("Usage: shield [options]")
		fmt.Println("Environment Variables:")
		fmt.Println("  SHIELD_PORT       HTTP port (default 8080)")
		fmt.Println("  SHIELD_GRPC_PORT  gRPC port (default 9090)")
		fmt.Println("  DATABASE_URL      PostgreSQL connection string")
		fmt.Println("  NATS_URL          NATS connection string")
		os.Exit(0)
	}

	port := os.Getenv("SHIELD_PORT")
	if port == "" {
		port = "8080"
	}

	grpcPort := os.Getenv("SHIELD_GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "9090"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/zeraxis?sslmode=disable"
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}

	// 1. Database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Printf("Warning: database not reachable: %v", err)
	} else {
		log.Println("PostgreSQL connection pool initialized")
	}

	// 2. NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Printf("Warning: failed to connect to NATS: %v", err)
	} else {
		log.Println("NATS connection initialized")
		defer nc.Close()
	}

	// 3. Multicluster coherency manager
	matView := cache.NewMaterializedView(db)
	if nc != nil {
		manager := multicluster.NewNATSCoherencyManager()
		if err := manager.Start(nc, &cacheWrapper{matView: matView}, "local-cluster"); err != nil {
			log.Printf("Failed to start NATS coherency manager: %v", err)
		}
	}

	// 4. HTTP Server
	eval := evaluator.NewOPAEvaluator(db, matView)
	admServer := admission.NewServer(":"+port, eval)

	// Add Webhook Handler
	webhookHandler := k8s.NewWebhookHandler(&evaluatorWrapper{eval: eval})
	admServer.Handler().(*http.ServeMux).Handle("/v1/k8s/validate", webhookHandler)

	log.Printf("Starting Shield HTTP server on port %s", port)
	log.Printf("Starting Shield gRPC server on port %s (placeholder)", grpcPort)

	errChan := make(chan error, 1)
	go func() {
		errChan <- admServer.Start()
	}()

	// Signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		log.Fatalf("Server error: %v", err)
	case sig := <-sigChan:
		log.Printf("Received signal %v, initiating graceful shutdown", sig)
		admServer.Stop(context.Background())
		log.Println("Shutdown complete")
	}
}
