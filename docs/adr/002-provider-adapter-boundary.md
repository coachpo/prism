# ADR 002: Provider Adapter Boundary

Status: proposed

## Context

Provider behavior currently lives in request, response, stream, and media hook maps. The audit found no formal `ProviderAdapter` boundary.

## Decision

Introduce provider adapters for OpenAI, Anthropic, and Gemini. Adapters own provider-native parsing, upstream request building, response adaptation, streaming, usage extraction, token estimation/counting, media handling, overflow classification, and conversion capabilities.

## Consequences

Adding provider behavior no longer requires edits across unrelated global hook maps. Operation hooks can remain as adapter internals, not the primary extension boundary.

## Rejected alternatives

- Keep hook maps as the provider boundary.
- Add provider-specific branches inside the shared executor.
- Use one generic passthrough adapter for all providers.

## Implementation notes

Start with compile-time conformance tests and migrate one operation family at a time.

## Tests required

Adapter contract tests for parse, build, stream classification, non-stream usage extraction, overflow classification, media rewrite, and conversion behavior.
