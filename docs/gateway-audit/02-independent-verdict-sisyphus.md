# Independent Architecture Verdict

> Historical audit note: this verdict records a pre-implementation read of the gateway architecture. Any statements about the older runtime catalog or unimplemented Responses adjunct routes are superseded by the current launch state, where the runtime allowlist has 11 `POST` operations, including `POST /v1/responses/input_tokens` and `POST /v1/responses/compact`.

## Verdict

C. Replace the core gateway architecture cleanly and migrate endpoints to it.

Do not choose A or B. The current implementation has valuable pieces, but keeping the core shape would preserve the wrong boundary: endpoint routing, planning, provider translation, retries/failover, overflow promotion, response capture, telemetry, feedback, and pricing are still too entangled inside the current runtime service.

Do not choose D or E either. A full rewrite would discard useful, tested assets: the operation catalog idea, route-matrix tests, OpenAI translation helpers, media hooks, pricing calculator, telemetry schema, feedback/outbox seams, and context-overflow tests.

## Why

The long-term simplest path is a clean gateway-core replacement inside the existing repo, with endpoint-by-endpoint migration. The app has no users, so compatibility shims are not a reason to keep the current internals. But the existing code already proves many semantics and should be mined rather than thrown away.

The decisive problem is not missing functionality alone. It is architectural shape: provider behavior is distributed across hook maps, runtime orchestration lives across very large `service.go` and `runtime.go` flows, and the target architecture asks for provider adapters, explicit registries, typed phases, and clean retry/streaming boundaries. That is a core replacement, not a targeted patch.

## Evidence

Runtime routing is already thin at the HTTP branch: `backend/internal/platform/http/runtime_branch.go` mounts one runtime handler for `/v1`, `/v1/*`, `/v1beta`, and `/v1beta/*`. The operation allowlist lives in `backend/internal/httpapi/runtime/operations.go`. At audit time, `RuntimeOperationCatalog` held the original OpenAI chat completions, OpenAI responses, OpenAI image generations, OpenAI image edits, Anthropic messages, Anthropic count tokens, Gemini generate content, Gemini stream generate content, and Gemini count tokens operations. The current launch-state catalog adds the OpenAI Responses adjunct routes, `POST /v1/responses/input_tokens` and `POST /v1/responses/compact`, for a total of 11 `POST` operations.

The shared gateway pipeline exists but is over-concentrated. `Service.handleStreamingProxy` in `backend/internal/httpapi/runtime/service.go` resolves operations, applies body limits, chooses streaming or buffered request mode, builds the plan, executes it, and hands off to response handling. `Service.writeProxyResponse` then owns stream/non-stream response branching, OpenAI stream translation buffering, non-stream overflow inspection, context overflow promotion, telemetry handoff, and response commit behavior.

The execution loop was similarly concentrated in the original audit. `Service.executeRequest` and `Service.executeSingleAttempt` in `backend/internal/httpapi/runtime/runtime.go` owned terminal attempts, hedging orchestration, admission rejection, failover status handling, feedback events, upstream URL/header construction, provider transport, and attempt attribution. T16 later closed the hedging-specific concern by proving hedging remains active runtime behavior with execution, metrics, and tests; the broader phase-separation finding remains architectural context.

Provider-specific behavior is hook-map based rather than adapter based. `operation_request_hooks.go`, `operation_response_hooks.go`, `operation_stream_hooks.go`, and `operation_media_hooks.go` define typed function fields for request stream detection, usage parsing, stream terminal classification, and image model rewriting. Searches for `ProviderAdapter`, `OpenAIAdapter`, `AnthropicAdapter`, `GeminiAdapter`, and `provider adapter` found no formal adapter boundary.

OpenAI chat/responses conversion exists and should be preserved as behavior, not as the final boundary. `backend/internal/httpapi/runtime/coding_agent_format_bridge.go` exposes `CodingAgentFormatBridge.PlanRequest`, `TranslateRequest`, `TranslateResponse`, and `ProxyEventStreamAndCaptureCompletedResponse`. `backend/internal/httpapi/runtime/operation_translation.go` defines `TranslationModeOpenAIResponsesToChatCompletions` and `TranslationModeOpenAIChatCompletionsToResponses`, with safe-shape rejection.

OpenAI image endpoints exist as first-class operations. `operations.go` registers `/v1/images/generations` and `/v1/images/edits`, while `operation_media_hooks.go` handles image-generation JSON model rewrite and image-edit JSON or multipart model rewrite. Tests in `backend/tests/runtime/operation_route_matrix_test.go` cover both endpoints.

Routing and load balancing have useful foundations but should be re-homed behind cleaner registries. `backend/internal/domain/loadbalance/runtime_local_state.go` implements `TryBeginConnectionAttempt` with QPS and stream/non-stream in-flight admission. `backend/internal/domain/loadbalance/runtime_strategy.go` supports stable ordering, single and round-robin strategies, failover status codes, feedback policy, and hedging policy. T16 evidence supersedes the original inactive-hedging read: hedging is active, observable, and regression-tested. `backend/internal/httpapi/runtime/planning_snapshot.go` shows model access targets can re-enter model routing and context filtering.

Context overflow is real but too special-case for the target architecture. `service.go` limits promotion through `planAllowsContextOverflowPromotion` and `tryContextOverflowPromotion`, while `backend/migrations/000002_context_overflow_promotion_target.sql` adds `context_overflow_promotion_target_id`. `backend/tests/runtime/context_overflow_promotion_test.go` proves non-stream OpenAI promotion, final promoted response, ineligible promotion behavior, facade sibling isolation, and 429 classification boundaries.

Logging, audit, usage, and pricing already have the right raw material. `backend/migrations/000001_initial_schema.sql` stores `operation_name`, `upstream_operation_name`, `operation_translation_mode`, `resolved_target_model_id`, `selected_terminal_target_id`, `upstream_request_path`, `context_routing`, stream outcome fields, token fields, pricing snapshot fields, and `pricing_config_version_used`. `backend/internal/httpapi/runtime/runtime_pricing.go` calculates per-token pricing, cache/reasoning components, FX conversion, pricing snapshot fields, and unpriced reasons.

The current tests are a major preservation asset. `backend/tests/runtime/operation_route_matrix_test.go` covers the registered operation matrix, model binding, upstream path behavior, usage, generation params, and attribution. `backend/tests/runtime/rejected_route_isolation_test.go` proves unsupported or wrong-method routes stay outside transport, admission, persistence, and side effects. `backend/internal/httpapi/runtime/operation_hook_residency_test.go` pins hook coverage for every current operation.

The feature gaps were architectural signals. The audit-time search for `responses/input_tokens` and `responses/compact` identified the adjunct-route gap that is now closed. Searches for RPM/TPM/IPM found stats surfaces in `backend/internal/domain/stats`, but no runtime admission or reservation path comparable to QPS/in-flight admission. Searches for provider adapter names found no formal adapter interface. These are not hard to patch one-by-one, but patching them into the existing shape would compound the current concentration.

## What To Preserve

- `backend/internal/httpapi/runtime/operations.go` as the seed for an explicit operation registry, with current Responses token counting and compaction adjunct operations preserved.
- OpenAI conversion behavior from `coding_agent_format_bridge.go` and `operation_translation*.go`, moved behind an OpenAI-family adapter.
- Media model extraction and rewrite logic from `operation_media_hooks.go`.
- Usage normalization ideas from `observability.go`, `operation_response_hooks.go`, and `operation_stream_hooks.go`.
- Pricing calculation from `runtime_pricing.go`, but behind an explicit pricing catalog/version interface.
- Telemetry, feedback, side-effect, and partition handoff seams from `telemetry_outbox.go`, `feedback_pipeline.go`, `runtime_side_effects.go`, and `log_partitions.go`.
- Route-matrix, rejected-route, hook-residency, translation, overflow, pricing, and observability tests as characterization coverage for migration.

## What To Delete or Replace

- Replace `Service.handleStreamingProxy` as the central gateway orchestrator.
- Replace `Service.writeProxyResponse` as the combined response, translation, promotion, telemetry, and commit coordinator.
- Replace `requestPlan` as the broad cross-phase data object with explicit typed phase outputs.
- Replace provider hook maps as the primary extensibility model with provider adapters plus operation-level phase hooks.
- T16 resolved the hedging-specific cleanup concern by keeping hedging active, configured, observable, and tested; do not delete or describe that path as inactive.
- Replace ad hoc OpenAI-family context overflow coupling with a provider-capability driven overflow policy.
- Replace QPS-only admission terminology if product requirements require RPM/TPM/IPM; do not hide dashboard-only metrics behind runtime quota language.

## Target Architecture

The target should keep the existing HTTP branch thin: route facades only resolve a registered operation and hand a request envelope to the gateway pipeline. All runtime operations should live in an explicit route registry with method, path matcher, request model-binding source, streaming capability, media limits, provider family, operation kind, and response-shape contract.

The gateway pipeline should be phase-based: ingress, operation resolution, auth/admission, request parse, model resolution, route planning, provider adaptation, upstream selection, execution, response adaptation, usage extraction, pricing, audit/logging, and side-effect handoff. Each phase should have a typed input/output and a typed hook point. No phase should need the full mutable state currently carried by `requestPlan`.

Provider adapters should own provider-specific protocol behavior. OpenAI, Anthropic, Gemini, and future adapters should expose request parsing, model binding, native path building, stream detection, stream terminal classification, non-stream usage extraction, token-count support, image/media behavior, overflow classification, and response preservation/translation capabilities. OpenAI-family conversion should be an adapter capability, not gateway-core branching.

Registries should be explicit and separate: operation registry, provider capability registry, route-planning registry, pricing catalog registry, and model/upstream routing registry. The runtime pipeline should consume those registries rather than reach directly into scattered maps and config fields.

Streaming must have a hard commit boundary. Before the first downstream byte/event, the gateway may fail over or reject. After the first byte/event, it must not transparently retry or switch upstreams; it may only classify, log, and surface the stream outcome. Translated streaming must either preserve that boundary incrementally or be documented as buffered/non-incremental.

Logging and audit should be mandatory phase outputs, not later reconstruction. Every final and attempted request should carry requested model, effective model, selected upstream, route reason, operation name, upstream operation name, usage source, stream outcome, pricing catalog version, and context-overflow decision.

## Implementation Strategy

1. Freeze current behavior with characterization tests. Keep route-matrix coverage for `/v1/responses/input_tokens` and `/v1/responses/compact` alongside the other registered operations.
2. Introduce new gateway-core package boundaries without moving all behavior at once: operation registry, provider adapter interface, typed phase envelopes, pricing catalog interface, and logging/audit event contract.
3. Port OpenAI chat/responses and image generation/editing first because they exercise conversion, media, streaming, non-streaming, pricing, and logging.
4. Port Anthropic and Gemini generation/token-count operations next, using adapter contract tests to prove provider-specific request, response, and stream behavior.
5. Add runtime admission for the quotas the product actually requires. If RPM/TPM/IPM are required, implement them beside QPS/in-flight in the admission phase with tests and logs.
6. Rebuild model redirect and context-window overflow on the new route-planning interfaces. Backup models must re-enter load balancing; upstream redirects may narrow candidates explicitly; public response shape preservation must be adapter-configured.
7. Retire old orchestration paths once all route-matrix, rejected-route, translation, overflow, pricing, and attribution tests pass on the new core. Delete rather than shim.

## Risks and Mitigations

The largest risk is regressing behavior that the current tests already cover. Mitigate this by treating `operation_route_matrix_test.go`, `rejected_route_isolation_test.go`, `context_overflow_promotion_test.go`, hook-residency tests, and translation tests as the migration harness.

The second risk is designing an adapter layer that is too abstract. Mitigate this by requiring each adapter method to correspond to a current phase requirement: model extraction, request build, stream detection, usage extraction, overflow classification, response conversion, and audit attribution.

The third risk is streaming semantics. Mitigate this with explicit tests proving no transparent retry after the first downstream event, plus separate tests for pre-commit failover and post-commit stream outcome logging.

The fourth risk is scope creep. Mitigate this by migrating one operation family at a time and deleting old paths immediately after equivalent tests are green.

## Confidence

High.

The evidence is consistent across direct code inspection, schema inspection, tests, and the existing audit. The current system is not throwaway, but its core boundaries do not match the target architecture. Verdict C gives the best balance: clean replacement of the gateway core, preservation of proven modules and tests, and no compatibility burden for internals that have not shipped to users.
