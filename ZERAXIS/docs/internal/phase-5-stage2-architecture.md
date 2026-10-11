# ZERAXIS Phase 5 Stage 2 Final Audit & Architecture Report

## 1. Executive Summary
Phase 5 Stage 2 introduces the **Detection & Correlation Engine** to the ZERAXIS platform. Moving beyond raw telemetry collection (Stage 1), Stage 2 implements a deterministic rule engine that evaluates runtime behavior, infrastructure state, and software supply-chain context to generate high-fidelity security findings. This design strictly avoids AI/ML, automated response, and over-engineered infrastructure, relying entirely on the proven Go + PostgreSQL + NATS stack.

## 2. Current Architecture
The ZERAXIS laboratory currently consists of:
- **Phase 1-2 (Infrastructure & Discovery)**: Microservices (Gateway, Auth, Product, Payment) and continuous asset discovery tracking Containers, Ports, and Networks.
- **Phase 3 (Supply Chain)**: SBOM generation, vulnerability scanning, and policy evaluation bound to immutable image digests.
- **Phase 4 (Unified Backbone)**: PostgreSQL as the durable event history and NATS for internal asynchronous transport, utilizing a canonical event envelope.
- **Phase 5 Stage 1 (Runtime Monitoring)**: Unprivileged Falco eBPF sensor pipeline capturing `PROCESS_EXECUTION`, `NETWORK_CONNECTION`, and `FILE_ACTIVITY`, enriched with Discovery `AssetID` and Supply Chain `image_digest`.

## 3. Stage 2 Objective
Design a deterministic Detection Engine capable of answering: *"What combination of infrastructure state, software supply-chain state, and runtime behavior represents a meaningful security finding?"* The system must reason about context, eschew assumptions of automatic malice, and output structured, deduplicated Security Findings with distinct Risk/Confidence scoring, preparing the platform for a future Incident Management layer.

## 4. Detection Architecture
The Detection Engine will be a new, unprivileged Go microservice (`detection-engine`). 
- **Input**: Subscribes to NATS topics (`zeraxis.events.runtime.>`, `zeraxis.events.supplychain.>`, `zeraxis.events.discovery.>`).
- **Processing**: Routes events into an in-memory evaluation pipeline consisting of Stateless Rule Evaluation, Stateful/Temporal Correlation, and Context Enrichment.
- **Output**: Persists findings to PostgreSQL and emits finding events to NATS.

## 5. Rule Model
Rules are deterministic and defined in a structured format (JSON/YAML). No AI/ML is used.
**Fields:**
- `rule_id`: Unique identifier (e.g., `rule-exec-tmp`)
- `rule_version`: Integer tracking rule changes
- `name` & `description`: Human-readable context
- `severity`: Potential impact (CRITICAL, HIGH, MEDIUM, LOW)
- `confidence`: Strength of evidence (HIGH, MEDIUM, LOW)
- `required_event_types`: Array of event types this rule evaluates
- `time_window`: Duration for temporal rules (e.g., `5s`)
- `conditions`: AST or expression string evaluating event payloads (e.g., `event.metadata.path startswith "/tmp/"`)
- `enabled`: Boolean toggle

## 6. Correlation Model
The engine maintains in-memory correlation state bounded by time windows.
- **Primary Key**: `AssetID` (Ensures correlation is bound to a specific running container instance).
- **Secondary Key**: `image_digest` (For aggregating findings across replicas).
- **Eviction**: State is strictly evicted when the rule's `time_window` expires, preventing memory leaks.
- **Persistence**: Intermediate correlation states are *not* persisted to the database. Only finalized findings are durable.

## 7. Supply Chain Correlation
Combines runtime behavior with static analysis. 
*Example*: If `Asset A` executes `package X`, the Detection Engine queries its contextual cache (populated via NATS supply chain events) to check if `package X` has a `CRITICAL` vulnerability.
**Defensibility**: We do *not* claim exploitation merely because a vulnerable package exists. We detect "behavior consistent with exploitation" (e.g., a vulnerable web server spawning a shell).

## 8. Temporal Correlation
Correlates sequences of events within a sliding time window.
*Example*: `PROCESS_EXECUTION` (wget) followed by `FILE_ACTIVITY` (/tmp/payload) within 2 seconds.
- **Handling**: State is kept in memory. Late or out-of-order events arriving outside the time window are evaluated independently (or dropped from the sequence) to maintain deterministic bounding. 

## 9. Finding Model
A Finding represents a correlated security violation. Findings *reference* events rather than duplicating them.
**Fields:**
- `finding_id`: UUID
- `rule_id` / `rule_version`
- `severity` / `confidence`
- `status`: OPEN, UPDATED, RESOLVED
- `first_seen` / `last_seen`: Timestamps
- `asset_id` / `image_digest`
- `event_ids`: Array of UUIDs linking to the unified events table
- `evidence`: Summarized string or JSON of the exact trigger

## 10. Severity/Confidence
Strictly decoupled:
- **Severity**: "How damaging would this behavior be?" (e.g., Root shell = CRITICAL).
- **Confidence**: "How strongly does the evidence indicate suspicious behavior?" (e.g., An unknown binary in `/tmp` = HIGH confidence. A standard binary doing standard network calls = LOW confidence).

## 11. False Positive Strategy
To handle expected legitimate behavior (e.g., `payment-service` pinging Postgres), the engine uses:
- **Baseline Allowlisting**: Rules can define exclusions based on `image_digest` or `asset_name`.
- **Targeted Rules**: Rules are written specifically for expected anomalies rather than generic blocks.
- **Suppression**: A mechanism to mute findings for specific `rule_id` + `image_digest` combinations if they are deemed acceptable operational noise.

## 12. Event Volume
Given Stage 1 bursts of ~150 events/sec, the Detection Engine must handle ~500-1000 events/sec peak.
- **Memory**: Bounded by the time window of stateful rules and max queue depth.
- **DB Writes**: Optimized. We do not write every evaluation step. We only write when a finding is created or updated.

## 13. NATS Integration
The Engine subscribes to:
- `zeraxis.events.runtime.>` (For behavioral triggers)
- `zeraxis.events.supplychain.>` (For updating internal vulnerability context caches)
- `zeraxis.events.discovery.>` (For tracking container lifecycles to clear correlation caches)
No external message brokers (Kafka/RabbitMQ) will be introduced.

## 14. Ordering / Duplication
NATS delivery is asynchronous and not strictly globally ordered.
- **Duplication**: The engine uses an LRU cache of recently processed `event_id`s to silently drop duplicates.
- **Ordering**: Temporal rules sort events by the embedded `timestamp` in the event envelope, not arrival time, buffered briefly within the time window.

## 15. Identity Model
- **Canonical Identity**: `AssetID` + immutable `image_digest`.
- **Handling Changes**: If a container restarts, it receives a new `AssetID`. The detection engine treats this as a distinct temporal timeline, preventing false correlations across container restarts.

## 16. Database Changes Required
PostgreSQL schema extensions (No Neo4j/graph DB needed):
- Table: `rules` (Stores rule definitions if we make them dynamic, otherwise loaded from files).
- Table: `findings` (Stores the finding metadata).
- Table: `finding_events` (Many-to-many relationship linking a `finding_id` to `event_id`s).

## 17. API Design
Read-only APIs exposed via the new microservice (and proxied via Gateway):
- `GET /api/v1/findings` (List with filters)
- `GET /api/v1/findings/{id}` (Details including event links)
- `GET /api/v1/assets/{id}/findings` (Asset-specific view)
- `GET /api/v1/rules` (List active rules)
*No arbitrary event injection endpoints.*

## 18. Security Boundary
The Detection Engine operates as a pure **observer**.
- NO Docker socket mounted.
- NO host filesystem access.
- NO privileged mode.
- NO execution/remediation capabilities (it cannot kill processes or modify firewalls).

## 19. Attack Scenarios
- **Scenario A (Shell in Container)**: `PROCESS_EXECUTION` (sh/bash) below a non-shell parent. **Severity**: HIGH. **Confidence**: HIGH.
- **Scenario B (Execution from Writable Dir)**: `PROCESS_EXECUTION` where path starts with `/tmp/`. **Severity**: MEDIUM. **Confidence**: HIGH.
- **Scenario C (Unexpected External Connection)**: `NETWORK_CONNECTION` to public IP from a container with an SBOM that indicates no expected external communication tool. **Severity**: MEDIUM. **Confidence**: LOW.

## 20. Performance Targets
- **Throughput**: Process 500 events/sec per instance.
- **Memory**: < 200MB (using bounded correlation state).
- **Latency**: Event ingestion to finding creation < 100ms.
- **Degradation**: If NATS overwhelms the consumer, drop events locally and emit a metric, rather than crashing with OOM.

## 21. Observability
Prometheus/Expvar metrics to expose:
- `events_evaluated_total`
- `rules_matched_total`
- `findings_created_total`
- `correlation_cache_size`
- `events_dropped_total`

## 22. Stage 2A/2B/2C Breakdown
- **Stage 2A**: Detection Rule Engine (Stateless). Evaluate single events (e.g., `execve` of `/bin/sh`).
- **Stage 2B**: Temporal Correlation. Implement the sliding window and multi-event rules.
- **Stage 2C**: Contextual Correlation. Integrate Supply Chain and Discovery data for complex contextual rules.

## 23. Risks
- **Memory Exhaustion**: Stateful temporal correlation could consume massive memory during a denial-of-service event storm.
- **False Positive Fatigue**: If baseline noise (e.g., Postgres file writes) isn't correctly filtered, findings will become useless.

## 24. Known Limitations
- The SBOM does not always perfectly map to runtime process paths, limiting the efficacy of strict executable allowlisting.
- Without a graph database, deep multi-hop traversal queries (e.g., tracking an attack across 4 microservices) will be computationally expensive in PostgreSQL.

## 25. MUST SOLVE BEFORE IMPLEMENTATION
1. Define the exact deterministic expression language for rule evaluation (e.g., Google CEL or simple Go struct matchers).
2. Establish the exact deduplication key structure for Findings to prevent database bloat during repetitive behavior.

## 26. Recommended Implementation Order
1. Database schema migrations (`findings`, `finding_events`).
2. Stage 2A: Detection Engine scaffolding, NATS consumption, and Stateless Rule Evaluation.
3. Stage 2B: Stateful Correlation Window manager.
4. Stage 2C: Contextual NATS subscriptions (Supply Chain).
5. Read-only API implementation.

==================================================
FINAL DECISION

APPROVED FOR IMPLEMENTATION
