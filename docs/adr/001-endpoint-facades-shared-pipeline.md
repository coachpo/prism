# ADR 001: Endpoint Facades Over Shared Pipeline

Status: proposed

## Context

The audit found that HTTP routing is already thin at the branch level, but runtime orchestration is concentrated in handler internals. The target architecture needs explicit operation facades without per-endpoint business logic.

## Decision

Use thin endpoint facades that resolve method/path, enforce request limits, build a request envelope, and hand all supported operations to one shared gateway pipeline.

## Consequences

Unsupported operations continue to reject before body reads, provider transport, telemetry, audit, usage, or side effects. Endpoint-specific behavior moves to operation metadata and provider adapters.

## Rejected alternatives

- Broad `/v1` passthrough.
- Per-endpoint handler forks.
- Keeping orchestration inside facade handlers.

## Implementation notes

Keep the operation registry as the only source of supported routes. The facade may own HTTP details only.

## Tests required

Route matrix tests, wrong-method tests, unsupported-route isolation tests, and facade tests proving no side effects occur before operation resolution.
