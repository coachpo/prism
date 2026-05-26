# BACKEND RUNTIME HTTPAPI KNOWLEDGE BASE

## OVERVIEW
`runtime/` owns Prism's operation-registered runtime proxy contract behind the mounted `/v1` and `/v1beta` prefixes. It resolves exact supported operations at ingress, carries operation metadata through request planning, routes provider-native differences through hook collections, persists `operation_name`, and keeps shared execution, telemetry, feedback, side effects, and partition ensuring inside one backend-owned runtime surface.

## STRUCTURE
```text
runtime/
├── operations.go                # Exact supported runtime operations, method/path matching, hook collection ids
├── service.go                   # Ingress rejection, shared executor wiring, transport, workers
├── runtime.go                   # Request planning, model binding, rewrite rules, upstream proxy flow
├── planning_snapshot.go         # Proxy-target snapshot assembly and resolution ordering helpers
├── proxy_selector_helpers.go    # Proxy model selection helpers used by request planning
├── cache.go                     # Shared runtime cache reads and snapshots
├── request_generation_params.go # Buffered generation-param extraction orchestration
├── operation_request_hooks.go   # Request hook registry and streaming-intent selection
├── operation_response_hooks.go  # Non-stream response parsing by operation kind
├── operation_stream_hooks.go    # SSE terminal and usage parsing by operation
├── operation_media_hooks.go     # Media model extraction and multipart/json rewrite helpers
├── generations.go               # Generation-request shaping and upstream helpers
├── observability.go             # Request-log and usage-event shaping with `operation_name`
├── log_partitions.go            # Runtime partition ensuring and cache
├── telemetry_outbox.go          # Durable telemetry enqueue and publisher wakeups
├── feedback_pipeline.go         # Runtime feedback persistence and worker handoff
├── runtime_side_effects.go      # Runtime side-effect manager and shutdown behavior
└── runtime_pricing.go           # Runtime pricing snapshots and usage pricing helpers
```

## WHERE TO LOOK
- Exact supported operations, hook collection ids, streaming flags, and model-binding sources: `operations.go`
- Ingress rejection before body reads, wrong-method handling, shared executor wiring, and response branching: `service.go`
- Request planning, model binding, request path rewrites, target resolution, and shared upstream execution: `runtime.go`, `generations.go`, `planning_snapshot.go`, `proxy_selector_helpers.go`
- Buffered generation-param extraction and operation-directed request hooks: `request_generation_params.go`, `operation_request_hooks.go`
- Non-stream response parsing for text generation, token count, and media operations: `operation_response_hooks.go`
- SSE terminal classification and usage merging for OpenAI, Anthropic, and Gemini stream operations: `operation_stream_hooks.go`
- Media-model extraction and multipart/json rewrite rules for OpenAI image operations: `operation_media_hooks.go`
- Request-log and usage-event shaping plus `operation_name` persistence: `observability.go`, `../../../migrations/000001_initial_schema.sql`
- Telemetry, feedback, and runtime side-effect ownership: `telemetry_outbox.go`, `feedback_pipeline.go`, `runtime_side_effects.go`
- Partition ensuring and partition-cache behavior: `log_partitions.go`, `../../platform/logretention/`
- Internal runtime regression coverage: `operations_test.go`, `service_ingress_test.go`, `request_generation_params_test.go`, `request_generation_params_runtime_test.go`, `operation_hook_residency_test.go`, `operation_media_hooks_test.go`, `operation_response_hooks_test.go`, `runtime_test.go`
- Route-matrix and rejected-route isolation coverage: `../../../tests/runtime/operation_route_matrix_test.go`, `../../../tests/runtime/rejected_route_isolation_test.go`, `../../../tests/runtime/request_generation_params_contract_test.go`, `../../../tests/integration/runtime_route_matrix_test.go`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `operations.go` as the single source of truth for supported runtime method/path pairs, hook collection ids, streaming flags, and model-binding sources.
- Keep selected-profile management scope out of proxy traffic. Runtime request planning uses the active runtime state, not `X-Profile-Id` management headers.
- Keep unsupported or wrong-method requests rejecting before body reads, runtime admission, provider transport, telemetry, audit, feedback, or runtime side effects.
- Keep the shared execution core in `service.go` and `runtime.go`; provider-native differences belong in request, response, stream, or media hooks instead of forked executors.
- Keep token-count operations out of generation-only parsing and usage assumptions.
- Keep media operations on dedicated media hooks instead of text-generation request/response heuristics.
- Keep telemetry, feedback, and runtime side-effect work on durable outboxes or worker seams instead of the hot request path.
- Keep runtime partition ensuring here plus `../../platform/logretention/`; handlers must not create or drop partitions ad hoc.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not describe mounted `/v1` and `/v1beta` prefixes as broad passthrough support.
- Do not add generic OpenAI or vendor fallback behavior outside the allowlist in `operations.go`.
- Do not inject management-only `X-Profile-Id` logic or auth-session state into runtime proxy handlers.
- Do not reuse text-generation hooks for media or token-count operations.
- Do not bypass the telemetry outbox, feedback pipeline, or runtime side-effect manager with inline writes or sends.
- Do not duplicate upstream auth/header shaping, operation matching, or provider-path parsing in sibling packages.
- Do not run retention cleanup or partition maintenance outside `log_partitions.go` and `../../platform/logretention/`.
