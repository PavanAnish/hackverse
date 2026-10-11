package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

func main() {
	persistent := flag.Bool("persistent", false, "Run persistently instead of exiting after one event")
	flag.Parse()

	fmt.Println("========================================")
	fmt.Println("  ZERAXIS Phase 4 NATS Consumer Test  ")
	fmt.Println("========================================")

	url := "nats://127.0.0.1:4222"
	if envUrl := os.Getenv("NATS_URL"); envUrl != "" {
		url = envUrl
	}

	nc, err := nats.Connect(url, nats.Timeout(5*time.Second))
	if err != nil {
		log.Fatalf("❌ Failed to connect to NATS at %s: %v", url, err)
	}
	defer nc.Close()

	fmt.Printf("✅ Connected to NATS: %s\n", nc.ConnectedUrl())

	subject := "zeraxis.events.>"
	
	// Channel to signal a valid event was received
	doneCh := make(chan struct{})

	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		fmt.Printf("\n[Event Received] Subject: %s\n", m.Subject)
		fmt.Printf("Payload: %s\n", string(m.Data))

		// Attempt to deserialize into canonical event struct
		var evt events.Event
		if err := json.Unmarshal(m.Data, &evt); err != nil {
			fmt.Printf("❌ Failed to deserialize event: %v\n", err)
			return
		}

		// Validate required fields
		if err := evt.Validate(); err != nil {
			fmt.Printf("❌ Event validation failed: %v\n", err)
			return
		}

		fmt.Println("✅ Event successfully validated!")
		fmt.Printf("  Event ID:      %s\n", evt.EventID)
		fmt.Printf("  Event Type:    %s\n", evt.EventType)
		fmt.Printf("  Event Version: %d\n", evt.EventVersion)
		fmt.Printf("  Timestamp:     %v\n", evt.Timestamp)
		fmt.Printf("  Source:        %s\n", evt.Source)
		fmt.Printf("  Subject:       %s\n", evt.Subject)
		
		if !*persistent {
			select {
			case doneCh <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		log.Fatalf("❌ Failed to subscribe to %s: %v", subject, err)
	}
	defer sub.Unsubscribe()

	fmt.Printf("✅ Subscribed to: %s\n", subject)
	
	if *persistent {
		fmt.Println("Running in persistent mode. Waiting for events... (Press Ctrl+C to exit)")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nExiting.")
		return
	}

	fmt.Println("Waiting up to 60 seconds for a valid event...")

	// 60 seconds timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	select {
	case <-doneCh:
		fmt.Println("\n✅ Verification successful! A valid unified event was received and processed.")
		os.Exit(0)
	case <-ctx.Done():
		fmt.Println("\n❌ Verification failed! Timed out after 60 seconds waiting for a valid unified event.")
		os.Exit(1)
	}
}
