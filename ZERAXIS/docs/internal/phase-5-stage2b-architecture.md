# ZERAXIS Phase 5 Stage 2B Architecture Report

## 1. Executive Summary
Phase 5 Stage 2B introduces Temporal Correlation to the ZERAXIS Detection Engine. Building on the stateless capabilities of Stage 2A, Stage 2B enables the detection of multi-step behavioral sequences (e.g., Download → Execute) occurring within a bounded time window. This architecture prioritizes a lightweight, deterministic in-memory state machine using Go and CEL, avoiding the operational bloat of external Complex Event Processing (CEP) frameworks while remaining strictly unprivileged and isolated.

## 2. Current Architecture
ZERAXIS currently features Asset Discovery (Phase 2), Supply Chain context (Phase 3), a Unified Event Backbone over PostgreSQL/NATS (Phase 4), Falco-based eBPF telemetry (Phase 5 Stage 1), and a Stateless Detection Engine evaluating single events using Google CEL (Phase 5 Stage 2A).

## 3. Stage 2B Objective
To answer the question: *"Is this SEQUENCE of events suspicious?"*
Stage 2B will deterministically correlate multiple low-to-medium confidence events into high-confidence security findings, bounding them by time and execution context, without introducing unbounded memory growth or external state stores.

## 4. State Model
The temporal state model is strictly **in-memory**.
- **Primary Execution Identity**: `container_id`. Temporal correlation is bound to a single running instance.
- **Canonical Identity**: `AssetID` and `image_digest` are preserved for context and finding generation.
- **State Data**: For each active sequence, the engine retains the matched `event_id`s, timestamps, and current step index.

## 5. Sliding Window
Each temporal rule defines a strict `time_window` (e.g., `5s`, `30s`, `5m`).
- A sliding window is initialized upon the arrival of an event matching the *first* step of a sequence.
- The window advances with real-time, completely evicting and dropping the state when the duration expires, regardless of partial sequence progress.

## 6. Event Ordering
NATS arrival order is not guaranteed to equal timestamp order.
- **Buffering**: Active sequence states temporarily buffer candidate events.
- **Sorting**: Evaluation relies on the embedded event `timestamp`, tolerating minor out-of-order arrival within the active sliding window.
- **Late Events**: Events arriving *after* a window has expired are processed statelessly or start a new sequence; they cannot retroactively complete an evicted state.

## 7. State Eviction
Strict limits enforce DoS resilience:
- **Maximum Event Age**: Hard eviction on `time_window` expiration.
- **Container Limits**: Maximum concurrent sequences/buffered events per `container_id` (e.g., cap at 500 uncompleted sequences).
- **Global Limits**: Hard memory threshold triggering LRU eviction of oldest partial sequences to prevent OOM.

## 8. Temporal Rule Model
Temporal rules extend the Stage 2A model:
- `rule_id`, `rule_version`, `severity`, `confidence`
- `time_window`: Duration (e.g., `30s`)
- `sequence`: An ordered list of steps (Step 1, Step 2, Step 3).
Each step defines a CEL expression (e.g., `step1.condition`, `step2.condition`).

## 9. State Machine Decision
**Decision**: Custom state machine implemented in Go, leveraging the existing CEL evaluator for step conditions.
**Evaluation**: 
- *CEP Frameworks (Flink/Esper)*: Rejected due to massive dependency footprint and complexity.
- *Go + CEL*: Highly performant, completely deterministic, securely sandboxed, and easily testable within our current microservice boundary.

## 10. Initial Temporal Rules
1. **Download → Execute**: `NETWORK_CONNECTION` (external) followed by `FILE_ACTIVITY` (write /tmp) followed by `PROCESS_EXECUTION` (from /tmp). (Window: 10s).
2. **Shell → External Connection**: `PROCESS_EXECUTION` (shell) followed by `NETWORK_CONNECTION` (external). (Window: 5s).
3. **Sensitive File Write → Process Execution**: `FILE_ACTIVITY` (/etc/) followed by `PROCESS_EXECUTION` (e.g., modified binary). (Window: 60s).
4. **Unexpected Process → External Connection**: `PROCESS_EXECUTION` (unknown/unexpected binary) followed by `NETWORK_CONNECTION`. (Window: 5s).
5. **Multiple Weak Signals**: Sequence of 3 distinct anomalous files written + 1 anomalous process. (Window: 30s).

## 11. Multi-Signal Correlation
Multiple weak signals combine deterministic state transitions.
- **Signal 1 (Shell)**: LOW confidence alone.
- **Signal 2 (External connection)**: LOW confidence alone.
- **Combined State**: Transitions to a SINGLE finding with HIGH confidence. Scoring is deterministic based on rule definition, not arbitrary ML weighting.

## 12. Finding Integration
Stage 2B will **re-use** the existing `findings` and `finding_events` tables.
- A completed sequence creates or updates a single parent finding.
- The `finding_events` table will link ALL events in the sequence (e.g., Event A, B, and C) to the single finding. No new "correlation finding" wrapper is necessary.

## 13. Fingerprinting
The fingerprinting logic remains deterministic to prevent flooding:
`SHA256(rule_id + rule_version + asset_id + image_digest + normalized(group_by_fields))`
100 occurrences of *Download → Execute* on the same asset within a quantized time window produces ONE finding, updated 99 times, with multiple `finding_events` linked.

## 14. Duplicate Handling
- **Duplicate Events**: Handled upstream by the `event_id` LRU cache.
- **Overlapping Sequences**: If A → B → C occurs twice in rapid succession for the same group, the deduplication fingerprint naturally resolves them into a single updated finding.

## 15. Partial Sequence Handling
If a sequence halts at A → B:
- State is held in-memory until `time_window` expires.
- Upon expiration, state is silently evicted.
- **No findings are generated** for partial sequences to prevent false positives.

## 16. Container Lifecycle
- **Identity**: `container_id` is the strict primary key for temporal state.
- **Restart/Recreation**: A restarted or recreated container always receives a new `container_id`. Therefore, correlation state fundamentally resets on destruction, preventing behavior bleeding. Canonical `AssetID` ensures findings remain linked accurately.

## 17. Image Digest Changes
If an image is rebuilt (new `image_digest`), the container must be recreated, natively resulting in a new `container_id`. Temporal state resets automatically. Old behaviors are cleanly decoupled from the new digest.

## 18. Supply Chain Boundary
Stage 2B exposes structured event objects (via CEL variables) where Supply Chain context can be queried. However, Stage 2B will NOT implement vulnerability lookup natively; this is strictly reserved for Stage 2C to avoid duplicating Supply Chain responsibilities.

## 19. Memory vs PostgreSQL
**In-Memory State**: Active sequences, timers, buffered events.
**PostgreSQL**: Finalized findings and finding-event links.
*Crash Behavior*: If the Engine restarts, all partial in-memory correlation state is lost. This is an accepted trade-off to avoid the massive I/O penalty of persisting every intermediate state transition.

## 20. NATS Architecture
Stage 2B consumes `zeraxis.events.runtime.>`.
It does NOT consume Stage 2A findings. Parallel evaluation (Stateless vs Stateful rules) directly on the telemetry stream prevents circular dependencies and complex feedback loops (Detection → NATS → Detection).

## 21. Performance
- **Target**: 500+ events/sec.
- **Worst-Case Memory**: Tracking 1,000 containers * 50 concurrent sequences * 3 events * 1KB = ~150MB overhead.
- **Latency**: Evaluation remains sub-millisecond once a sequence completes.

## 22. DoS Protection
A malicious container spawning 10,000 connections/sec will trigger:
- **Per-Container Event Cap**: Drops new tracking state for that `container_id` once sequence queues saturate.
- **Eviction**: Fails safely. Records a dropped-event metric instead of OOMing.

## 23. Time Handling
- **Future Timestamps**: Discarded if >5 seconds in the future (clock skew).
- **Late Timestamps**: Discarded if they fall entirely outside the active sliding window bounds.

## 24. Observability
New expvar/Prometheus metrics:
- `correlation_states_active`
- `correlation_sequences_completed`
- `correlation_sequences_expired`
- `correlation_events_dropped_dos`

## 25. Security
Stage 2B remains completely unprivileged. CEL rules execute in a strict sandbox. No host capabilities, PID access, or Docker socket mounts are introduced.

## 26. Testing Strategy
Controlled Go unit and integration tests will validate:
- **Valid Sequence (A→B→C)**: Finding generated.
- **Expired Sequence**: Evicted, no finding.
- **Out of Order (A→C→B)**: Finding generated if within window.
- **Isolation**: New `container_id` isolates state cleanly.
- **OOM Protection**: Synthetic burst of 100,000 events verifies bounded memory eviction.

## 27. Database Changes
**No schema changes are required.** The existing `findings` and `finding_events` structure perfectly supports multiple events linked to a single finding ID.

## 28. API
A new read-only API endpoint is proposed (but not strictly required immediately):
`GET /api/v1/findings/{id}/timeline`
This will query `finding_events` and sort the linked event payloads chronologically to visualize the multi-step attack for operators.

## 29. Failure Semantics
- **NATS Failure**: Events missed during downtime are lost.
- **DB Failure**: Completed findings cannot persist; sequences complete but fail to write.
- **Engine Restart**: All partial in-memory state is lost (not recovered).
We explicitly **do not claim** exactly-once delivery or zero-loss semantics.

## 30. Implementation Breakdown
1. **2B.1**: In-memory Correlation State Engine (Windowing, eviction, DoS caps).
2. **2B.2**: Temporal Rule Model (CEL sequence steps).
3. **2B.3**: First Temporal Rules implementation.
4. **2B.4**: Finding Integration (DB linking).
5. **2B.5**: Stress/Failure and Out-of-order testing.

## 31. Risks
- **Clock Skew**: Highly desynchronized container clocks could cause valid sequences to be silently dropped as "late" or "future".
- **Memory Pressure**: Extreme baseline noise (e.g., massive file I/O) could prematurely saturate the per-container queues, dropping valid security signals.

## 32. Known Limitations
- Partial sequence loss upon engine deployment/restart.
- Multi-container correlation (e.g., tracing a lateral movement attack across two distinct `AssetIDs`) is explicitly NOT supported in this bounded model.

## 33. MUST SOLVE BEFORE IMPLEMENTATION
1. Exact definition of the YAML/JSON schema for Temporal Rules (specifically, how step variables/results are referenced in subsequent steps).
2. Tuning the default Per-Container Queue limits to balance detection accuracy against memory safety.


## 34. OUT-OF-ORDER EVENT SEMANTICS

**Algorithm:**
The engine uses a Watermark-based Late Event handling algorithm buffered per `container_id`.

**Definitions:**
- **Watermark (W)**: The highest embedded timestamp seen for a specific `container_id`, minus an `allowed_lateness` tolerance (e.g., 2 seconds).
- **Future Event**: Timestamp > Current System Time + `max_clock_skew` (e.g., 5 seconds). Discarded immediately.
- **Late Event**: Timestamp < Current Watermark. Discarded immediately.

**Pseudocode:**
```text
function receive(event):
    // 1. Clock skew validation
    if event.timestamp > system.time + 5s:
        drop("future_event")
        return

    // 2. Watermark check
    container_state = get_state(event.container_id)
    if event.timestamp < container_state.watermark:
        drop("late_event")
        return

    // 3. Update watermark
    if event.timestamp > container_state.highest_timestamp:
        container_state.highest_timestamp = event.timestamp
    container_state.watermark = container_state.highest_timestamp - 2s

    // 4. Buffer and sort
    container_state.buffer.add(event)
    container_state.buffer.sort_by(timestamp)

    // 5. Ordered Evaluation (only evaluate events strictly behind the watermark)
    while container_state.buffer[0].timestamp <= container_state.watermark:
        ready_event = container_state.buffer.pop(0)
        evaluate_state_machine(ready_event, container_state)

function evaluate_state_machine(event, state):
    // Find all active sequences this event matches
    for seq in state.active_sequences:
        if rule.matches(seq.current_step, event):
            seq.add(event)
            seq.current_step++
            
            if seq.is_complete():
                generate_finding(seq)
                state.active_sequences.remove(seq)

    // Check if event starts a NEW sequence
    for rule in temporal_rules:
        if rule.matches(rule.step[0], event):
            new_seq = start_sequence(rule, event)
            state.active_sequences.add(new_seq)

    // 6. Eviction of expired states
    for seq in state.active_sequences:
        if event.timestamp - seq.start_event.timestamp > rule.time_window:
            state.active_sequences.remove(seq)
```
*Note*: Once a sequence is completed and a finding generated, it is immutable. A late event cannot retroactively modify an already-completed sequence.

## 35. TEMPORAL RULE SCHEMA

The YAML schema remains declarative. CEL variables implicitly expose previously matched events in the sequence to allow cross-step assertions (e.g., ensuring the binary downloaded in Step 1 is the exact same binary executed in Step 2).

**Features:**
- Cross-step references via the `sequence` array (e.g., `sequence[0].metadata.path`).
- Implicit AND condition between steps.
- No arbitrary loops or branching; linear deterministic sequences only.

**Example 1: Download & Execute**
```yaml
rules:
  - rule_id: download_execute
    rule_version: 1
    name: Download and Execute
    severity: HIGH
    confidence: HIGH
    enabled: true
    time_window: 10s
    correlation:
      fields:
        - asset_id
        - container_id

    sequence:
      - name: network
        event_type: NETWORK_CONNECTION
        condition: 'event.metadata.direction == "egress" && !event.metadata.destination_ip.startsWith("10.")'

      - name: download
        event_type: FILE_ACTIVITY
        condition: 'event.metadata.operation == "WRITE" && event.metadata.path.startsWith("/tmp/")'

      - name: execute
        event_type: PROCESS_EXECUTION
        # Cross-step reference: Executable must match the exact file written in the previous step
        condition: 'event.metadata.executable == sequence[1].metadata.path'
```

**Example 2: Shell Spawns External Connection**
```yaml
rules:
  - rule_id: shell_external_net
    rule_version: 1
    name: Shell External Network Connection
    severity: CRITICAL
    confidence: HIGH
    enabled: true
    time_window: 5s
    correlation:
      fields:
        - asset_id

    sequence:
      - name: shell
        event_type: PROCESS_EXECUTION
        condition: 'event.metadata.process in ["sh", "bash", "ash"]'

      - name: network
        event_type: NETWORK_CONNECTION
        # Ensure the network connection originated from the shell process
        condition: 'event.metadata.process == sequence[0].metadata.process && event.metadata.direction == "egress"'
```

## 36. MEMORY SAFETY BENCHMARK

To replace theoretical estimates with concrete measurements, a dedicated Go benchmark suite (`BenchmarkTemporalEngine_MemorySafety`) will be written before merging Stage 2B.

**Benchmark Design:**
- **Load Generation**: Synthetic injection of `PROCESS_EXECUTION`, `FILE_ACTIVITY`, and `NETWORK_CONNECTION` events directly into the Go engine (bypassing NATS/Postgres to isolate engine memory).
- **Scale Vectors**: 100, 500, and 1,000 distinct `container_id` streams concurrently.
- **Noise Vectors**: 90% background noise (events that match Step 1 but never complete Step 2) to force sequence tracking and eventual eviction.

**Hard Global Memory Safety Mechanism:**
Because Go does not natively support strict byte-limit enforcement on heap allocations (without aggressive manual `runtime.GC()` or `SetMemoryLimit` which impacts latency), the engine will implement a **Conservative State-Count Limit**:
- `MAX_ACTIVE_CONTAINERS = 5000`
- `MAX_SEQUENCES_PER_CONTAINER = 100`
*(Max active partial sequences globally = 500,000)*.

At roughly 500 bytes per partial sequence state (pointers + small structs), this guarantees the correlation heap cannot exceed ~250MB regardless of event burst size.
If a container exceeds `100` active partial sequences, new sequences for that container are dropped, and a `correlation_dropped_dos` metric is incremented.

**Metrics Measured During Benchmark:**
- `runtime.ReadMemStats(&m)` (HeapAlloc, HeapInuse)
- CPU ns/op
- Eviction triggers per second.

==================================================
FINAL DECISION

**READY FOR STAGE 2B IMPLEMENTATION**
