package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	
	"github.com/pavanvanish/zeraxis/engines/executor/internal/docker"
	"github.com/pavanvanish/zeraxis/engines/executor/internal/engine"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	natsURL := os.Getenv("NATS_URL")

	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	if natsURL == "" {
		log.Fatal("NATS_URL is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to open db: %v", err)
	}
	defer db.Close()
	
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	dockerClient, err := docker.NewRealClient()
	if err != nil {
		log.Fatalf("Failed to initialize typed docker client: %v", err)
	}

	execEngine := engine.NewExecutor(db, dockerClient, nc)

	// Subscribe exactly to authorization event
	sub, err := nc.Subscribe("zeraxis.events.execution.authorized", func(msg *nats.Msg) {
		execEngine.HandleNatsWakeup(context.Background(), msg.Data)
	})
	if err != nil {
		log.Fatalf("NATS subscription failed: %v", err)
	}

	// Health and Readiness
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "DB down", http.StatusServiceUnavailable)
			return
		}
		if !nc.IsConnected() {
			http.Error(w, "NATS down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})

	server := &http.Server{
		Addr: ":8095", // Executor port
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Println("Executor Engine started. Listening for authorizations on NATS.")

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down Executor Engine...")
	sub.Unsubscribe()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	log.Println("Shutdown complete.")
}
