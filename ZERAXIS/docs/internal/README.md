# Internal Engineering Documentation

This directory contains historical architecture proposals, implementation gate documents, phase-by-phase audit notes, and development history for the ZERAXIS project.

These documents were written during active development (Phases 2–9) and represent the evolving design thinking, implementation decisions, and verification criteria used at each phase.

## Contents

### Architecture Documents
- `phase-2-discovery.md` — Discovery Engine architecture
- `phase-3-supply-chain.md` — Supply-Chain Engine architecture
- `phase-5-runtime.md` — Runtime Engine architecture
- `phase-5-stage2-*.md` — Stage 2 detection architecture variants
- `phase-6-*.md` — Correlation, risk model, incident model, attack chain model
- `phase-7.1-*.md` — Execution boundary, authorization model, identity validation

### Implementation Gates
- `phase-6-implementation-gate.md` — Phase 6 completion criteria
- `phase-5-stage2-implementation-gate.md` — Stage 2 detection gate
- `phase-7.2-*.md` — Executor implementation notes

### ADRs (Architecture Decision Records)
- `adr/` and `adrs/` directories — individual architectural decision records

## Status

These documents are **historical engineering artifacts**. They may not reflect the final implementation state precisely, as the design evolved during development. The authoritative description of the current implementation is:

- [`../README.md`](../../README.md) — architecture, setup, reference
- [`../THREAT_MODEL.md`](../THREAT_MODEL.md) — trust boundaries and mitigations
- [`../FAILURE_SEMANTICS.md`](../FAILURE_SEMANTICS.md) — failure behaviour
- [`../LIMITATIONS.md`](../LIMITATIONS.md) — known limitations

These internal documents are preserved as engineering history and for contributors who want to understand the rationale behind design decisions.
