# ADR 007: OpenAI Chat Completions to Responses Conversion Strategy

Status: proposed

## Context

OpenAI Chat Completions and Responses conversion exists today as a bridge. The audit found it should be preserved as behavior but moved behind an OpenAI adapter boundary.

## Decision

Make Chat Completions <-> Responses conversion an OpenAI adapter capability. Conversion is allowed only for safe request, response, and stream shapes; unsupported shapes fail explicitly.

## Consequences

The public operation shape can be preserved while upstream native support differs. Conversion rules become provider-specific and testable.

## Rejected alternatives

- Best-effort lossy conversion.
- Gateway-core branching for OpenAI conversion.
- Treating conversion as a fallback after upstream failure.

## Implementation notes

The adapter must expose conversion mode, upstream operation name, usage source, and response-shape preservation metadata to accounting.

## Tests required

Golden request/response conversion tests, stream conversion tests, unsupported-shape rejection tests, and observability attribution tests.
