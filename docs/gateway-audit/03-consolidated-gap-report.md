# Consolidated Gateway Gap Report

> Historical audit note: this consolidated report preserves pre-implementation gap analysis. Its OpenAI Responses adjunct-route gap is closed in the current launch state, where the runtime allowlist has 11 `POST` operations, including `POST /v1/responses/input_tokens` and `POST /v1/responses/compact`.

## Executive Summary

Both audits converge on the same verdict: Prism has a useful operation-registered gateway foundation, but the target architecture should replace the current gateway core cleanly instead of patching around it. The application has no users, so the clean path is to preserve proven behaviors and tests while deleting or replacing the abstractions that make new gateway features hard to add.

The highest-priority blocking gap in the original audits was the unimplemented OpenAI Responses adjunct pair: `/v1/responses/input_tokens` and `/v1/responses/compact`. That gap is closed in the current launch state. The remaining highest-risk architecture gaps are the lack of provider adapters, concentrated orchestration in `service.go` and `runtime.go`, runtime admission limited to QPS and in-flight controls, narrow OpenAI non-stream context overflow promotion, and observability fields that do not explicitly expose `route_reason` or `usage_source` by name.

The target state should keep the thin mounted HTTP branch and the explicit operation allowlist, but move request execution into a phase-based gateway core with provider adapters, typed hooks, explicit registries, hard streaming commit rules, and accounting events that preserve requested model, effective model, selected upstream, route reason, usage source, and pricing catalog version.

## Audit Inputs Reviewed

- `docs/gateway-audit/01-full-architecture-audit.md`
- `docs/gateway-audit/02-independent-verdict-sisyphus.md`

## Agreement Summary

All reviewed inputs agree that the runtime is operation-registered rather than a broad `/v1` or `/v1beta` passthrough. Direct evidence: `backend/internal/platform/http/runtime_branch.go:23` mounts `/v1` and `/v1beta` prefixes to one runtime handler, and `backend/internal/httpapi/runtime/operations.go:43` defines the exact runtime operation catalog.

All reviewed inputs agree that the current gateway has reusable assets. Preserve the operation catalog idea, route-matrix tests, rejected-route isolation tests, OpenAI chat/responses conversion behavior, OpenAI image media hooks, usage normalization, runtime pricing, telemetry/outbox seams, and context-overflow regression coverage.

All reviewed inputs agree that the target should be Verdict C: replace the core gateway architecture cleanly and migrate operations to it. Do not keep the current core shape for compatibility, and do not throw away the tested behavior as a full rewrite.

All reviewed inputs originally agreed that `/v1/responses/input_tokens` and `/v1/responses/compact` were audit-time gaps. That agreement is historical only. The current launch-state catalog has 11 `POST` operations and includes both routes as first-class OpenAI Responses adjunct operations.

All reviewed inputs agree that provider behavior is implemented through operation hook maps, not provider adapters. Direct evidence: `operation_request_hooks.go:13`, `operation_response_hooks.go:29`, `operation_stream_hooks.go:15`, and `operation_media_hooks.go:34`; direct search found no `ProviderAdapter`, `OpenAIAdapter`, `AnthropicAdapter`, or `GeminiAdapter` symbols.

All reviewed inputs agree that QPS overflow and in-flight admission exist. Direct evidence: `runtime.go:1725-1737` calls `TryBeginConnectionAttempt` with `QPSLimit`, `MaxInFlightNonStream`, and `MaxInFlightStream`; `runtime_local_state.go:220-232` increments QPS and in-flight counters when the policy respects them.

All reviewed inputs agree that the current tests are strong preservation assets. Direct evidence: `operation_route_matrix_test.go:60` covers supported operations, `rejected_route_isolation_test.go:22` protects unsupported-route isolation, `operation_hook_residency_test.go:8` pins hook ownership, and `context_overflow_promotion_test.go:15` tests OpenAI non-stream overflow promotion.

## Partial Agreement

The audits partially agree on observability completeness. The schema preserves substantial attribution fields: `request_logs` has `operation_name`, `upstream_operation_name`, `operation_translation_mode`, target and endpoint ids, stream fields, tokens, costs, `pricing_config_version_used`, and `context_routing` at `backend/migrations/000001_initial_schema.sql:934-1003`; `usage_request_events` repeats many of those fields at `000001_initial_schema.sql:1242-1297`. However, direct search found no backend/schema fields named `route_reason` or `usage_source`, so the target contract is only partially met.

The audits partially agree on streaming safety. Native streams use deferred commit and stream hooks, while translated streams are buffered before response commit in `service.go:546-570`. That avoids committing an untranslatable stream, but it does not yet express the target architecture as a provider-adapter streaming contract with tests proving no retry or overflow after the first downstream byte/event.

The audits partially agree on context-window overflow. It exists and is tested, but it is narrow. `planAllowsContextOverflowPromotion` rejects streaming requests and only allows OpenAI chat/responses operation names in `service.go:750-760`; promotion requires configured target ids checked in `service.go:789-799`.

The audits originally partially agreed on load-balancing maturity because earlier evidence showed hedging modeled in execution but inactive through strategy policy. T16 closed that finding: hedging remains active runtime behavior with `executeHedgedRequest`, `recordRuntimeHedge`, `prism.runtime.hedge.count`, and adaptive hedging regression tests, while streaming commit rules still prevent hedge replay after the first downstream event.

## Conflicting Findings

No hard disagreement survived direct code inspection. The apparent conflicts resolve as scope differences.

The phrase "provider hooks are extensible" is true for current operation-level hooks, but it does not satisfy the target provider-adapter architecture. Direct resolution: hooks are maps keyed by operation collection id; adapter symbols are absent.

The phrase "context overflow fallback exists" is true for non-stream OpenAI chat/responses with explicit promotion targets, but it does not satisfy a generic OpenAI-family or all-provider fallback contract. Direct resolution: `service.go:750-760` excludes streaming and non-OpenAI operations.

The phrase "logging has the right raw material" is true for operation, upstream operation, pricing, stream outcome, token, cost, and `context_routing` fields, but it does not prove explicit `route_reason` and `usage_source` preservation. Direct resolution: direct search found no backend/schema matches for `route_reason` or `usage_source`.

The phrase "QPS overflow exists" is true, but it does not prove RPM/TPM/IPM quota admission. Direct resolution: `runtime_local_state.go:220-232` records QPS and in-flight counters; direct search found RPM/TPM terms in stats surfaces, not equivalent runtime admission controls.

## Prioritized Gap Table

| Gap ID | Severity: P0 / P1 / P2 / P3 | Area | Description | Evidence | Impact | Clean-cut decision: keep / refactor / replace / delete / defer | Owner module | Dependencies | Required tests |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| G-001 | Closed | Superseded audit-time feature gap | OpenAI Responses token-count and compaction endpoints were added after the original audits. | Current runtime allowlist has 11 `POST` operations including `POST /v1/responses/input_tokens` and `POST /v1/responses/compact`. | Required OpenAI Responses adjunct endpoint coverage is now present. | keep | `backend/internal/httpapi/runtime/operations.go`; gateway operation registry | G-002, G-003 | Preserve route-matrix coverage for `POST /v1/responses/input_tokens` and `POST /v1/responses/compact`; rejected-route isolation for wrong methods; usage/accounting assertions. |
| G-002 | P1 | Bad abstraction | Provider-specific behavior is spread across hook maps rather than provider adapters. | `operation_request_hooks.go:13-58`; `operation_response_hooks.go:29-80`; `operation_stream_hooks.go:15-44`; `operation_media_hooks.go:34-47`; adapter-name search returned zero matches. | New provider or operation support touches several maps and keeps provider capabilities implicit. | replace | new `gateway/provider` package plus runtime operation hooks | none | Adapter contract tests for OpenAI, Anthropic, Gemini request build, stream policy, usage extraction, overflow classification, and media behavior. |
| G-003 | P1 | Bad abstraction | Gateway orchestration is concentrated in `Service.handleStreamingProxy`, `Service.writeProxyResponse`, `executeRequest`, and `executeSingleAttempt`. | `service.go:348-419`, `service.go:500-668`, `runtime.go:1412-1751`. | Hard to reason about phase ownership, streaming commit safety, retries, promotion, telemetry, and pricing. | replace | `backend/internal/httpapi/runtime` -> new gateway core modules | G-002 | Characterization tests stay green while extracting planner, executor, response pipeline, and accounting sink. |
| G-004 | P1 | Missing feature | Runtime quota admission covers QPS and in-flight, but not RPM/TPM/IPM runtime reservation. | `runtime.go:1725-1737`; `runtime_local_state.go:220-232`; RPM/TPM direct hits were stats-only. | Required quota behavior can be displayed but not enforced at runtime. | refactor | `backend/internal/domain/loadbalance`; gateway admission phase | G-003 | Per-minute request/token/image quota tests, concurrent reservation/release tests, rejected-admission event tests. |
| G-005 | P1 | Incorrect behavior | Context overflow fallback is narrow OpenAI non-stream replay, not a provider-capability fallback model. | `service.go:750-760`, `service.go:789-799`, `context_overflow_promotion_test.go:15-115`. | Required OpenAI-family fallback exists only for configured non-stream cases and can be misread as generic. | refactor | gateway route planner and provider adapters | G-002, G-003 | Non-stream OpenAI promotion tests; streaming no-promotion tests; provider classifier false-positive tests; backup-model re-entry tests. |
| G-006 | P1 | Unsafe streaming behavior | Closed by T17 stream-boundary tests: OpenAI, Anthropic, and Gemini coverage proves no retry or hedge/overflow replay after the first downstream event. | `.omo/evidence/task-17-fixtures.txt`; `.omo/evidence/task-13-stream-boundary.txt`; `.omo/evidence/task-7-no-retry-stream.txt`; `.omo/evidence/task-11-no-stream-overflow.txt`. | Keep the hard stream commit invariant documented and tested. | closed | gateway streaming phase | G-003 | Preserve pre-commit failover tests, post-first-event no-replay tests, client-disconnect accounting tests, and translated-stream mode tests. |
| G-007 | P1 | Observability/accounting gap | Target requires `route_reason` and `usage_source`, but direct search found no fields by those names. | `000001_initial_schema.sql:934-1003`, `000001_initial_schema.sql:1242-1297`; zero direct matches for `route_reason` or `usage_source`. | Audit/accounting cannot unambiguously explain why a route was selected or where usage came from. | refactor | runtime observability and usage event contract | G-003 | Request-log and usage-event tests asserting route reason, usage source, requested/effective model, upstream, and pricing version. |
| G-008 | P2 | Duplicated logic | OpenAI chat/responses conversion is valuable but sits outside a provider adapter capability boundary. | `coding_agent_format_bridge.go:33-88`; `operation_translation.go:10-24`. | Conversion behavior is harder to compose with new OpenAI adjunct operations. | refactor | OpenAI provider adapter | G-002 | OpenAI adapter conversion tests for chat->responses and responses->chat, streaming and non-streaming. |
| G-009 | P2 | Bad abstraction | `requestPlan` acts as a broad cross-phase data object. | audit 02 `What To Delete or Replace`; `runtime.go:877-945` plan assembly and `runtime.go:1412-1751` execution consume broad plan state. | Phase contracts remain implicit and brittle. | replace | gateway core phase envelopes | G-003 | Typed phase envelope tests and compile-time adapter contract tests. |
| G-010 | P2 | Configuration gap | Closed by T16: hedging remains active runtime behavior with explicit execution, metrics, and adaptive hedging tests. | `.omo/evidence/task-16-hedging.txt`; active refs include `executeHedgedRequest`, `recordRuntimeHedge`, `prism.runtime.hedge.count`, and runtime hedge regressions. | Keep hedging documented as active/tested behavior and preserve the no-hedge-replay-after-stream-commit invariant. | closed | load-balance strategy and executor | G-003 | Preserve adaptive hedging tests, disabled-hedge policy coverage, hedge metrics coverage, and stream post-first-event no-replay coverage. |
| G-011 | P2 | Test gap | Tests pin current behavior but not target provider-adapter or phase contracts. | `operation_route_matrix_test.go:60`; `operation_hook_residency_test.go:8`; audit 02 Implementation Strategy. | Refactor could pass current route tests while missing adapter/registry guarantees. | refactor | backend runtime tests | G-002, G-003 | Adapter contract suite, registry contract suite, phase-level accounting suite. |
| G-012 | P2 | Observability/accounting gap | Pricing version is stored, but pricing is coupled to runtime response capture rather than an explicit pricing registry interface. | `runtime_pricing.go:43-124`; `000001_initial_schema.sql:986`, `000001_initial_schema.sql:1283`. | Target pricing catalog versioning can regress during gateway-core extraction. | keep | runtime pricing, new pricing registry | G-003 | Pricing registry snapshot tests; unpriced-reason tests for missing usage and incomplete streams. |
| G-013 | P2 | Security/privacy gap | Audit body capture and response capture are currently interwoven with response handling. | `service.go:507-510`, `service.go:527-540`, `000001_initial_schema.sql:996-1003`; audit 01 Logging/Audit section. | Adapter migration could accidentally over-capture bodies or lose redaction boundaries. | refactor | runtime audit and observability sink | G-003 | Audit-body enabled/disabled tests, redaction tests, no-body-capture tests for media and streams. |
| G-014 | P3 | Cleanup/future hardening | Runtime docs and code still describe hooks as the primary seam, while the target wants adapters plus typed hooks. | `backend/internal/httpapi/runtime/AGENTS.md`; audit 02 Target Architecture. | Documentation can drift during migration. | refactor | runtime docs and ownership files | G-002 | Docs consistency check after adapter package lands. |

## Gap Taxonomy

- Closed audit-time feature gap: G-001.
- Missing feature: G-004.
- Incorrect behavior: G-005.
- Bad abstraction: G-002, G-003, G-009.
- Duplicated logic: G-008.
- Unsafe streaming behavior: G-006 was closed by T17 stream-boundary proof.
- Observability/accounting gap: G-007, G-012.
- Test gap: G-011.
- Active hedging proof: G-010 was closed by T16 and must remain covered by runtime hedging tests.
- Security/privacy gap: G-013.

## Dependency Graph

```text
G-002 provider adapters
  -> G-001 OpenAI Responses adjunct operations
  -> G-005 provider-capability context overflow fallback
  -> G-008 OpenAI conversion capability cleanup

G-003 gateway core phase split
  -> G-004 runtime RPM/TPM/IPM admission
  -> G-006 streaming commit boundary
  -> G-007 explicit route_reason and usage_source accounting
  -> G-009 typed phase envelopes
  -> G-012 pricing registry interface
  -> G-013 audit/privacy boundary

G-010 is closed by T16 active-hedging evidence; preserve hedging tests and metrics while enforcing no replay after stream commit.
G-011 target contract tests must be added before completing G-002 or G-003.
G-014 documentation cleanup waits until G-002 and G-003 are real.
```

Recommended order after T16/T17 closure evidence: keep G-006 and G-010 covered by regression tests, then finish remaining open provider, routing, accounting, and docs gaps without reopening old planner or hook-map seams.

## Clean-Cut Architecture Decisions

Because there are no users, do not preserve internal compatibility seams that make the gateway harder to reason about.

Replace the core orchestrator instead of gradually adding more branches to `Service.handleStreamingProxy`, `Service.writeProxyResponse`, `executeRequest`, and `executeSingleAttempt`.

Replace hook maps as the primary provider boundary. Keep operation hooks as provider-adapter internals where they are useful, but do not make new features depend on editing four separate global hook maps.

Replace `/v1` passthrough thinking with explicit operation registration. Missing OpenAI token counting and compaction should be first-class operations or removed from the product contract; they should not be silently proxied as unknown vendor routes.

Keep hedging as active runtime behavior because T16 proved it is configured, observable, and tested through `executeHedgedRequest`, `recordRuntimeHedge`, `prism.runtime.hedge.count`, and adaptive hedging regressions. The streaming commit invariant still forbids any hedge replay after the first downstream byte or event.

Refactor observability into an explicit accounting contract. If the target says route reason and usage source must be preserved, those fields or an equivalent typed JSON contract must exist and be tested.

Refactor context overflow promotion as a provider-capability policy. Keep current OpenAI non-stream behavior as characterization coverage, but do not expand it through ad hoc branches.

## Recommended Target-State Architecture

### Module Layout

```text
backend/internal/httpapi/runtime/
  route_facade.go        # HTTP-only facade, operation resolution, body limits
  runtime_compat.go      # temporary bridge during migration only
backend/internal/gateway/core/
  pipeline.go            # phase runner
  envelopes.go           # typed phase inputs and outputs
  errors.go              # gateway-domain errors
backend/internal/gateway/registry/
  operations.go          # operation registry
  capabilities.go        # provider and operation capabilities
  pricing.go             # pricing catalog registry
backend/internal/gateway/provider/
  adapter.go             # ProviderAdapter interface
  openai/                # OpenAI adapter and conversion capability
  anthropic/             # Anthropic adapter
  gemini/                # Gemini adapter
backend/internal/gateway/routing/
  planner.go             # model/upstream route planning
  redirects.go           # model redirect and upstream redirect policy
  overflow.go            # context-window overflow policy
backend/internal/gateway/admission/
  admission.go           # QPS/RPM/TPM/IPM/in-flight reservation
backend/internal/gateway/streaming/
  commit.go              # first-byte/event commit boundary
  sse.go                 # stream classification and usage merge
backend/internal/gateway/accounting/
  sink.go                # request log, audit log, usage event, pricing event
```

### Main Interfaces

```go
type OperationRegistry interface {
    Resolve(method string, path string) (Operation, AllowedMethods, bool)
}

type ProviderAdapter interface {
    ParseRequest(Operation, RequestEnvelope) (ProviderRequest, error)
    BuildUpstream(ProviderRequest, RoutePlan) (UpstreamRequest, error)
    StreamPolicy(Operation, ProviderRequest) StreamPolicy
    AdaptNonStreamResponse(Operation, UpstreamResponse) (ClientResponse, UsageEnvelope, error)
    AdaptStream(Operation, StreamEnvelope) (StreamResult, error)
    ClassifyOverflow(Operation, UpstreamResponse) OverflowClassification
}

type RoutePlanner interface {
    Plan(RequestEnvelope, Operation, ProviderRequest) (RoutePlan, error)
}

type AdmissionController interface {
    Reserve(RouteAttempt, UsageEstimate) (AdmissionLease, AdmissionDecision)
}

type AccountingSink interface {
    RecordAttempt(AccountingEvent) error
    RecordFinal(AccountingEvent) error
}
```

### Request Flow

HTTP facade resolves operation, enforces request body limits, builds a request envelope, and hands it to the gateway pipeline. The pipeline runs phases in order: operation resolution, auth/admission precheck, parse, model binding, route planning, provider adaptation, admission reservation, upstream execution, response adaptation, accounting, and side-effect handoff.

### Routing Flow

The route planner consumes explicit registries: model registry, upstream registry, route policy registry, capability registry, and pricing registry. Model redirects re-enter route planning. Upstream redirects narrow the candidate set explicitly and record a route reason. Backup-model overflow targets are selected by provider capability and configured policy, not by ad hoc response handling.

### Streaming Flow

Before the first downstream byte or event, the gateway may reject, fail over, translate, or choose a backup target. After the first downstream byte or event, the gateway must not retry, hedge, promote, or switch upstreams. It may only classify stream outcome, merge usage, record audit/accounting, and surface downstream errors. Translated OpenAI streams must be declared either buffered or incremental, with tests for the chosen mode.

### Logging/Audit/Usage/Pricing Flow

Every attempt event and final event should carry requested model, effective model, selected upstream, route reason, operation name, upstream operation name, translation mode, usage source, stream outcome, context-overflow decision, pricing catalog version, pricing snapshot, and audit capture policy. Request-log and usage-event writes should come from typed accounting events, not reconstruction from a mutable `requestPlan`.

## Required Test Matrix

| Test Area | Required tests | Covers gaps |
| --- | --- | --- |
| Operation registry | RED/GREEN route-matrix cases for all existing operations plus `/v1/responses/input_tokens` and `/v1/responses/compact`; wrong-method and unsupported-route isolation. | G-001, G-011 |
| Provider adapters | Contract tests for OpenAI, Anthropic, and Gemini parse/build/usage/stream/overflow behavior. | G-002, G-008 |
| Gateway phases | Characterization tests proving current behavior before extraction; phase tests for request envelope, route plan, execution result, and accounting event outputs. | G-003, G-009 |
| Admission | QPS, in-flight, RPM, TPM, IPM reservation and release tests, including concurrent requests and rejected-admission event logging. | G-004 |
| Context overflow | Non-stream OpenAI promotion stays green; streaming promotion stays impossible; provider false-positive classifiers reject rate-limit errors; backup model re-enters load balancing. | G-005 |
| Streaming commit | Pre-commit failover passes; after the first downstream byte/event, retry, hedge replay, promotion, and upstream switching remain impossible; translated-stream buffering or incremental behavior is explicitly tested. | G-006 closed by T17 |
| Observability/accounting | Request-log and usage-event tests for requested model, effective model, selected upstream, route reason, usage source, operation/upstream operation, stream outcome, context decision, and pricing version. | G-007, G-012 |
| Active hedging | Runtime hedging remains configured, emits hedge metrics, has adaptive hedging tests, and cannot replay after stream commit. | G-010 closed by T16/T17 |
| Audit/privacy | Audit capture enabled/disabled tests, body redaction tests, no unintended body capture for streams/media, and response-body capture boundaries. | G-013 |
| Docs consistency | Runtime ownership docs and API docs match the final operation registry and adapter architecture. | G-014 |

## Open Questions

1. What exact wire contract should Prism expose for OpenAI Responses token counting and compaction: vendor-native paths, Prism-defined compatibility paths, or both? This materially affects operation names, usage extraction, and route-matrix fixtures.
2. Are RPM, TPM, and IPM hard runtime quotas or dashboard-only metrics? If hard quotas, the owner must define scope: per endpoint, terminal target, public model, profile, proxy key, or a combination.
3. Should OpenAI translated streams remain buffered for safety, or must they be incremental? This determines the streaming adapter contract and memory/latency tradeoff.
4. What canonical vocabulary should `route_reason` and `usage_source` use? The current schema has `context_routing` and usage normalization, but no fields by those exact names.
