package multicluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type Cache interface {
	Invalidate(fingerprint string) error
}

type IncidentResolvedEvent struct {
	ClusterID   string    `json:"cluster_id"`
	Fingerprint string    `json:"fingerprint"`
	Timestamp   time.Time `json:"timestamp"`
}

type NATSCoherencyManager struct {
	nc        *nats.Conn
	cache     Cache
	clusterID string
	sub       *nats.Subscription
}

func NewNATSCoherencyManager() *NATSCoherencyManager {
	return &NATSCoherencyManager{}
}

func (m *NATSCoherencyManager) Start(nc *nats.Conn, cache Cache, clusterID string) error {
	m.nc = nc
	m.cache = cache
	m.clusterID = clusterID

	// Wildcard subscription for cross-cluster invalidation
	subject := "zeraxis.events.*.correlation.incident.resolved"
	sub, err := m.nc.Subscribe(subject, func(msg *nats.Msg) {
		var evt IncidentResolvedEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			return
		}

		// Only invalidate cache if the event originated from a different cluster.
		// Local incidents are handled instantly by the local evaluation engine before event dispatch.
		if evt.ClusterID != m.clusterID {
			m.cache.Invalidate(evt.Fingerprint)
		}
	})
	if err != nil {
		return err
	}
	m.sub = sub
	m.nc.Flush()
	return nil
}

func (m *NATSCoherencyManager) PublishResolved(ctx context.Context, fingerprint string) error {
	evt := IncidentResolvedEvent{
		ClusterID:   m.clusterID,
		Fingerprint: fingerprint,
		Timestamp:   time.Now(),
	}
	payload, _ := json.Marshal(evt)
	
	// Publish to own cluster's namespace
	subject := fmt.Sprintf("zeraxis.events.%s.correlation.incident.resolved", m.clusterID)
	return m.nc.Publish(subject, payload)
}

func (m *NATSCoherencyManager) Stop() {
	if m.sub != nil {
		m.sub.Unsubscribe()
	}
}
