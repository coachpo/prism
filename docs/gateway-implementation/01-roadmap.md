# Gateway Implementation Roadmap

## Roadmap Rules

This roadmap is a clean-cut replacement plan for the gateway core. Existing behavior should be pinned with characterization tests, then migrated operation family by operation family. Do not add compatibility shims for internal APIs that have not shipped to users.

Each phase exits only when targeted tests pass, observability remains intact, and obsolete code in that phase is either deleted or explicitly scheduled for deletion.

## Phase 1: Gateway core skeleton

Goal: introduce the new package boundaries and typed phase contracts without changing runtime behavior.

Deliverables:

- `gateway/core` phase runner and envelopes.
- `gateway/registry` operation, capability, route, model, upstream, and pricing registry contracts.
- `gateway/provider.ProviderAdapter` interface.
- `gateway/accounting.AccountingEvent` contract.
- Characterization tests proving existing supported operations still behave as before.

Exit criteria:

- Existing route matrix, rejected-route isolation, hook residency, OpenAI translation, context-overflow, pricing, and observability tests are green.
- No runtime operation has moved yet unless a compatibility bridge keeps current behavior identical.

## Phase 2: Routing, QPS overflow, and load balancing

Goal: move routing and admission into explicit planner and reservation phases.

Deliverables:

- `ModelProfile`, `UpstreamEndpoint`, `RouteRule`, `RoutePolicy`, and `RoutePlan` records.
- Candidate ordering for single, fill-first, round-robin, and failover behavior.
- QPS overflow to the next eligible upstream.
- RPM, TPM, IPM, and concurrency reservation semantics if product policy requires them.
- Route reason emission for normal selection, QPS overflow, admission rejection, model redirect, and upstream redirect.

Exit criteria:

- QPS overflow tests prove candidate A exhaustion selects candidate B.
- Admission tests cover request, token, image, and concurrency reservations when enabled.
- Old route planning code is ready to delete for migrated operations.

## Phase 3: OpenAI-family LLM endpoints and conversion

Goal: migrate OpenAI text operations to the adapter boundary.

Deliverables:

- `POST /v1/chat/completions` adapter support.
- `POST /v1/responses` adapter support.
- `/v1/responses/input_tokens` or equivalent token-counting operation.
- `/v1/responses/compact` or equivalent compaction operation.
- Chat Completions <-> Responses conversion as an OpenAI adapter capability.
- Public response-shape preservation policy for converted and overflow-backed responses.

Exit criteria:

- Route matrix covers all OpenAI-family LLM endpoints.
- Conversion tests cover streaming and non-streaming accepted and rejected shapes.
- Missing Responses adjunct operations are no longer a P0 gap.

## Phase 4: OpenAI image generation and image editing

Goal: migrate OpenAI image operations to the adapter boundary without reusing text-generation hooks.

Deliverables:

- `POST /v1/images/generations` adapter support.
- `POST /v1/images/edits` adapter support for JSON and multipart model binding.
- Media request body limits and audit-body boundaries.
- No token-usage assumptions for image responses unless the provider returns supported usage.

Exit criteria:

- Image route-matrix cases pass through the adapter path.
- Multipart rewrite tests prove only the intended `model` field changes.
- Old media hook maps are deleted or internalized by the OpenAI adapter.

## Phase 5: Anthropic and Gemini adapters

Goal: migrate non-OpenAI generation and token-counting operations to provider adapters.

Deliverables:

- Anthropic Messages adapter support.
- Anthropic count-tokens adapter support.
- Gemini generateContent adapter support.
- Gemini streamGenerateContent adapter support.
- Gemini countTokens adapter support.
- Provider-specific stream terminal and usage parsing behind adapters.

Exit criteria:

- Anthropic and Gemini route-matrix cases pass through adapters.
- Provider adapter contract tests cover request parsing, native path building, usage extraction, and stream terminal classification.

## Phase 6: Logging, audit, usage, and pricing

Goal: make accounting a mandatory typed output of the gateway pipeline.

Deliverables:

- Attempt and final accounting events.
- Explicit fields for requested model, effective model, selected upstream, route reason, usage source, operation name, upstream operation name, and pricing catalog version.
- Request logging separated from immutable audit logging.
- PriceCatalog version capture and unpriced reason handling.
- Audit body capture policy preserved across streaming, non-streaming, media, and translated responses.

Exit criteria:

- Request-log, audit-log, usage-event, and pricing tests assert all target attribution fields.
- Missing usage and incomplete-stream pricing outcomes are deterministic and tested.

## Phase 7: Typed hooks and extensibility

Goal: replace global hook maps as the public extension seam with typed phase hooks.

Deliverables:

- Hook phases for ingress, request parsed, route planned, before attempt, after attempt, response adapted, usage extracted, priced, audit prepared, and final recorded.
- Typed hook inputs and outputs.
- Registration rules that prevent hooks from changing committed stream semantics.
- Hook tests proving ordering, mutation boundaries, and error handling.

Exit criteria:

- Existing operation hooks are internal adapter implementation details or replaced.
- Adding a new operation requires registry plus adapter declarations, not edits to unrelated global maps.

## Phase 8: Cleanup and test hardening

Goal: delete replaced runtime orchestration and harden the migration test suite.

Deliverables:

- Delete obsolete `service.go` / `runtime.go` orchestration paths after equivalent operations are migrated.
- Keep hedging only as active, configured, and tested runtime behavior.
- Remove old hook registries that no longer define extension behavior.
- Update docs to make the new gateway core the source of truth.

Exit criteria:

- Full backend runtime, contract, integration, and priority suites pass.
- No unsupported route bypasses the operation registry.
- No streaming path can retry, hedge, redirect, or overflow-promote after commit.
