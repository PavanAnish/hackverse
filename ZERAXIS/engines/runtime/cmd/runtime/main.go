package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pavanvanish/zeraxis/engines/runtime/internal/api"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/collector"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/engine"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/normalizer"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/repository"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/resolver"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting ZERAXIS Runtime Engine", "version", "0.1.0")

	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable")
	natsURL := getEnv("NATS_URL", "nats://nats:4222")
	discURL := getEnv("DISCOVERY_API_URL", "http://discovery-engine:8090")
	scURL := getEnv("SUPPLY_CHAIN_API_URL", "http://supply-chain-engine:8091")
	falcoPath := getEnv("FALCO_EVENTS_PATH", "/var/run/falco/events.jsonl")
	apiPort := getEnv("API_PORT", "8092")

	// Initialize PostgreSQL
	repo, err := repository.NewPostgres(dbURL, "/migrations")
	if err != nil {
		slog.Error("Database init failed", "error", err)
		os.Exit(1)
	}
	defer repo.Close()
	slog.Info("PostgreSQL connected")

	// Initialize NATS Publisher
	pub, err := events.NewNATSPublisher(natsURL)
	if err != nil {
		slog.Error("Failed to initialize NATS publisher", "error", err)
		os.Exit(1)
	}
	defer pub.Close()
	slog.Info("NATS Publisher connected")

	// Initialize Collector (file tailer)
	tailer := collector.NewTailer(falcoPath, 10000)

	// Initialize Resolver
	client := resolver.NewDiscoveryClient(discURL, scURL)
	res := resolver.NewResolver(client, 5*time.Minute)

	// Initialize Normalizer
	norm := normalizer.NewNormalizer(res)

	// Initialize Engine
	eng := engine.NewEngine(tailer, norm, repo, pub, res)

	// Initialize API Server
	srv := api.NewServer(repo, eng, falcoPath, apiPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start API server
	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			slog.Error("API error", "error", err)
		}
	}()

	// Start collector
	go tailer.Run(ctx)

	// Start engine
	go eng.Run(ctx)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	slog.Info("Runtime engine stopped")
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
