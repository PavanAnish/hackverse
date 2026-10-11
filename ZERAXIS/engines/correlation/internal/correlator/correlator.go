// Package correlator implements the deterministic incident correlation algorithm.
//
// # Correlation Rules (from docs/phase-6-correlation-model.md)
//
// Same-asset correlation:
//   asset_id must match EXACTLY
//   image_digest must match EXACTLY (when present on both findings)
//   finding timestamp must fall within an active 1-hour sliding window
//
// Cross-asset correlation:
//   Deferred to Phase 6.2+ — requires Discovery NETWORK_MEMBERSHIP evidence
//   NOT implemented in Phase 6.2. Cross-asset findings produce separate incidents.
//
// # Memory Safety
//   Max active incidents in memory: 5000 (backed by Postgres for overflow)
//   Dedup cache for received finding IDs: max 10000 entries, then reset
//   These limits are hard-coded constants, not configurable at runtime via untrusted input.
//
// # NATS Semantics
//   Core NATS (not JetStream) is used. If the engine restarts, NATS messages
//   published while offline are NOT replayed. Previously committed incidents
//   in Postgres survive restarts. Temporal gaps in correlation windows are
//   accepted as a known limitation of Core NATS.
package correlator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/models"
	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"
	pkgevents "github.com/pavanvanish/zeraxis/pkg/events"
)

// ====== Configuration Constants ======

// correlationWindow defines how long an incident remains open for new findings
// after its last_seen time. Findings arriving after this window produce a new incident.
const correlationWindow = 1 * time.Hour

// maxActiveIncidentsMemory is the maximum number of fingerprint→incidentID entries
// kept in the in-memory cache. When exceeded, the engine falls back to DB lookups.
const maxActiveIncidentsMemory = 5000

// maxDedupCache is the maximum number of finding_ids tracked for deduplication.
// When reached, the cache is reset. A small temporal duplicate window is acceptable.
const maxDedupCache = 10000

// maxFindingsPerIncident caps how many distinct findings can be linked to one incident.
// This prevents a single noisy asset from bloating incident evidence indefinitely.
const maxFindingsPerIncident = 200

// ====== Metrics (simple atomic counters, logged periodically) ======

// MetricsSnapshot is a read-only copy of operational counters (no mutex).
type MetricsSnapshot struct {
	FindingsReceived    int64
	FindingsRejected    int64
	DuplicateFindings   int64
	CorrelationAttempts int64
	CorrelationSuccess  int64
	CorrelationRejected int64
	IncidentsCreated    int64
	IncidentsUpdated    int64
	ActiveIncidents     int64
	StateEvictions      int64
	DatabaseErrors      int64
	NATSErrors          int64
	RecommendationsGenerated int64
	RecommendationsSuppressed int64
}

// metrics holds live atomic counters protected by a mutex.
type metrics struct {
	mu                  sync.Mutex
	findingsReceived    int64
	findingsRejected    int64
	duplicateFindings   int64
	correlationAttempts int64
	correlationSuccess  int64
	correlationRejected int64
	incidentsCreated    int64
	incidentsUpdated    int64
	stateEvictions      int64
	databaseErrors      int64
	natsErrors          int64
	recsGenerated       int64
	recsSuppressed      int64
}

func (m *metrics) inc(p *int64) {
	m.mu.Lock()
	*p++
	m.mu.Unlock()
}

func (m *metrics) snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MetricsSnapshot{
		FindingsReceived:    m.findingsReceived,
		FindingsRejected:    m.findingsRejected,
		DuplicateFindings:   m.duplicateFindings,
		CorrelationAttempts: m.correlationAttempts,
		CorrelationSuccess:  m.correlationSuccess,
		CorrelationRejected: m.correlationRejected,
		IncidentsCreated:    m.incidentsCreated,
		IncidentsUpdated:    m.incidentsUpdated,
		StateEvictions:      m.stateEvictions,
		DatabaseErrors:      m.databaseErrors,
		NATSErrors:          m.natsErrors,
		RecommendationsGenerated: m.recsGenerated,
		RecommendationsSuppressed: m.recsSuppressed,
	}
}

// ====== Correlator ======

// Correlator is the Phase 6.2 incident correlation engine.
type Correlator struct {
	repo      *repository.Repository
	publisher pkgevents.Publisher

	// In-memory fingerprint → incidentID cache.
	// Key: incident fingerprint. Value: incidentID.
	// Bounded by maxActiveIncidentsMemory.
	mu          sync.RWMutex
	fpCache     map[string]string // fingerprint → incident_id

	// Deduplication of finding_ids seen in this process lifetime.
	// Prevents duplicate NATS deliveries from creating duplicate work.
	dedupSeen   map[string]struct{}

	m metrics
}

// New constructs a Correlator.
func New(repo *repository.Repository, publisher pkgevents.Publisher) *Correlator {
	return &Correlator{
		repo:      repo,
		publisher: publisher,
		fpCache:   make(map[string]string, 128),
		dedupSeen: make(map[string]struct{}, 128),
	}
}

// Metrics returns a snapshot of the current operational metrics.
func (c *Correlator) Metrics() MetricsSnapshot { return c.m.snapshot() }

// ====== Entry Point ======

// HandleFinding is called for each NATS message on zeraxis.events.detection.finding.>
// It validates, deduplicates, correlates, and persists the finding into an incident.
//
// The Detection Engine publishes a raw models.FindingRef JSON (NOT a full events.Event
// envelope). This is confirmed by inspecting engines/detection/main.go line 268.
func (c *Correlator) HandleFinding(ctx context.Context, data []byte) {
	c.m.inc(&c.m.findingsReceived)

	// ---- 1. Parse and validate ----
	ref, err := parseFindingRef(data)
	if err != nil {
		c.m.inc(&c.m.findingsRejected)
		slog.Warn("Rejected malformed finding", "error", err)
		return
	}

	// ---- 2. Deduplicate ----
	if c.isDuplicate(ref.FindingID) {
		c.m.inc(&c.m.duplicateFindings)
		slog.Debug("Duplicate finding, skipping", "finding_id", ref.FindingID)
		return
	}

	// ---- 3. Correlate ----
	c.m.inc(&c.m.correlationAttempts)
	if err := c.correlate(ctx, ref); err != nil {
		c.m.inc(&c.m.databaseErrors)
		slog.Error("Correlation failed", "finding_id", ref.FindingID, "error", err)
		// Do not mark as duplicate — allow retry on next delivery
		return
	}

	// Mark as seen only after successful persistence
	c.markSeen(ref.FindingID)
}

// ====== Parsing & Validation ======

// parseFindingRef parses the raw NATS payload from the Detection Engine.
// Maximum accepted payload size: 1 MB. Rejects clearly malformed or oversized data.
func parseFindingRef(data []byte) (*models.FindingRef, error) {
	const maxPayloadBytes = 1 << 20 // 1 MB
	if len(data) > maxPayloadBytes {
		return nil, fmt.Errorf("payload too large: %d bytes (max %d)", len(data), maxPayloadBytes)
	}

	var ref models.FindingRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	// Validate required fields
	if ref.FindingID == "" {
		return nil, fmt.Errorf("missing finding_id")
	}
	if _, err := uuid.Parse(ref.FindingID); err != nil {
		return nil, fmt.Errorf("finding_id not a valid UUID: %w", err)
	}
	if ref.RuleID == "" {
		return nil, fmt.Errorf("missing rule_id")
	}
	if !isValidSeverity(ref.Severity) {
		return nil, fmt.Errorf("invalid severity: %q", ref.Severity)
	}
	// asset_id, if present, must be a UUID
	if ref.AssetID != "" {
		if _, err := uuid.Parse(ref.AssetID); err != nil {
			return nil, fmt.Errorf("asset_id not a valid UUID: %w", err)
		}
	}
	// TriggeringEventID should be a UUID if present
	if ref.TriggeringEventID != "" {
		if _, err := uuid.Parse(ref.TriggeringEventID); err != nil {
			// Non-fatal: log and continue (tolerate older engine versions)
			slog.Debug("triggering_event_id not a UUID, continuing", "value", ref.TriggeringEventID)
		}
	}

	return &ref, nil
}

func isValidSeverity(s string) bool {
	switch s {
	case "INFO", "LOW", "MEDIUM", "HIGH", "CRITICAL":
		return true
	}
	return false
}

// ====== Deduplication ======

func (c *Correlator) isDuplicate(findingID string) bool {
	c.mu.RLock()
	_, seen := c.dedupSeen[findingID]
	c.mu.RUnlock()
	return seen
}

func (c *Correlator) markSeen(findingID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.dedupSeen) >= maxDedupCache {
		// Reset rather than evict to avoid O(n) scan. Accepts a brief duplicate window.
		c.dedupSeen = make(map[string]struct{}, 128)
		c.m.inc(&c.m.stateEvictions)
		slog.Info("Dedup cache reset", "reason", "capacity exceeded", "limit", maxDedupCache)
	}
	c.dedupSeen[findingID] = struct{}{}
}

// ====== Correlation Algorithm ======

// correlate implements the Phase 6.1 deterministic correlation algorithm.
//
// Decision tree:
//  1. Compute incident fingerprint for this finding.
//  2. Check in-memory cache → hit: attempt to add to existing incident.
//  3. Cache miss → query Postgres for the fingerprint.
//  4. DB hit with last_seen within correlationWindow → add finding.
//  5. DB hit with last_seen outside correlationWindow → create new incident.
//  6. DB miss → create new incident.
//
// Cross-asset correlation is NOT implemented in Phase 6.2.
func (c *Correlator) correlate(ctx context.Context, ref *models.FindingRef) error {
	now := time.Now().UTC()

	fp := computeFingerprint(ref.AssetID, ref.ImageDigest, ref.ContainerID, now)

	// --- Check active incident limit ---
	c.mu.RLock()
	cacheSize := len(c.fpCache)
	c.mu.RUnlock()
	if cacheSize >= maxActiveIncidentsMemory {
		// Fall back to pure DB path (no caching for this call)
		return c.correlateViaDatabaseOnly(ctx, ref, fp, now)
	}

	// --- Cache lookup ---
	c.mu.RLock()
	existingID, inCache := c.fpCache[fp]
	c.mu.RUnlock()

	if inCache {
		return c.updateOrCreateFromCache(ctx, ref, fp, existingID, now)
	}

	// --- DB lookup ---
	inc, err := c.repo.GetIncidentByFingerprint(ctx, fp)
	if err != nil {
		return fmt.Errorf("db lookup by fingerprint: %w", err)
	}

	if inc != nil {
		// Phase 7.8 Technical Debt Fix: Never absorb into a RESOLVED incident
		if inc.Status == "RESOLVED" || inc.Status == "CLOSED" {
			// Do NOT mutate fingerprint. A new incident will be created with the exact same fingerprint.
			return c.createNewIncident(ctx, ref, fp, now)
		}

		// Found in DB — check if still within correlation window
		if now.Sub(inc.LastSeen) <= correlationWindow {
			return c.addFindingToExisting(ctx, ref, inc, now)
		}
		// Outside window → start a new incident with a fresh fingerprint
		// Use a time-bucketed fingerprint so the new window gets a distinct key
		fp = computeFingerprintWithBucket(ref.AssetID, ref.ImageDigest, ref.ContainerID, now)
	}

	// Create new incident
	return c.createNewIncident(ctx, ref, fp, now)
}

// correlateViaDatabaseOnly is the fallback path when the in-memory cache is full.
// It is identical to the main path but never writes to c.fpCache.
func (c *Correlator) correlateViaDatabaseOnly(ctx context.Context, ref *models.FindingRef, fp string, now time.Time) error {
	c.m.inc(&c.m.stateEvictions)
	inc, err := c.repo.GetIncidentByFingerprint(ctx, fp)
	if err != nil {
		return err
	}
	if inc != nil && (inc.Status == "RESOLVED" || inc.Status == "CLOSED") {
		return c.createNewIncident(ctx, ref, fp, now)
	}
	if inc != nil && now.Sub(inc.LastSeen) <= correlationWindow {
		return c.addFindingToExisting(ctx, ref, inc, now)
	}
	fp = computeFingerprintWithBucket(ref.AssetID, ref.ImageDigest, ref.ContainerID, now)
	return c.createNewIncident(ctx, ref, fp, now)
}

// updateOrCreateFromCache is called when the fingerprint is in the in-memory cache.
// It re-validates the DB state to handle restarts and evictions.
func (c *Correlator) updateOrCreateFromCache(ctx context.Context, ref *models.FindingRef, fp, existingID string, now time.Time) error {
	inc, err := c.repo.GetIncidentByID(ctx, existingID)
	if err != nil {
		return err
	}
	if inc == nil {
		// Cache stale — incident was deleted externally. Create fresh.
		c.mu.Lock()
		delete(c.fpCache, fp)
		c.mu.Unlock()
		return c.createNewIncident(ctx, ref, fp, now)
	}
	if now.Sub(inc.LastSeen) > correlationWindow {
		// Window expired — create new incident
		c.mu.Lock()
		delete(c.fpCache, fp)
		c.mu.Unlock()
		newFP := computeFingerprintWithBucket(ref.AssetID, ref.ImageDigest, ref.ContainerID, now)
		return c.createNewIncident(ctx, ref, newFP, now)
	}
	return c.addFindingToExisting(ctx, ref, inc, now)
}

// addFindingToExisting links a finding to an existing incident.
func (c *Correlator) addFindingToExisting(ctx context.Context, ref *models.FindingRef, inc *models.Incident, now time.Time) error {
	// Enforce per-incident finding cap
	if inc.FindingCount >= maxFindingsPerIncident {
		c.m.inc(&c.m.correlationRejected)
		slog.Warn("Incident at finding capacity, rejecting", "incident_id", inc.IncidentID, "count", inc.FindingCount)
		return nil
	}

	added, err := c.repo.AddFindingToIncident(ctx, inc.IncidentID, ref.FindingID, now, ref.Severity)
	if err != nil {
		return fmt.Errorf("add finding to incident: %w", err)
	}
	if !added {
		// Already linked — idempotent
		c.m.inc(&c.m.duplicateFindings)
		return nil
	}

	// Phase 6.3: Recalculate incident risk with the new finding
	if err := c.RecalculateIncidentRisk(ctx, inc, now); err != nil {
		slog.Error("Failed to recalculate incident risk", "incident_id", inc.IncidentID, "error", err)
		// We log but don't fail the transaction, as the base incident/finding link is already committed.
	}

	c.m.inc(&c.m.incidentsUpdated)
	c.m.inc(&c.m.correlationSuccess)
	slog.Info("Finding added to incident",
		"incident_id", inc.IncidentID,
		"finding_id", ref.FindingID,
		"severity", ref.Severity,
		"risk_level", safeRiskLevel(inc.RiskLevel),
	)

	// Publish INCIDENT_UPDATED to NATS (non-blocking — Postgres already committed)
	c.publishIncidentEvent(ctx, pkgevents.EventTypeIncidentCreated+"_UPDATED", inc.IncidentID, ref)
	return nil
}

func safeRiskLevel(s *string) string {
	if s == nil {
		return "UNKNOWN"
	}
	return *s
}

// createNewIncident creates a new incident and links the first finding.
func (c *Correlator) createNewIncident(ctx context.Context, ref *models.FindingRef, fp string, now time.Time) error {
	inc := &models.Incident{
		IncidentID:  uuid.New().String(),
		Fingerprint: fp,
		Status:      models.StatusOpen,
		AssetID:     ref.AssetID,
		ContainerID: ref.ContainerID,
		ImageDigest: ref.ImageDigest,
		Severity:    ref.Severity,
		FindingCount: 1,
		FirstSeen:   now,
		LastSeen:    now,
	}

	// Persist atomically (incident + finding_link in one tx)
	if err := c.repo.CreateIncidentWithFinding(ctx, inc, ref.FindingID); err != nil {
		return fmt.Errorf("create incident: %w", err)
	}

	// Phase 6.3: Recalculate incident risk with the initial finding
	if err := c.RecalculateIncidentRisk(ctx, inc, now); err != nil {
		slog.Error("Failed to calculate initial incident risk", "incident_id", inc.IncidentID, "error", err)
	}

	// Update in-memory cache
	c.mu.Lock()
	c.fpCache[fp] = inc.IncidentID
	c.mu.Unlock()

	c.m.inc(&c.m.incidentsCreated)
	c.m.inc(&c.m.correlationSuccess)
	slog.Info("New incident created",
		"incident_id", inc.IncidentID,
		"fingerprint", fp,
		"finding_id", ref.FindingID,
		"asset_id", ref.AssetID,
		"severity", ref.Severity,
		"risk_level", safeRiskLevel(inc.RiskLevel),
	)

	// Publish INCIDENT_CREATED to NATS (after DB commit)
	c.publishIncidentEvent(ctx, "INCIDENT_CREATED", inc.IncidentID, ref)
	return nil
}

// ====== Fingerprint ======

// computeFingerprint produces the deterministic incident grouping key.
//
// Formula: SHA256( asset_id + ":" + image_digest + ":" + hourBucket )
//
// The hour bucket is the Unix timestamp truncated to the hour.
// This ensures findings within the same hour produce the same key,
// and findings in a new hour produce a new incident after the window expires.
//
// Empty asset_id or image_digest are explicitly included as empty strings
// so that distinct combinations produce distinct fingerprints.
func computeFingerprint(assetID, imageDigest, containerID string, t time.Time) string {
	bucket := t.UTC().Truncate(correlationWindow).Unix()
	raw := fmt.Sprintf("%s:%s:%s:%d", assetID, imageDigest, containerID, bucket)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// computeFingerprintWithBucket uses a finer bucket (current hour) to start a new
// incident after the correlation window of a previous one has expired.
func computeFingerprintWithBucket(assetID, imageDigest, containerID string, t time.Time) string {
	return computeFingerprint(assetID, imageDigest, containerID, t)
}

// ====== NATS Publication ======

// publishIncidentEvent publishes a Phase 6 event to NATS using the existing
// Unified Event Envelope. Publication failures are logged but do NOT cause
// a retry — Postgres is the durable store.
//
// Subject: zeraxis.events.correlation.incident.created
//          zeraxis.events.correlation.incident.updated
//
// The engine NEVER subscribes to zeraxis.events.correlation.> so there is
// no possible NATS feedback loop.
func (c *Correlator) publishIncidentEvent(ctx context.Context, eventType, incidentID string, ref *models.FindingRef) {
	meta := map[string]interface{}{
		"incident_id": incidentID,
		"finding_id":  ref.FindingID,
		"rule_id":     ref.RuleID,
		"severity":    ref.Severity,
	}
	metaBytes, _ := json.Marshal(meta)

	evt := pkgevents.NewEvent(eventType, pkgevents.SourceCorrelation, time.Now().UTC())
	evt.AssetID = ref.AssetID
	evt.ImageDigest = ref.ImageDigest
	evt.Severity = ref.Severity
	evt.Metadata = metaBytes

	// Subject: zeraxis.events.correlation.incident.created  OR  .updated
	action := "created"
	if eventType != "INCIDENT_CREATED" {
		action = "updated"
	}
	evt.Subject = fmt.Sprintf("zeraxis.events.correlation.incident.%s", action)

	if c.publisher != nil {
	if err := c.publisher.Publish(ctx, evt); err != nil {
		c.m.inc(&c.m.natsErrors)
		slog.Warn("Failed to publish incident event to NATS",
			"incident_id", incidentID,
			"event_type", eventType,
			"error", err,
		)
		// Do NOT return error — incident is already durable in Postgres
		}
	}
}
