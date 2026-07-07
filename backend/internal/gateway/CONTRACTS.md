# Gateway Preserved Contracts

This note freezes the external and runtime contracts captured in T01 before gateway-core work starts. Future gateway packages must preserve these facts unless the implementation plan explicitly assigns a task to change them.

## Evidence

- Baseline suite receipt: `artifacts/evidence/task-1-baseline.txt`
- Focused route receipt: `artifacts/evidence/task-1-responses-current.txt`
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
- Requested model fields and resolved/effective target fields must not be rewritten to hide redirects or facades.

## Hook And Usage Contracts

Provider-native differences are currently selected by operation hook collection IDs. Future provider adapters may internalize these hooks, but behavior must remain operation-specific:

- Text-generation operations use text-generation request, response, stream, and usage parsing rules.
- Token-count operations stay out of generation-only parsing and usage assumptions.
- OpenAI image generation and image editing stay on media hooks, including multipart model binding for image edits.
- Gemini model IDs are bound from the route path, not from a request-body model field.
- OpenAI and Anthropic model IDs are bound from the request body.

Current usage behavior is provider-carrier based: OpenAI Chat Completions reads root `usage`, OpenAI Responses reads root or nested `response.usage`, Anthropic Messages reads root or message `usage`, Gemini reads `usageMetadata`, and token-count endpoints preserve token-count semantics instead of generation semantics.

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
