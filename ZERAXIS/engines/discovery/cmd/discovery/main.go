package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pavanvanish/zeraxis/engines/discovery/internal/api"
	"github.com/pavanvanish/zeraxis/engines/discovery/internal/discovery"
	dockerclient "github.com/pavanvanish/zeraxis/engines/discovery/internal/docker"
	"github.com/pavanvanish/zeraxis/engines/discovery/internal/repository"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func main() {
	// Structured JSON logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting ZERAXIS Discovery Engine", "version", "0.1.0")

	// Configuration from environment
	databaseURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable")
	intervalStr := getEnv("DISCOVERY_INTERVAL", "30s")
	apiPort := getEnv("API_PORT", "8090")
	migrationsDir := getEnv("MIGRATIONS_DIR", "/migrations")

	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		slog.Error("Invalid DISCOVERY_INTERVAL", "value", intervalStr, "error", err)
		os.Exit(1)
	}

	// Initialize Docker client
	docker, err := dockerclient.NewClient()
	if err != nil {
		slog.Error("Failed to create Docker client", "error", err)
		os.Exit(1)
	}
	defer docker.Close()

	// Verify Docker connectivity
	ctx := context.Background()
	if err := docker.Ping(ctx); err != nil {
		slog.Error("Failed to connect to Docker daemon", "error", err)
		slog.Error("Ensure the Docker socket is mounted: -v /var/run/docker.sock:/var/run/docker.sock:ro")
		os.Exit(1)
	}
	slog.Info("Docker daemon connected")

	// Initialize PostgreSQL
	repo, err := repository.NewPostgres(databaseURL, migrationsDir)
	if err != nil {
		slog.Error("Failed to initialize PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer repo.Close()
	slog.Info("PostgreSQL connected, migrations applied")

	// Initialize NATS Publisher
	natsURL := getEnv("NATS_URL", "nats://nats:4222")
	pub, err := events.NewNATSPublisher(natsURL)
	if err != nil {
		slog.Error("Failed to initialize NATS publisher", "error", err)
		os.Exit(1)
	}
	defer pub.Close()
	slog.Info("NATS Publisher connected")

	// Create discovery components
	scanner := discovery.NewScanner(docker)
	reconciler := discovery.NewReconciler()
	loop := discovery.NewLoop(scanner, reconciler, repo, pub, interval)

	// Create API server
	server := api.NewServer(repo, apiPort)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Start API server in background
	go func() {
		if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
			slog.Error("API server error", "error", err)
		}
	}()

	// Start discovery loop in background
	go func() {
		loop.Run(ctx)
	}()

	slog.Info("Discovery engine running",
		"api_port", apiPort,
		"discovery_interval", interval.String(),
	)

	// Wait for shutdown signal
	sig := <-sigCh
	slog.Info("Received shutdown signal", "signal", sig)
	cancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("API server shutdown error", "error", err)
	}

	slog.Info("Discovery engine stopped")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
