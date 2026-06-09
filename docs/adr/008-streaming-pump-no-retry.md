# ADR 008: Streaming Pump and No-Retry-After-First-Byte Rule

Status: proposed

## Context

Streaming behavior currently depends on response handling branches. The target architecture requires a hard commit boundary for retry, overflow, and upstream switching.

## Decision

The streaming pump has two phases. Before the first downstream byte or event, the gateway may reject, fail over, or choose another planned attempt. After the first downstream byte or event, the gateway must not retry, hedge, redirect, overflow-promote, or switch upstreams.

## Consequences

Clients never see mixed upstream streams. Stream outcomes remain observable even when usage is incomplete.

## Rejected alternatives

- Retry on stream errors after partial output.
- Context overflow promotion after stream commit.
- Hidden buffering for all streams unless explicitly configured.

## Implementation notes

Translated OpenAI streams must be explicitly classified as buffered or incremental. The chosen mode must be visible in tests and docs.

## Tests required

Pre-commit failover tests, post-commit no-retry tests, client-disconnect tests, upstream-read-error tests, and translated-stream mode tests.
