# ADR 011: Typed Hook System

Status: proposed

## Context

The current hook maps are useful but are not the target extension boundary. Hooks should be typed phase extensions that cannot corrupt stream or accounting invariants.

## Decision

Use typed hook phases around gateway ingress, request parsing, route planning, before attempt, after attempt, response adaptation, usage extraction, pricing, audit preparation, and final recording.

## Consequences

Extensions become explicit, ordered, and testable. Provider-specific hooks move behind adapters unless they are genuinely cross-provider gateway phases.

## Rejected alternatives

- Untyped callback maps.
- Provider-specific branches in the pipeline.
- Hooks that can mutate committed stream state.

## Implementation notes

Each hook phase has a typed input/output envelope. Hooks may enrich metadata or reject pre-commit work, but cannot alter post-commit stream semantics.

## Tests required

Hook ordering tests, type-boundary tests, rejection tests, metadata enrichment tests, and post-commit mutation prevention tests.
