package resolver

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

var ErrUnresolved = errors.New("container identity could not be resolved")

// ResolvedIdentity holds the canonical identity for a container.
type ResolvedIdentity struct {
	AssetID     string
	ImageDigest string
	ResolvedAt  time.Time
}

// Resolver maps Falco container IDs to ZERAXIS AssetIDs and immutable image digests.
type Resolver struct {
	client     *DiscoveryClient
	cache      map[string]*ResolvedIdentity
	cacheTTL   time.Duration
	mu         sync.RWMutex
	unresolved int64
}

// NewResolver creates a resolver with an in-memory cache.
func NewResolver(client *DiscoveryClient, cacheTTL time.Duration) *Resolver {
	return &Resolver{
		client:   client,
		cache:    make(map[string]*ResolvedIdentity),
		cacheTTL: cacheTTL,
	}
}

// Unresolved returns the number of resolution failures.
func (r *Resolver) Unresolved() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.unresolved
}

// Resolve maps a Falco 12-char container ID to an AssetID and image digest.
// Uses cache first, then queries Discovery + Supply Chain APIs.
// On failure, performs bounded exponential backoff retry up to 60 seconds.
func (r *Resolver) Resolve(ctx context.Context, containerID string) (*ResolvedIdentity, error) {
	if containerID == "" || containerID == "host" {
		return nil, ErrUnresolved
	}

	// Check cache
	r.mu.RLock()
	if cached, ok := r.cache[containerID]; ok {
		if time.Since(cached.ResolvedAt) < r.cacheTTL {
			r.mu.RUnlock()
			return cached, nil
		}
	}
	r.mu.RUnlock()

	// Bounded retry with exponential backoff
	backoffs := []time.Duration{0, 2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

	for _, delay := range backoffs {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		identity, err := r.resolveOnce(ctx, containerID)
		if err == nil {
			r.mu.Lock()
			r.cache[containerID] = identity
			r.mu.Unlock()
			return identity, nil
		}

		slog.Debug("Container resolution attempt failed",
			"container_id", containerID,
			"error", err,
			"retry_delay", delay,
		)
	}

	// All retries exhausted
	r.mu.Lock()
	r.unresolved++
	r.mu.Unlock()

	slog.Warn("Container identity resolution failed after all retries",
		"container_id", containerID,
	)
	return nil, ErrUnresolved
}

func (r *Resolver) resolveOnce(ctx context.Context, containerID string) (*ResolvedIdentity, error) {
	assets, err := r.client.FetchAssets(ctx)
	if err != nil {
		return nil, err
	}

	// Find asset by container_id prefix match
	var matched *DiscoveryAsset
	for i := range assets {
		// Falco gives 12-char; Discovery stores 12-char container_id
		if strings.HasPrefix(assets[i].ContainerID, containerID) || strings.HasPrefix(containerID, assets[i].ContainerID) {
			matched = &assets[i]
			break
		}
	}

	if matched == nil {
		return nil, errors.New("container not found in discovery inventory")
	}

	// Fetch immutable image digest from supply chain
	digest, err := r.client.FetchImageDigest(ctx, matched.AssetID)
	if err != nil || digest == "" {
		slog.Debug("Image digest not available from supply chain, falling back to discovery image ID",
			"asset_id", matched.AssetID,
			"error", err,
		)
		// Fallback to the Docker ImageID from discovery if supply chain is unavailable
		digest = matched.ImageID
	}

	return &ResolvedIdentity{
		AssetID:     matched.AssetID,
		ImageDigest: digest,
		ResolvedAt:  time.Now(),
	}, nil
}

// EvictExpired removes cache entries older than TTL.
func (r *Resolver) EvictExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range r.cache {
		if time.Since(v.ResolvedAt) >= r.cacheTTL {
			delete(r.cache, k)
		}
	}
}

// CheckCache provides a non-blocking fast path for resolving identities from cache.
func (r *Resolver) CheckCache(containerID string) (*ResolvedIdentity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.cache[containerID]
	if ok && time.Since(entry.ResolvedAt) < r.cacheTTL {
		return entry, true
	}
	return nil, false
}
