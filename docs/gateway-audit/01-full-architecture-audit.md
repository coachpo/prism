# Full Gateway Architecture Audit

> Historical audit note: this document records an earlier architecture audit. Claims about a smaller runtime catalog and unimplemented Responses adjunct routes are superseded by the current launch state. The live runtime allowlist is now 11 `POST` operations, including `POST /v1/responses/input_tokens` and `POST /v1/responses/compact`.

## Executive Summary

Prism is a focused Go runtime gateway plus React management dashboard. The runtime surface is explicitly operation-registered, not a broad `/v1` or `/v1beta` passthrough: the public proxy branch mounts `/v1`, `/v1/*`, `/v1beta`, and `/v1beta/*`, then `RuntimeOperationCatalog` allowlists 11 POST operations in `backend/internal/httpapi/runtime/operations.go`.

The strongest architecture is the shared runtime executor in `backend/internal/httpapi/runtime/service.go` and `backend/internal/httpapi/runtime/runtime.go`: operation resolution happens before planning, all supported operations use one planning/execution path, provider-specific request/response/stream/media behavior is concentrated in operation hook maps, and request/usage/pricing/audit persistence is shaped in `observability.go` and `runtime_pricing.go`.

The main launch blockers identified by this audit were not the absence of a pipeline, but gaps against the target gateway contract. QPS and in-flight admission existed, but RPM/TPM/IPM modeling was not found. OpenAI chat/responses conversion existed, and the then-missing `/v1/responses/input_tokens` and `/v1/responses/compact` routes have since been added to the registered runtime allowlist. Context overflow promotion existed as a CLIProxyAPI response replay path and preflight context filtering, but not as a generic provider context-length fallback for every upstream family.

## Current Architecture Diagram

```text
client
  -> platform HTTP middleware: RequestID, RealIP, Recoverer, CORS
  -> runtime branch: /v1, /v1/*, /v1beta, /v1beta/*
  -> runtime auth/admission/ingress telemetry middleware
  -> RuntimeService.handleStreamingProxy
  -> ResolveRuntimeOperation / RuntimeOperationCatalog
  -> buildRequestPlan: active profile, model binding, snapshot, routing plan
  -> runtime access target resolution and context filtering
  -> build planned upstream request: model/path/body rewrite or OpenAI translation
  -> executeRequest: admission, upstream dispatch, failover, feedback events
  -> response branch: non-stream parser or SSE pump/translation
  -> telemetry envelope: request_logs, audit_logs, usage_request_events, outbox
```

Evidence: `backend/internal/platform/http/server.go`, `runtime_branch.go`, `backend/internal/httpapi/runtime/service.go`, `runtime.go`, `operations.go`, `operation_request_hooks.go`, `operation_response_hooks.go`, `operation_stream_hooks.go`, and `observability.go`.

## Repository Map

| Area | Finding | Evidence |
| --- | --- | --- |
| Backend language/runtime | Go backend module targets Go `1.26.2`. | `backend/go.mod`; `backend/cmd/prism-backend/main.go` |
| Backend web framework | HTTP routing uses `github.com/go-chi/chi/v5`; global middleware includes chi `RequestID`, `RealIP`, `Recoverer`, and Prism CORS middleware. | `backend/go.mod`; `backend/internal/platform/http/server.go` |
| Frontend runtime | React 19, TypeScript 5.9, Vite 8, TailwindCSS 4, shadcn, Node `>=24`. | `frontend/package.json` |
| Package manager | Frontend declares `pnpm@10.30.1`; backend uses Go modules. | `frontend/package.json`; `backend/go.mod` |
| Test framework | Backend tests are Go tests under `backend/tests/{contract,integration,runtime,priority}` plus internal package tests. Frontend uses Node test and Playwright. | `backend/tests/AGENTS.md`; `frontend/package.json` |
| Build/lint/typecheck | Backend build is `go build ./cmd/prism-backend`; frontend build is `tsc -b && vite build`; frontend lint is `eslint .`. | `backend/README.md`; `frontend/package.json` |
| Deployment assumptions | Local launcher `start.sh`, Go backend, React/Vite frontend, PostgreSQL, Docker/GHCR images, and bootstrap config selected by `PRISM_CONFIG_PATH`. | `README.md`; `backend/README.md`; `backend/Dockerfile` |

## Endpoint Inventory

Runtime proxy endpoints are exhaustive in `RuntimeOperationCatalog`; all are POST-only and all enter `Service.handleStreamingProxy` through the same mounted runtime handler.

| Method/path | Handler / registry | Stream | Non-stream | Provider/upstream | Shape |
| --- | --- | --- | --- | --- | --- |
| `POST /v1/chat/completions` | `runtimeOperationCatalog` -> `Service.handleStreamingProxy` | Request body `stream` via request hook | Yes | OpenAI family | Pass-through or OpenAI Chat -> Responses translation when terminal config selects it |
| `POST /v1/responses` | same | Request body `stream` via request hook | Yes | OpenAI family | Pass-through or OpenAI Responses -> Chat translation when terminal config selects it |
| `POST /v1/responses/input_tokens` | same, token-count hook collection | No | Yes | OpenAI family | Responses input-token counting adjunct operation |
| `POST /v1/responses/compact` | same | No | Yes | OpenAI family | Responses compaction adjunct operation |
| `POST /v1/images/generations` | same, media hook collection | No; `neverStreamRequest` | Yes | OpenAI image | JSON model extraction/rewrite; response pass-through without usage capture |
| `POST /v1/images/edits` | same, media hook collection | No; `neverStreamRequest` | Yes | OpenAI image | Multipart or JSON model extraction/rewrite; response pass-through without usage capture |
| `POST /v1/messages` | same | Request body `stream` via request hook | Yes | Anthropic | Provider-native path/body with usage hooks |
| `POST /v1/messages/count_tokens` | same, token-count hook collection | No | Yes | Anthropic | Provider-native token-count response with token-count usage extraction |
| `POST /v1beta/models/{model}:generateContent` | path matcher `geminiRuntimeOperationPath(":generateContent")` | No | Yes | Gemini | Path model binding and provider-native Gemini response hooks |
| `POST /v1beta/models/{model}:streamGenerateContent` | path matcher `geminiRuntimeOperationPath(":streamGenerateContent")` | Always stream | No native non-stream route; operation itself is streaming | Gemini | Path model binding and Gemini SSE/usage hooks |
| `POST /v1beta/models/{model}:countTokens` | path matcher `geminiRuntimeOperationPath(":countTokens")` | No | Yes | Gemini | Path model binding and token-count usage extraction |

Management/control endpoints are mounted under `/api` and `/health`. The route contract enumerates profile-scoped management routes for models, endpoints, connections, pricing templates, load-balance strategies/state/events, stats, audit logs, settings, config bundle import/export, header blocklist, and user-agent rules. Direct service route mounts add auth, bootstrap config, profiles, vendors, sidecars, and realtime websocket endpoints. Evidence: `backend/internal/platform/http/management_route_contract.json`, `management_branch.go`, and each management service `MountManagementRoutes`.

Top-level exposed non-runtime endpoints verified:

| Method/path family | Owner | Evidence |
| --- | --- | --- |
| `GET /health` | platform management branch | `backend/internal/platform/http/management_branch.go` |
| `/api/settings/auth*`, `/api/public-bootstrap`, `/api/login`, `/api/logout`, `/api/refresh`, `/api/session` | auth management service | `backend/internal/httpapi/management/auth/service.go` |
| `/api/config/bootstrap*` | bootstrap config service | `backend/internal/httpapi/management/bootstrapconfig/service.go` |
| `/api/config/profile/*`, `/api/config/vendors/*` | config bundle service | `backend/internal/httpapi/management/configbundle/service.go` |
| `/api/endpoints*` | endpoint CRUD and endpoint connection dropdowns | `backend/internal/httpapi/management/endpoints/service.go` |
| `/api/models*` | model CRUD, model targets, model endpoint lookups | `backend/internal/httpapi/management/models/service.go` |
| `/api/profiles*` | profile lifecycle and activation | `backend/internal/httpapi/management/profiles/service.go` |
| `/api/settings/*`, `/api/maintenance/log-retention/jobs` | costing, timezone, log retention, retention jobs | `backend/internal/httpapi/management/settings/service.go` |
| `/api/sidecars*` | global CLIProxyAPI sidecar control plane | `backend/internal/httpapi/management/sidecars/service.go` |
| `/api/vendors*` | global vendor catalog CRUD | `backend/internal/httpapi/management/vendors/service.go` |
| `/api/realtime/ws` | realtime websocket | `backend/internal/httpapi/realtime/service.go` |
| `/api/loadbalance/*`, `/api/stats/*`, `/api/audit/*`, `/api/config/*rules` | load-balance, observability, audit, config rule surfaces | `backend/internal/platform/http/management_route_contract.json` |

## Endpoint Façade Findings

- P2: Runtime endpoints are thin at the router level: `mountRuntimeBranch` attaches one `RuntimeService.Handler()` to `/v1`, `/v1/*`, `/v1beta`, and `/v1beta/*`; exact endpoint classification occurs in `ResolveRuntimeOperation`, not per-route handlers. Evidence: `backend/internal/platform/http/runtime_branch.go`; `backend/internal/httpapi/runtime/operations.go`; `backend/internal/httpapi/runtime/service.go`.
- P1: The runtime handler itself is not a thin façade. `Service.handleStreamingProxy`, `buildRequestPlan`, `executeRequest`, response parsing, feedback, and telemetry live in `service.go` and `runtime.go`; this is a shared pipeline but also concentrates routing, provider dispatch, stream handling, pricing, logging, and feedback in one package. Evidence: `backend/internal/httpapi/runtime/service.go`; `backend/internal/httpapi/runtime/runtime.go`; `backend/internal/httpapi/runtime/observability.go`; `backend/internal/httpapi/runtime/runtime_pricing.go`.
- P0: Missing required OpenAI-family endpoints: no registered `/v1/responses/input_tokens` or `/v1/responses/compact` route was found. Searches performed: `responses/input_tokens|responses/compact|/v1/responses/input_tokens|/v1/responses/compact` over `backend` and `docs` returned no matches outside this audit. Evidence: `backend/internal/httpapi/runtime/operations.go`.
- P2: Management endpoints are conventional CRUD/control handlers rather than the runtime gateway surface; their invalidation semantics are explicit in `management_route_contract.json`. Evidence: `backend/internal/platform/http/management_route_contract.json`; `backend/internal/platform/http/management_branch.go`.

## Shared Pipeline Findings

| Stage | Status | Evidence |
| --- | --- | --- |
| ingress request id / trace id | Present. Chi `RequestID` middleware and runtime spans wrap ingress. | `backend/internal/platform/http/server.go`; `backend/internal/httpapi/runtime/service.go`; `backend/internal/httpapi/runtime/observability.go` |
| authentication / tenant policy | Present for runtime and management middleware; selected-profile management scope is separate from active runtime profile. | `backend/internal/platform/http/runtime_branch.go`; `management_branch.go`; `backend/internal/platform/http/management_route_contract.json` |
| operation resolution | Present and early. Unsupported paths and wrong methods reject before body reads, admission, transport, telemetry, audit, feedback, or side effects. | `backend/internal/httpapi/runtime/service.go`; `backend/tests/runtime/rejected_route_isolation_test.go` |
| body limit / body mode | Present. Runtime JSON and media limits are selected by operation/media hook; path-bound Gemini streaming can avoid request buffering when no replay is needed. | `backend/internal/httpapi/runtime/service.go`; `operation_media_hooks.go` |
| model binding / rewrite | Present. Body-bound operations use operation hooks for model extraction/rewrite; Gemini path-bound operations rewrite the path model. | `backend/internal/httpapi/runtime/runtime.go`; `operation_media_hooks.go` |
| active profile planning | Present. Runtime uses active cached runtime plan, not management `X-Profile-Id`. | `backend/internal/httpapi/runtime/runtime.go`; `backend/internal/httpapi/runtime/AGENTS.md` |
| provider differences | Present as hook maps rather than forked executors. | `operation_request_hooks.go`; `operation_response_hooks.go`; `operation_stream_hooks.go`; `operation_media_hooks.go` |
| side effects | Present through runtime side-effect manager, telemetry outbox, and feedback pipeline. | `service.go`; `telemetry_outbox.go`; `feedback_pipeline.go`; `runtime_side_effects.go` |

P1 gap: the package has a shared pipeline, but the amount of responsibility in `runtime.go` and `service.go` is high. A clean-cut extraction of planning, admission/execution, response capture, and promotion orchestration would improve reviewability without changing the external contract.

## Routing / QPS Overflow / Load-Balancing Findings

- P2: Connection ordering supports stable priority ordering, single strategy, round-robin cursoring, failover status codes, feedback state, retry delay, and ban state. Evidence: `backend/internal/domain/loadbalance/runtime_strategy.go`; `runtime_local_state.go`; `backend/internal/httpapi/runtime/proxy_selector_helpers.go`.
- P2: Per-connection admission gates QPS and separate stream/non-stream in-flight limits before launching upstream transport. Rejections are recorded as load-balance events. Evidence: `backend/internal/domain/loadbalance/runtime_local_state.go`; `backend/internal/domain/loadbalance/runtime_events.go`; `backend/internal/httpapi/runtime/runtime.go`.
- P1: RPM, TPM, and IPM are observability concepts, not verified runtime admission controls. Searches for `RPM`, `TPM`, `IPM`, rate-limit, and token-per-minute terms found stats/dashboard aggregation but no runtime reservation or per-minute token admission path. Evidence: `backend/internal/domain/stats/snapshot.go`; `backend/internal/domain/stats/types.go`; no matching runtime control in `backend/internal/domain/loadbalance/` or `backend/internal/httpapi/runtime/`.
- P2: Exact facade routing exists for OpenAI facade models using weighted eligible context selection and eligible-weight redistribution. Evidence: `backend/internal/httpapi/runtime/runtime.go`; `planning_snapshot.go`; `proxy_selector_helpers.go`.
- P2: Hedging is structurally modeled but currently disabled by `RuntimeStrategy.HedgePolicy()` returning an empty policy. Evidence: `backend/internal/domain/loadbalance/runtime_strategy.go`; `backend/internal/httpapi/runtime/runtime.go`.

## Model Redirection and Context Overflow Findings

- P2: Ordinary model redirection is explicit: requested model resolves exactly, access targets can point to model or connection targets, and the selected target controls upstream model/path rewrite. Evidence: `resolveRequestedModel`, `buildPlannedUpstreamRequest`, and `assembleRequestPlan` in `backend/internal/httpapi/runtime/runtime.go`; `backend/internal/httpapi/runtime/planning_snapshot.go`.
- P2: Context-window-aware target selection exists. Routing decisions carry estimated input tokens, reserved output tokens, usable window, selected context band, skipped target reasons, cost ranking, and facade-selection metadata. Evidence: `runtimeContextRoutingDecision` in `runtime.go`; `backend/internal/httpapi/runtime/planning_snapshot.go`.
- P1: Context overflow promotion is intentionally narrow. It only runs for non-streaming OpenAI chat/responses requests with a configured `context_overflow_promotion_target_id`; it is not a generic provider fallback. Evidence: `planAllowsContextOverflowPromotion`, `tryContextOverflowPromotion`, and `buildExplicitTargetRequestPlan` in `service.go`/`runtime.go`; `backend/migrations/000002_context_overflow_promotion_target.sql`.
- P2: Promotion records additive metadata instead of rewriting requested-model identity. Evidence: `runtimeContextOverflowPromotionDecision`, `attachRuntimeContextOverflowPromotionDecision`, and observability translation tests in `backend/internal/httpapi/runtime/runtime.go`; `backend/internal/httpapi/runtime/observability_translation_test.go`.

## OpenAI Format Conversion Findings

- P2: OpenAI chat/responses conversion is implemented as an explicit bridge, not implicit provider fallback. `CodingAgentFormatBridge.PlanRequest` chooses a translation mode only when an OpenAI connection's probed upstream operation differs from the ingress operation. Evidence: `backend/internal/httpapi/runtime/coding_agent_format_bridge.go`; `backend/internal/httpapi/runtime/operation_translation.go`; `backend/internal/providercompat/providercompat.go`.
- P2: Supported conversion directions are only `openai_responses_to_chat_completions` and `openai_chat_completions_to_responses`; unsupported request, response, or stream shapes return typed domain errors instead of best-effort lossy conversion. Evidence: `backend/internal/httpapi/runtime/operation_translation.go`; `operation_translation_request.go`; `operation_translation_response.go`; `operation_translation_stream.go`.
- P1: Conversion is deliberately partial and safe-shape oriented. Tool-heavy stream shapes, previous response ids, structured outputs, audio, and other unsupported shapes are rejected by translation tests rather than silently degraded. Evidence: `backend/internal/httpapi/runtime/operation_translation_request_test.go`; `operation_translation_stream_test.go`.
- Superseded P0: `/v1/responses/input_tokens` and `/v1/responses/compact` were audit-time gaps. The current runtime catalog now treats them as first-class OpenAI Responses adjunct operations.

## Provider Adapter Findings

- P1: No formal provider adapter interface such as `ProviderAdapter`, `OpenAIAdapter`, `AnthropicAdapter`, or `GeminiAdapter` was found. Searches for those names and `provider adapter` returned no implementation. Provider-specific behavior lives in operation hook registries and `providercompat` metadata. Evidence: `operation_request_hooks.go`; `operation_response_hooks.go`; `operation_stream_hooks.go`; `operation_media_hooks.go`; `backend/internal/providercompat/providercompat.go`.
- P2: The existing hook pattern is cohesive for current providers. Request hooks own generation params and stream intent, response hooks own non-stream parsing and overflow classification, stream hooks own SSE terminal/usage handling, and media hooks own image model extraction/rewrite. Evidence: `backend/internal/httpapi/runtime/operation_hook_residency_test.go`.
- P1: Adapter boundaries become unclear in `runtime.go` and `service.go` because provider-neutral planning, OpenAI translation, CLIProxyAPI overflow promotion, auth/header shaping, and transport execution all meet there. Evidence: `backend/internal/httpapi/runtime/runtime.go`; `service.go`; `coding_agent_format_bridge.go`.

## Streaming Findings

- P2: Streaming is operation-directed. OpenAI chat/responses and Anthropic messages infer `stream` from the request body; Gemini `streamGenerateContent` is always streaming; media and token-count operations force non-stream behavior. Evidence: `operation_request_hooks.go`; `operations.go`; `operation_hook_residency_test.go`.
- P2: SSE response handling uses operation stream hooks for terminal classification and usage merging. OpenAI chat uses the final empty-choices usage chunk, OpenAI responses uses `response.completed`, Anthropic uses `message_stop`, and Gemini treats `done` or `usageMetadata` as terminal. Evidence: `backend/internal/httpapi/runtime/operation_stream_hooks.go`.
- P1: Translated streams are buffered before client write so Prism can fail translation before committing headers. Native passthrough streams can commit earlier and finalize durable activity after completion. Evidence: `backend/internal/httpapi/runtime/service.go`; `backend/internal/httpapi/runtime/coding_agent_format_bridge.go`.
- P1: Streaming context overflow promotion is explicitly excluded. `planAllowsContextOverflowPromotion` returns false for streaming requests, so long streaming requests depend on preflight context routing rather than post-response replay. Evidence: `backend/internal/httpapi/runtime/service.go`; `backend/tests/runtime/context_overflow_promotion_test.go`.

## Logging / Audit / Usage / Pricing Findings

- P2: Runtime logging is operation-aware. `request_logs` and `usage_request_events` include `operation_name`, `upstream_operation_name`, `operation_translation_mode`, `upstream_request_path`, stream outcome, context routing, pricing snapshot, and token/cost columns. Evidence: `backend/migrations/000001_initial_schema.sql`.
- P2: Audit logs are partitioned and capture request/response metadata, optional bodies, and audit-enabled flags at request time. Evidence: `backend/migrations/000001_initial_schema.sql`; `backend/internal/domain/audit/service.go`.
- P2: Usage normalization is centralized in `responseUsage` plus per-provider rules for OpenAI chat, OpenAI responses, Anthropic messages, Gemini generate, and Gemini stream. Evidence: `backend/internal/httpapi/runtime/observability.go`; `operation_response_hooks.go`; `operation_stream_hooks.go`.
- P2: Pricing computes per-million-token costs, cache-read/cache-creation/reasoning components, FX conversion, report currency, pricing snapshots, and unpriced reasons for missing pricing, missing usage, or incomplete streams. Evidence: `backend/internal/httpapi/runtime/runtime_pricing.go`; `backend/migrations/000001_initial_schema.sql`.

## Hooks and Extensibility Findings

- P2: Hooks are the primary extension seam for runtime operations. Every registered operation has request, response, stream, or media hook residency tested against provider, usage rule, stream behavior, and media/token-count exclusions. Evidence: `backend/internal/httpapi/runtime/operation_hook_residency_test.go`.
- P2: The operation catalog is the single extension gate. Adding a new gateway operation requires a method/path entry, hook collection, model-binding source, and route-matrix coverage. Evidence: `backend/internal/httpapi/runtime/operations.go`; `backend/internal/httpapi/runtime/AGENTS.md`; `backend/tests/runtime/operation_route_matrix_test.go`.
- P1: Hooks are maps keyed by collection id, not interface-typed provider modules. This keeps current behavior simple, but provider additions will touch several registries and tests at once. Evidence: `operation_request_hooks.go`; `operation_response_hooks.go`; `operation_stream_hooks.go`; `operation_media_hooks.go`.
- P2: Side-effect extensibility is better separated than provider extensibility: telemetry outbox hooks, feedback pipeline options, side-effect hooks, scheduler workers, and log partition ensuring are explicit seams. Evidence: `backend/internal/httpapi/runtime/telemetry_outbox.go`; `feedback_pipeline.go`; `runtime_side_effects.go`; `log_partitions.go`.

## Test Coverage Findings

- P2: The route matrix now covers the 11 registered runtime operations across OpenAI, Anthropic, Gemini, media, token count, streaming, model binding, usage, and persisted attribution. Evidence: `backend/tests/runtime/operation_route_matrix_test.go`.
- P2: Rejected runtime routes are regression-tested to avoid provider transport, admission state, telemetry, audit, usage, outbox rows, and side effects. Evidence: `backend/tests/runtime/rejected_route_isolation_test.go`.
- P2: Hook residency tests enforce provider/hook ownership for request generation params, streaming observers, non-stream response parsers, media hooks, token-count hooks, and stream hooks. Evidence: `backend/internal/httpapi/runtime/operation_hook_residency_test.go`.
- P2: Context overflow promotion has runtime tests for single promotion, promoted final response, ineligible promotion, facade sibling isolation, and plain 429 non-promotion. Evidence: `backend/tests/runtime/context_overflow_promotion_test.go`; `backend/internal/httpapi/runtime/operation_response_overflow_classifier_test.go`.
- P2: OpenAI conversion has focused request/response/stream tests plus observability attribution tests. Evidence: `backend/internal/httpapi/runtime/operation_translation_request_test.go`; `operation_translation_response_test.go`; `operation_translation_stream_test.go`; `observability_translation_test.go`.
- Superseded P1: At audit time, no test evidence was found for `/v1/responses/input_tokens` or `/v1/responses/compact`. The current route matrix and hook coverage include both adjunct operations.

## Prioritized Gap Table

| Priority | Gap | Impact | Evidence | Recommendation |
| --- | --- | --- | --- | --- |
| Closed | Superseded audit-time `/v1/responses/input_tokens` and `/v1/responses/compact` gap | Gateway now exposes both required OpenAI Responses adjunct operations. | `backend/internal/httpapi/runtime/operations.go`; route-matrix and hook coverage | Preserve catalog entries, hooks, route-matrix cases, docs, and rejection/usage expectations. |
| P1 | RPM/TPM/IPM admission not found | Runtime can enforce QPS and in-flight, but not token/request/image per minute quotas. | `runtime_local_state.go`; stats-only `RPM`/`TPM` hits | Add explicit per-minute admission state or document QPS-only scope. |
| P1 | No formal provider adapter boundary | Provider behavior is distributed across hook maps and runtime orchestration. | `operation_*_hooks.go`; no `ProviderAdapter` search hits | Keep hooks for operation details, but extract a provider capability module before adding more providers. |
| P1 | Runtime orchestration concentration | `service.go`/`runtime.go` own planning, execution, promotion, translation, response handling, and side effects. | `service.go`; `runtime.go` | Split into planner, executor, response pipeline, and promotion coordinator packages. |
| P1 | Context overflow promotion is OpenAI non-stream only | Other families and streaming rely on preflight routing or final upstream errors. | `planAllowsContextOverflowPromotion` in `service.go` | Keep narrow if intentional; otherwise define provider-neutral overflow classifiers and replay safety rules. |
| P2 | Hedging is modeled but disabled | Execution code supports hedged attempts but no strategy can enable it today. | `RuntimeStrategy.HedgePolicy()` in `runtime_strategy.go` | Either remove dormant hedge paths or add explicit config/tests before shipping hedging. |
| P2 | Translated streams buffer before response commit | Safer error behavior, but large translated streams lose streaming latency/memory characteristics. | `service.go`; `coding_agent_format_bridge.go` | Document this tradeoff or implement incremental safe stream translation with terminal rollback constraints. |

## Clean-Cut Recommendations

1. Preserve Responses adjunct operations as first-class catalog entries. Do not treat `/v1` as a catch-all passthrough.
2. Decide whether the runtime quota model is QPS/in-flight only or RPM/TPM/IPM. If RPM/TPM/IPM are product requirements, implement them as admission state beside QPS rather than dashboard-only stats.
3. Extract `runtime.go` into clear modules: operation planning, access-target selection, upstream request building, execution/admission, context overflow promotion, and response attribution.
4. Keep provider differences in operation hooks, but introduce a small provider capability layer before adding new providers or new OpenAI adjunct operations.
5. Keep context overflow promotion explicit and additive. If expanding it beyond CLIProxyAPI/OpenAI non-stream, require provider-specific classifiers and tests for false-positive rate-limit rejection.
6. Preserve the rejected-route isolation invariant: unsupported/wrong-method operations must continue to reject before body reads, transport, admission, telemetry, audit, usage, feedback, or outbox side effects.
7. Keep pricing and usage attribution in the runtime response pipeline, but make missing-usage and incomplete-stream unpriced reasons visible in operator docs and tests.

## Evidence Appendix

### Source Files Reviewed

- Runtime ingress and router: `backend/internal/platform/http/server.go`, `backend/internal/platform/http/runtime_branch.go`, `backend/internal/httpapi/runtime/service.go`.
- Runtime operation contract: `backend/internal/httpapi/runtime/operations.go`, `backend/internal/httpapi/runtime/AGENTS.md`.
- Request planning and execution: `backend/internal/httpapi/runtime/runtime.go`, `planning_snapshot.go`, `proxy_selector_helpers.go`, `generations.go`.
- Provider hook registries: `operation_request_hooks.go`, `operation_response_hooks.go`, `operation_stream_hooks.go`, `operation_media_hooks.go`.
- OpenAI conversion: `coding_agent_format_bridge.go`, `operation_translation.go`, `operation_translation_request.go`, `operation_translation_response.go`, `operation_translation_stream.go`, `backend/internal/providercompat/providercompat.go`.
- Routing/admission: `backend/internal/domain/loadbalance/runtime_strategy.go`, `runtime_local_state.go`, `runtime_events.go`.
- Observability/pricing schema: `backend/migrations/000001_initial_schema.sql`, `backend/migrations/000002_context_overflow_promotion_target.sql`.
- Runtime tests: `backend/tests/runtime/operation_route_matrix_test.go`, `rejected_route_isolation_test.go`, `context_overflow_promotion_test.go`; `backend/internal/httpapi/runtime/operation_hook_residency_test.go`, `operation_response_overflow_classifier_test.go`, `operation_translation_*_test.go`, `observability_translation_test.go`.

### Searches Performed For Missing Or Unclear Behavior

- `responses/input_tokens|responses/compact|/v1/responses/input_tokens|/v1/responses/compact` over `backend` and `docs`: no implementation or docs match found outside this audit.
- `ProviderAdapter|OpenAIAdapter|AnthropicAdapter|GeminiAdapter|provider adapter` over `backend/internal` and `docs`: no formal adapter interface found.
- `RPM|TPM|IPM|RateLimit|token.*minute|minute.*token` over runtime, load-balance, stats, tests, migrations, and docs: stats/dashboard terms found; runtime token/request/image-per-minute admission not found.
- `context_overflow|ContextOverflow|OpenAIResponsesToChat|OpenAIChatCompletionsToResponses|TranslationMode|ProxyEventStream|operation_name` over runtime and runtime tests: promotion, translation, stream, and attribution paths found as cited above.
