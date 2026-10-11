// Package main is the entry point for the Phase 6.2 Correlation Engine.
//
// Architecture:
//   NATS subscriber (zeraxis.events.detection.finding.>)
//       → Correlator (deterministic incident grouping)
//           → PostgreSQL (durable incident persistence)
//           → NATS publisher (zeraxis.events.correlation.incident.>)
//   HTTP API (read-only, port 8094)
//
// NATS Limitation (Core NATS):
//   This engine uses Core NATS — NOT JetStream.
//   Messages published while this service is offline are NOT replayed on restart.
//   Durable state (incidents table) survives restarts via PostgreSQL.
//   Temporal correlation gaps caused by missed NATS messages are an accepted limitation.
//
// Security Boundary:
//   No Docker socket access.
//   No privileged mode.
//   No host filesystem mounts.
//   No kernel capabilities.
//   No infrastructure mutation of any kind.
//   No automated remediation.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/engines/correlation/pkg/approvals"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/api"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/correlator"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"
	pkgevents "github.com/pavanvanish/zeraxis/pkg/events"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting ZERAXIS Correlation Engine", "version", "0.1.0", "phase", "6.2")

	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/zeraxis?sslmode=disable")
	natsURL := getEnv("NATS_URL", "nats://nats:4222")
	apiPort := getEnv("API_PORT", "8094")
	migrationsDir := getEnv("MIGRATIONS_DIR", "/migrations")

	// ---- PostgreSQL ----
	repo, err := repository.New(dbURL, migrationsDir)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer repo.Close() //nolint:errcheck
	slog.Info("PostgreSQL connected")

	// ---- NATS Publisher ----
	pub, err := pkgevents.NewNATSPublisher(natsURL)
	if err != nil {
		slog.Error("Failed to connect NATS publisher", "error", err)
		os.Exit(1)
	}
	defer pub.Close()
	slog.Info("NATS Publisher connected")

	// ---- Correlator ----
	corr := correlator.New(repo, pub)

	// ---- NATS Subscriber ----
	// Subscribes ONLY to: zeraxis.events.detection.finding.>
	// Does NOT subscribe to: zeraxis.events.correlation.>
	// This prevents NATS feedback loops by design.
	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			slog.Warn("NATS subscriber disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			slog.Info("NATS subscriber reconnected", "url", nc.ConnectedUrl())
		}),
	)
	if err != nil {
		slog.Error("Failed to connect NATS subscriber", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	ctx := context.Background()

	sub, err := nc.Subscribe("zeraxis.events.detection.finding.>", func(m *nats.Msg) {
		// Fire-and-forget goroutine per message to avoid blocking the NATS dispatcher.
		// Bounded by NATS message queue semantics.
		go func(data []byte) {
			corr.HandleFinding(ctx, data)
		}(m.Data)
	})
	if err != nil {
		slog.Error("Failed to subscribe to findings", "error", err)
		os.Exit(1)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	execSub, err := nc.Subscribe("zeraxis.events.execution.audit", func(m *nats.Msg) {
		go func(data []byte) {
			corr.HandleExecutionAudit(ctx, data)
		}(m.Data)
	})
	if err != nil {
		slog.Error("Failed to subscribe to execution audit events", "error", err)
		os.Exit(1)
	}
	defer execSub.Unsubscribe() //nolint:errcheck

	slog.Info("Subscribed to NATS findings",
		"subject", "zeraxis.events.detection.finding.>",
		"note", "Core NATS: missed messages during downtime are NOT replayed",
	)


	// ---- Approvals Service ----
	approvalSvc := approvals.NewService(repo.DB(), nc)

	// ---- HTTP API ----
	srv := api.New(repo, corr, approvalSvc, apiPort)

	srv.Start()

	// ---- Metrics logger goroutine ----
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			m := corr.Metrics()
			b, _ := json.Marshal(m)
			slog.Info("Correlator metrics", "metrics", string(b))
		}
	}()

	// ---- Shutdown ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	slog.Info("Correlation Engine shutting down")
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
