package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/api"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/engine"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/policy"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/repository"
	"github.com/pavanvanish/zeraxis/engines/supply-chain/internal/scanner"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting ZERAXIS Supply Chain Engine", "version", "0.1.0")

	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable")
	apiPort := getEnv("API_PORT", "8091")
	discURL := getEnv("DISCOVERY_API_URL", "http://discovery-engine:8090")
	regURL := getEnv("REGISTRY_URL", "registry:5000")
	intervalStr := getEnv("SCAN_INTERVAL", "1h")
	interval, _ := time.ParseDuration(intervalStr)

	repo, err := repository.NewPostgres(dbURL, "/migrations")
	if err != nil {
		slog.Error("Database init failed", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	// Initialize NATS Publisher
	natsURL := getEnv("NATS_URL", "nats://nats:4222")
	pub, err := events.NewNATSPublisher(natsURL)
	if err != nil {
		slog.Error("Failed to initialize NATS publisher", "error", err)
		os.Exit(1)
	}
	defer pub.Close()
	slog.Info("NATS Publisher connected")

	localScanner := scanner.NewLocalScanner()
	polCfg := policy.NewConfig(
		getEnv("POLICY_CRITICAL", "BLOCK"),
		getEnv("POLICY_HIGH", "WARN"),
		getEnv("POLICY_MEDIUM", "RECORD"),
		getEnv("POLICY_LOW", "RECORD"),
	)

	eng := engine.NewEngine(localScanner, repo, pub, discURL, regURL, polCfg, interval)
	srv := api.NewServer(repo, apiPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			slog.Error("API error", "error", err)
		}
	}()

	go eng.Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	slog.Info("Supply chain engine stopped")
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
