# ADR 006: Context-Window Overflow Fallback

Status: proposed

## Context

Current context overflow promotion is narrow: non-stream OpenAI chat/responses with explicit promotion targets. The target requires token estimate/count, context comparison, backup model selection, and backup load balancing.

## Decision

Make context-window overflow a provider-capability policy. The pipeline estimates or counts tokens before execution, compares against configured context windows, selects a backup model when needed, and sends that backup model through normal route planning.

## Consequences

Overflow fallback is planned before request execution whenever possible. Post-response overflow replay remains non-stream only and must be explicitly configured.

## Rejected alternatives

- Generic retry on provider 400/413/422/429.
- Implicit larger-model fallback.
- Streaming overflow replay after response commit.

## Implementation notes

Adapters own token estimation/counting and overflow classification. Public response-shape preservation is a ModelProfile policy.

## Tests required

Token estimate/count tests, context-window comparison tests, backup re-entry tests, false-positive overflow classifier tests, and no-overflow-after-stream-commit tests.
