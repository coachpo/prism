# ADR 009: Usage and Pricing Data Model

Status: proposed

## Context

The current schema stores operation names, token fields, pricing snapshots, and pricing config version, but the target requires explicit usage source and pricing catalog version preservation.

## Decision

Use typed usage and pricing events emitted by the gateway pipeline. Every final accounting event records requested model, effective model, selected upstream, operation, upstream operation, usage source, normalized usage, pricing catalog version, pricing snapshot, and unpriced reason when applicable.

## Consequences

Pricing becomes reproducible from recorded data. Missing usage and incomplete streams are first-class accounting outcomes.

## Rejected alternatives

- Recompute pricing later from mutable catalog data.
- Infer usage source from provider family only.
- Store costs without catalog version.

## Implementation notes

PriceCatalog is selected from the effective route attempt and copied into immutable accounting fields at response time.

## Tests required

Pricing snapshot tests, usage-source tests, missing-usage tests, incomplete-stream unpriced tests, and currency/FX conversion tests.
