# BACKEND RUNTIME HTTPAPI KNOWLEDGE BASE

## OVERVIEW
`runtime/` owns Prism's operation-registered runtime proxy contract behind the mounted `/v1` and `/v1beta` prefixes. It resolves exact supported operations at ingress, carries operation metadata through request planning, resolves requested models by exact `planningSnapshot.ModelsByID` lookup, routes provider-native differences through hook collections, persists `operation_name`, and keeps shared execution, durable request-history telemetry, feedback, side effects, and partition ensuring inside one backend-owned runtime surface.

## STRUCTURE
```text
runtime/
├── operations.go                # Exact supported runtime operations, method/path matching, hook collection ids
├── service.go                   # Ingress rejection, shared executor wiring, transport, workers
├── runtime.go                   # Request planning, model binding, rewrite rules, upstream proxy flow
├── runtime_planner.go           # Final plan assembly
├── routing_plan*.go             # Routing plan compilation and validation helpers
├── planning_snapshot.go         # Access-target snapshot assembly and resolution ordering helpers
├── planning_snapshot_legacy.go  # Legacy snapshot compatibility helpers
├── proxy_selector_helpers.go    # Access-target ordering helpers used by request planning
├── cache.go                     # Shared runtime cache reads and snapshots
├── request_generation_params.go # Internal generation-param extraction orchestration
├── *_adapter_bridge.go          # Runtime-to-gateway provider adapter bridge files
├── gateway_*_bridge.go          # Gateway core and typed-hook bridge files
├── provider_usage_conversion.go # Provider usage normalization bridge
├── operation_request_hooks.go   # Request hook registry and streaming-intent selection
├── operation_response_hooks.go  # Non-stream response parsing by operation
├── operation_stream_hooks.go    # SSE terminal and usage parsing by operation
├── operation_translation.go     # OpenAI strict mode-equality and rejection boundary
├── openai_models.go             # Local OpenAI model-list filtering and request branching
├── codex_models.go              # Mutable Codex catalog synthesis plus ETag/304 handling
├── codex_models_updater.go      # Startup/24h upstream refresh with embedded fallback
├── codex_client_models.json     # Verbatim OpenAI Codex model metadata template
├── generations.go               # Generation-request shaping and upstream helpers
├── observability.go             # Request-log and usage-event shaping with operation metadata
├── log_partitions.go            # Runtime partition ensuring and cache
├── telemetry_outbox.go          # Durable telemetry enqueue and publisher wakeups
├── feedback_pipeline.go         # Runtime feedback persistence and worker handoff
├── runtime_side_effects.go      # Runtime side-effect manager and shutdown behavior
└── runtime_pricing.go           # Runtime pricing snapshots and usage pricing helpers
```

## WHERE TO LOOK
- Exact supported operations, hook collection ids, streaming flags, and model-binding sources: `operations.go`
- Ingress rejection before body reads, wrong-method handling, shared executor wiring, and response branching: `service.go`
- Request planning, model binding, request path rewrites, unified access-target resolution, final-target attribution, exact `planningSnapshot.ModelsByID` requested-model lookup, and shared upstream execution: `runtime.go`, `runtime_planner.go`, `routing_plan*.go`, `generations.go`, `planning_snapshot.go`, `planning_snapshot_legacy.go`, `proxy_selector_helpers.go`
- Runtime-to-gateway adapter seams and usage normalization: `*_adapter_bridge.go`, `gateway_core_bridge.go`, `gateway_typed_hooks_bridge.go`, `provider_usage_conversion.go`
- Automatic generation-param extraction and operation-directed request hooks: `request_generation_params.go`, `operation_request_hooks.go`
- Non-stream response parsing for text generation and token count operations: `operation_response_hooks.go`
- SSE terminal classification and usage merging for OpenAI, Anthropic, and Gemini stream operations: `operation_stream_hooks.go`
- OpenAI strict mode equality, mismatched-target skipping, unsupported-wire rejection behavior, and the read-only mode check: `operation_translation.go`, `planning_snapshot.go`, `../../../openaimodecheck/`, `runtime_test.go`
- Local OpenAI and refreshable Codex client model-list responses: `openai_models.go`, `codex_models.go`, `codex_models_updater.go`, `codex_client_models.json`, `codex_models_test.go`
- Request-log and usage-event shaping plus `operation_name` persistence: `observability.go`, `../../../migrations/000001_initial_schema.sql`
- Telemetry, feedback, and runtime side-effect ownership: `telemetry_outbox.go`, `feedback_pipeline.go`, `runtime_side_effects.go`
- Partition ensuring and partition-cache behavior: `log_partitions.go`, `../../platform/logretention/`
- Internal runtime regression coverage: `operations_test.go`, `service_ingress_test.go`, `request_generation_params_test.go`, `request_generation_params_runtime_test.go`, `operation_hook_residency_test.go`, `operation_response_hooks_test.go`, `operation_response_overflow_classifier_test.go`, `gateway_typed_hooks_bridge_test.go`, `planning_snapshot_contract_test.go`, `routing_plan_test.go`, `runtime_test.go`
- Route-matrix, native compatibility, streaming, body-limit, and rejected-route coverage: `../../../tests/runtime/body_limits_test.go`, `../../../tests/runtime/operation_route_matrix_test.go`, `../../../tests/runtime/operation_route_matrix_openai_compatibility_test.go`, `../../../tests/runtime/runtime_streaming_buffering_test.go`, `../../../tests/runtime/rejected_route_isolation_test.go`, `../../../tests/runtime/request_generation_params_contract_test.go`, `../../../tests/integration/runtime_route_matrix_test.go`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `operations.go` as the single source of truth for supported runtime method/path pairs, hook collection ids, streaming flags, and model-binding sources.
- Keep management scope out of proxy traffic. Runtime request planning uses the current runtime snapshot, not `X-Profile-Id` management headers.
- Keep requested-model resolution exact. Runtime planning starts from `planningSnapshot.ModelsByID` using the client-supplied model ID exactly, then follows ordinary access-target ordering; do not add regex matching or capability-metadata expansion in this package.
- Keep unsupported or wrong-method requests rejecting before body reads, runtime admission, provider transport, telemetry, audit, feedback, or runtime side effects.
- Keep the shared execution core in `service.go` and `runtime.go`; provider-native differences belong in request, response, or stream hooks instead of forked executors.
- Keep retired exact-facade and context-fit preflight behavior out of runtime planning; preserve requested/resolved model observability through the ordinary target plan.
- Keep token-count operations out of generation-only parsing and usage assumptions.
- Keep OpenAI text routing native-only: both the model's accepted format and the connection capability must support the ingress operation, otherwise planning skips or rejects the attempt.
- Keep `GET /v1/models` local. Requests with a `client_version` query parameter use the current Codex catalog shape and deterministic ETag; startup and 24-hour refreshes may replace the embedded seed, while requests without `client_version` preserve the OpenAI `object`/`data` list.
- Keep telemetry, feedback, and runtime side-effect work on durable outboxes or worker seams instead of the hot request path.
- Keep runtime partition ensuring here plus `../../platform/logretention/`; handlers must not create or drop partitions ad hoc.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not describe mounted `/v1` and `/v1beta` prefixes as broad passthrough support.
- Do not add generic OpenAI or vendor fallback behavior outside the allowlist in `operations.go`.
- Do not inject management-only `X-Profile-Id` logic or auth-session state into runtime proxy handlers.
- Do not reintroduce exact facades, context-window preflight filtering, or facade-level response-body model rewriting.
- Do not reuse text-generation hooks for token-count operations.
- Do not reintroduce OpenAI Chat/Responses sibling translation, provider fallbacks, or best-effort request rewrites.
- Do not bypass the telemetry outbox, feedback pipeline, or runtime side-effect manager with inline writes or sends.
- Do not duplicate upstream auth/header shaping, operation matching, or provider-path parsing in sibling packages.
- Do not run retention cleanup or partition maintenance outside `log_partitions.go` and `../../platform/logretention/`.
