package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

type NATSInvalidationListener struct {
	nc    *nats.Conn
	cache *MaterializedView
	sub   *nats.Subscription
}

func NewNATSInvalidationListener(nc *nats.Conn, cache *MaterializedView) *NATSInvalidationListener {
	return &NATSInvalidationListener{
		nc:    nc,
		cache: cache,
	}
}

func (l *NATSInvalidationListener) Start(ctx context.Context) error {
	sub, err := l.nc.Subscribe("zeraxis.events.correlation.incident.resolved", func(msg *nats.Msg) {
		var envelope events.Event
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			return
		}

		var meta struct {
			Fingerprint string `json:"fingerprint"`
		}
		if envelope.Metadata != nil {
			json.Unmarshal(envelope.Metadata, &meta)
		}

		if meta.Fingerprint != "" {
			err := l.cache.Invalidate(context.Background(), meta.Fingerprint)
			if err != nil {
				log.Printf("Failed to invalidate cache for fingerprint %s: %v", meta.Fingerprint, err)
			}
		}
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to invalidation events: %w", err)
	}
	l.sub = sub
	return nil
}

func (l *NATSInvalidationListener) Stop() error {
	if l.sub != nil {
		return l.sub.Unsubscribe()
	}
	return nil
}
