package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// Publisher interface abstracting NATS
type Publisher interface {
	Publish(ctx context.Context, event *Event) error
	Close()
}

type NATSPublisher struct {
	nc *nats.Conn
}

// NewNATSPublisher creates a new publisher with a long-lived NATS connection.
// It uses reconnect logic automatically handled by the nats.go client.
func NewNATSPublisher(url string) (*NATSPublisher, error) {
	nc, err := nats.Connect(url, 
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			slog.Warn("NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			slog.Info("NATS reconnected", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			slog.Info("NATS connection closed")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &NATSPublisher{nc: nc}, nil
}

// Publish validates and serializes the event, then publishes it to NATS.
func (p *NATSPublisher) Publish(ctx context.Context, event *Event) error {
	// Validate
	if err := event.Validate(); err != nil {
		return fmt.Errorf("event validation failed: %w", err)
	}

	// Serialize
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Publish
	// Note: NATS pub/sub is at-most-once delivery (best-effort transport).
	// JetStream is not used in Phase 4.
	if err := p.nc.Publish(event.Subject, data); err != nil {
		return fmt.Errorf("nats publish failed: %w", err)
	}

	slog.Debug("Event published to NATS", 
		"event_id", event.EventID, 
		"event_type", event.EventType, 
		"subject", event.Subject,
	)
	return nil
}

func (p *NATSPublisher) Close() {
	if p.nc != nil {
		p.nc.Close()
	}
}

// NoopPublisher is used when NATS is disabled or for testing
type NoopPublisher struct{}

func (p *NoopPublisher) Publish(ctx context.Context, event *Event) error {
	return nil
}

func (p *NoopPublisher) Close() {}
