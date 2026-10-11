package normalizer

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/pavanvanish/zeraxis/engines/runtime/internal/collector"
	"github.com/pavanvanish/zeraxis/engines/runtime/internal/resolver"
	"github.com/pavanvanish/zeraxis/pkg/events"
)

// ruleToEventType maps Falco rule names to ZERAXIS event types.
var ruleToEventType = map[string]string{
	"ZERAXIS_PROCESS_EXEC": events.EventTypeProcessExecution,
	"ZERAXIS_NET_CONNECT":  events.EventTypeNetworkConnection,
	"ZERAXIS_FILE_WRITE":   events.EventTypeFileActivity,
}

// Normalizer transforms RawFalcoEvents into canonical ZERAXIS events.
type Normalizer struct {
	resolver *resolver.Resolver
}

// NewNormalizer creates a normalizer with a container identity resolver.
func NewNormalizer(resolver *resolver.Resolver) *Normalizer {
	return &Normalizer{resolver: resolver}
}

// Normalize transforms a raw Falco event into a unified ZERAXIS event, applying retry resolution.
func (n *Normalizer) Normalize(ctx context.Context, raw *collector.RawFalcoEvent) *events.Event {
	return n.normalizeInternal(ctx, raw, false)
}

// NormalizeBypassed transforms a raw Falco event skipping retries.
func (n *Normalizer) NormalizeBypassed(ctx context.Context, raw *collector.RawFalcoEvent) *events.Event {
	return n.normalizeInternal(ctx, raw, true)
}

func (n *Normalizer) normalizeInternal(ctx context.Context, raw *collector.RawFalcoEvent, bypassRetries bool) *events.Event {
	eventType, ok := ruleToEventType[raw.Rule]
	if !ok {
		return nil // Not a ZERAXIS rule
	}

	containerID := raw.GetStringField("container.id")
	if containerID == "" || containerID == "host" || strings.HasPrefix(containerID, "buildkit") {
		return nil // Host-level event, skip
	}

	assetID := "UNRESOLVED"
	imageDigest := "UNRESOLVED"

	if bypassRetries {
		// Just do a quick one-shot resolve without retries. 
		// If it fails, it remains UNRESOLVED explicitly.
		// Wait, the resolver.Resolve method has retries hardcoded. 
		// We'll add ResolveFast to resolver, or we just leave it UNRESOLVED if bypassed.
		// Bypassed means the queue is completely full, we just emit it as UNRESOLVED.
		// Let's at least check the cache first.
		if ident, ok := n.resolver.CheckCache(containerID); ok {
			assetID = ident.AssetID
			imageDigest = ident.ImageDigest
		} else {
			slog.Warn("Event bypassed resolution queue, emitting explicitly as UNRESOLVED", "container_id", containerID)
		}
	} else {
		identity, err := n.resolver.Resolve(ctx, containerID)
		if err != nil {
			slog.Warn("Unresolved runtime event after retries",
				"container_id", containerID,
				"rule", raw.Rule,
				"error", err,
			)
			// Explicitly mark as UNRESOLVED instead of dropping
		} else {
			assetID = identity.AssetID
			imageDigest = identity.ImageDigest
		}
	}

	timestamp, err := time.Parse(time.RFC3339Nano, raw.Time)
	if err != nil {
		timestamp = time.Now()
	}

	evt := events.NewEvent(eventType, events.SourceRuntime, timestamp)
	if assetID != "UNRESOLVED" { evt.AssetID = assetID }
	if imageDigest != "UNRESOLVED" { evt.ImageDigest = imageDigest }
	evt.Severity = events.SeverityInfo
	evt.Subject = events.GenerateSubject(events.SourceRuntime, eventType)

	var metadata map[string]interface{}
	switch eventType {
	case events.EventTypeProcessExecution:
		metadata = n.buildProcessMetadata(raw)
	case events.EventTypeNetworkConnection:
		metadata = n.buildNetworkMetadata(raw)
	case events.EventTypeFileActivity:
		metadata = n.buildFileMetadata(raw)
	}
	metadata["container_id"] = containerID
	if assetID == "UNRESOLVED" {
		metadata["resolution_status"] = "UNRESOLVED"
	}

	metaBytes, _ := json.Marshal(metadata)
	evt.Metadata = metaBytes

	return evt
}

func (n *Normalizer) buildProcessMetadata(raw *collector.RawFalcoEvent) map[string]interface{} {
	return map[string]interface{}{
		"process":    raw.GetStringField("proc.name"),
		"executable": raw.GetStringField("proc.exepath"),
		"command":    raw.GetStringField("proc.cmdline"),
		"pid":        raw.GetIntField("proc.pid"),
		"parent_pid": raw.GetIntField("proc.ppid"),
		"uid":        raw.GetIntField("user.uid"),
		"user":       raw.GetStringField("user.name"),
	}
}

func (n *Normalizer) buildNetworkMetadata(raw *collector.RawFalcoEvent) map[string]interface{} {
	fd := raw.GetStringField("fd.name")
	protocol := raw.GetStringField("fd.l4proto")
	process := raw.GetStringField("proc.name")

	meta := map[string]interface{}{
		"process":  process,
		"protocol": protocol,
		"fd_name":  fd,
	}

	if parts := strings.Split(fd, "->"); len(parts) == 2 {
		srcParts := splitHostPort(parts[0])
		dstParts := splitHostPort(parts[1])
		meta["source_ip"] = srcParts[0]
		meta["source_port"] = srcParts[1]
		meta["destination_ip"] = dstParts[0]
		meta["destination_port"] = dstParts[1]
		meta["direction"] = "egress"
	}

	return meta
}

func (n *Normalizer) buildFileMetadata(raw *collector.RawFalcoEvent) map[string]interface{} {
	return map[string]interface{}{
		"operation": "WRITE",
		"path":      raw.GetStringField("fd.name"),
		"process":   raw.GetStringField("proc.name"),
		"pid":       raw.GetIntField("proc.pid"),
		"uid":       raw.GetIntField("user.uid"),
	}
}

func splitHostPort(s string) [2]string {
	s = strings.TrimSpace(s)
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return [2]string{s, ""}
	}
	return [2]string{s[:idx], s[idx+1:]}
}
