# Gateway Preserved Contracts

This note freezes the external and runtime contracts captured in T01 before gateway-core work starts. Future gateway packages must preserve these facts unless the implementation plan explicitly assigns a task to change them.

## Evidence

- Baseline suite receipt: `.omo/evidence/task-1-baseline.txt`
- Focused route receipt: `.omo/evidence/task-1-responses-current.txt`
- Runtime operation source of truth today: `backend/internal/httpapi/runtime/operations.go`
- Rejected-route isolation test: `backend/tests/runtime/rejected_route_isolation_test.go`
- Route-matrix characterization: `backend/tests/runtime/operation_route_matrix_test.go`

## Current Runtime Allowlist

Runtime traffic is operation-registered. The mounted `/v1` and `/v1beta` branches are not passthrough prefixes.

| Method | Public path | Operation name | API family | Streaming | Model binding |
| --- | --- | --- | --- | --- | --- |
| POST | `/v1/chat/completions` | `openai.chat_completions` | `openai` | no | request body |
| POST | `/v1/responses` | `openai.responses` | `openai` | no | request body |
| POST | `/v1/responses/input_tokens` | `openai.responses.input_tokens` | `openai` | no | request body |
| POST | `/v1/responses/compact` | `openai.responses.compact` | `openai` | no | request body |
| POST | `/v1/images/generations` | `openai.images.generations` | `openai` | no | request body |
| POST | `/v1/images/edits` | `openai.images.edits` | `openai` | no | request body |
| POST | `/v1/messages` | `anthropic.messages` | `anthropic` | no | request body |
| POST | `/v1/messages/count_tokens` | `anthropic.count_tokens` | `anthropic` | no | request body |
| POST | `/v1beta/models/{model}:generateContent` | `gemini.generate_content` | `gemini` | no | path `{model}` |
| POST | `/v1beta/models/{model}:streamGenerateContent` | `gemini.stream_generate_content` | `gemini` | yes | path `{model}` |
| POST | `/v1beta/models/{model}:countTokens` | `gemini.count_tokens` | `gemini` | no | path `{model}` |

## Exact Responses Adjunct Routes

T08 added these routes as explicit registered operations:

- `POST /v1/responses/input_tokens` as `openai.responses.input_tokens`
- `POST /v1/responses/compact` as `openai.responses.compact`

Do not treat either route as a broad passthrough alias. They remain exact operation-registry entries with route-matrix, wrong-method, rejection-isolation, usage/accounting, and provider-adapter coverage.

## Rejection Contract

Unsupported routes and wrong methods reject at ingress before request body reads, runtime planning, provider transport, runtime telemetry outbox writes, request logs, audit logs, usage events, feedback, or durable runtime side effects.
Current rejection responses are:

- Unsupported operation path: HTTP 404 JSON response with `detail: "Runtime operation not found"`.
- Wrong method for a known operation path: HTTP 405 JSON response with `detail: "Method not allowed for runtime operation"` and `Allow: POST`.

The rejected-route isolation suite currently proves unsupported OpenAI, Anthropic, and Gemini routes plus wrong-method variants do not reach provider transport, do not create runtime admission state, do not submit runtime side effects, and do not persist request-log, audit-log, usage-event, or runtime-telemetry-outbox rows.

## Runtime Attribution To Preserve

Supported operation execution must keep carrying operation metadata through planning, forwarding, response handling, traces, and persistence:

- `operation_name` persists to request logs and usage request events.
- Upstream attribution includes upstream operation name, operation translation mode, and upstream request path.
- Request-log detail preserves additive `context_routing` metadata, including facade selection and context-overflow-promotion details.
- Requested model fields and resolved/effective target fields must not be rewritten to hide redirects, facades, or overflow promotion.

## Hook And Usage Contracts

Provider-native differences are currently selected by operation hook collection IDs. Future provider adapters may internalize these hooks, but behavior must remain operation-specific:

- Text-generation operations use text-generation request, response, stream, and usage parsing rules.
- Token-count operations stay out of generation-only parsing and usage assumptions.
- OpenAI image generation and image editing stay on media hooks, including multipart model binding for image edits.
- Gemini model IDs are bound from the route path, not from a request-body model field.
- OpenAI and Anthropic model IDs are bound from the request body.

Current usage behavior is provider-carrier based: OpenAI Chat Completions reads root `usage`, OpenAI Responses reads root or nested `response.usage`, Anthropic Messages reads root or message `usage`, Gemini reads `usageMetadata`, and token-count endpoints preserve token-count semantics instead of generation semantics.

## Context Overflow Promotion Contract

Context overflow promotion is explicit and additive today:

- It is allowed only for OpenAI Chat Completions and OpenAI Responses requests.
- It requires a configured `context_overflow_promotion_target_id` on the source model.
- Chat Completions streaming promotion applies only to `POST /v1/chat/completions` with `stream=true` when tokenizer-backed estimation is available, the source target is over its usable context window, and the explicit promoted target fits the same request. The gateway builds but does not execute the source plan, opens zero source upstream requests, executes one promoted attempt, and records `trigger_phase=pre_dispatch_estimate`.
- Responses streaming promotion applies only to `POST /v1/responses` with `stream=true` before client-visible source bytes. The gateway can replay a pre-stream JSON provider-overflow body before downstream commit. It can also stage a bounded SSE prelude made only of `response.created` and `response.in_progress`, limited to `16 KiB`, `2` events, and `250 ms`, then replay only when the next staged event is a code-classified overflow `error`. Non-overflow errors, unknown prelude events, semantic output such as `response.output_text.delta`, cap expiry, timeout, or any client-visible source byte disable replay and continue the source stream unchanged. The gateway replays the original buffered ingress body exactly once to the explicit promoted target, preserving `previous_response_id`, `store`, and other continuation fields.
- Existing non-stream Chat Completions and Responses provider-overflow replay remains available with additive `trigger_phase=provider_overflow` metadata.
- It records additive request-log and trace metadata without rewriting the requested model identity. Metadata includes `trigger_phase`, estimate fields when present, source and final attempt counts, and from/to resolved target and selected terminal target identities.
- Promotion is not a generic retry, redirect, or fallback policy. It does not multi-hop, does not reopen facade siblings, and a promoted-target failure is final.
- The streaming safety contract below remains the hard boundary: after the first downstream byte or event, the gateway must not promote or switch upstreams.

`trigger_phase` values are part of the context-overflow promotion metadata contract:

- `pre_dispatch_estimate`
- `provider_overflow`

## Frozen Future Vocabularies

Future gateway accounting must use these exact vocabularies from the implementation plan.

`route_reason` values:

- `direct_match`
- `model_redirect`
- `upstream_redirect`
- `qps_overflow`
- `rpm_overflow`
- `tpm_overflow`
- `ipm_overflow`
- `concurrency_overflow`
- `retry_429`
- `retry_5xx`
- `retry_connect_timeout`
- `context_overflow_preflight`
- `context_overflow_provider_fallback`
- `circuit_open_skip`
- `no_healthy_upstream`
- `policy_reject`

`usage_source` values:

- `provider`
- `provider_stream_terminal`
- `local_estimate`
- `missing`

These names are target-state vocabulary, not current persisted column names. T01 search found no current backend fields named `route_reason` or `usage_source`; later accounting work must introduce explicit fields or an equivalent typed persisted contract without late inference.

## Streaming Safety Contract

Before the first downstream byte or event, gateway code may reject, fail over, translate, or choose an eligible backup target according to registered operation behavior. After the first downstream byte or event, it must not retry, hedge, redirect, promote, or switch upstreams; it may only classify stream outcome, merge usage, record audit/accounting, and surface downstream errors.
