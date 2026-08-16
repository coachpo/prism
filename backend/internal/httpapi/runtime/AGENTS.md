# BACKEND RUNTIME HTTPAPI KNOWLEDGE BASE

## OVERVIEW
`runtime/` owns Prism's operation-registered runtime proxy contract behind the mounted `/v1` and `/v1beta` prefixes. It resolves exact supported operations at ingress, carries operation metadata through request planning, resolves requested models by exact `planningSnapshot.ModelsByID` lookup, routes provider-native differences through hook collections, persists `operation_name`, and keeps shared execution, durable request-history telemetry, feedback, side effects, and partition ensuring inside one backend-owned runtime surface.

## STRUCTURE
```text
runtime/
├── operations.go                # Exact supported runtime operations, method/path matching, hook collection ids
├── operation_capability_gate.go # OpenAI text and image capability gates for ingress operations
├── operation_image_audit.go     # Image audit-body redaction dispatch before persistence
├── service.go                   # Ingress rejection, shared executor wiring, transport, workers
├── runtime.go                   # Request planning, model binding, rewrite rules, upstream proxy flow
├── runtime_planner.go           # Final plan assembly
├── routing_plan*.go             # Routing plan compilation and validation helpers
├── planning_snapshot.go         # Access-target snapshot assembly and resolution ordering helpers
├── planning_snapshot_legacy.go  # Legacy snapshot compatibility helpers
├── planning_classification.go   # Failed OpenAI text resolution → the three stable planning rejection codes
├── planning_terminal_target_adapter.go # Terminal Target record scanning and runtime-connection projection
├── proxy_selector_helpers.go    # Access-target ordering helpers used by request planning
├── cache.go                     # Shared runtime cache reads and snapshots
├── runtime_context.go           # Runtime trace-context propagation helpers
├── ingress_request_id.go        # Server-generated ingress correlation ID middleware + response-writer guard
├── request_generation_params.go # Internal generation-param extraction orchestration
├── *_adapter_bridge.go          # Runtime-to-gateway provider adapter bridge files
├── gateway_*_bridge.go          # Gateway core and typed-hook bridge files
├── provider_usage_conversion.go # Provider usage normalization bridge
├── operation_request_hooks.go   # Request hook registry and streaming-intent selection
├── operation_response_hooks.go  # Non-stream response parsing by operation
├── operation_stream_hooks.go    # SSE terminal and usage parsing by operation
├── operation_translation.go     # OpenAI native operation-set compatibility and rejection boundary
├── openai_models.go             # Local OpenAI model-list filtering and response
├── generations.go               # Generation-request shaping and upstream helpers
├── observability.go             # Request-log, usage-event, and audit shaping with v2 scoped/failure fields
├── attempt_lifecycle.go         # Attempt triggers/results, launch-ordinal tracking, 64-attempt cap, failed-response sampler, safe transport/stream diagnostics
├── bounded_audit_capture.go     # 4 MiB per-body / 12+4 MiB per-ingress bounded audit capture
├── telemetry_outbox_v2.go       # v2 metadata/artifact split, provisional→finalized streaming state machine
├── log_partitions.go            # Runtime partition ensuring and cache
├── telemetry_outbox.go          # Durable telemetry enqueue and publisher wakeups
├── feedback_pipeline.go         # Runtime feedback persistence and worker handoff
├── runtime_side_effects.go      # Runtime side-effect manager and shutdown behavior
├── runtime_pricing.go           # Runtime pricing snapshots and usage pricing helpers
└── *_test.go                    # Route matrix, hook residency, planning, and ingress regressions
```

## WHERE TO LOOK
- Exact supported operations, hook collection ids, streaming flags, and model-binding sources: `operations.go`
- Ingress rejection before body reads, wrong-method handling, shared executor wiring, and response branching: `service.go`
- Request planning, model binding, request path rewrites, unified access-target resolution, final-target attribution, exact `planningSnapshot.ModelsByID` requested-model lookup, and shared upstream execution: `runtime.go`, `runtime_planner.go`, `routing_plan*.go`, `generations.go`, `planning_snapshot.go`, `planning_snapshot_legacy.go`, `proxy_selector_helpers.go`
- Runtime-to-gateway adapter seams and usage normalization: `*_adapter_bridge.go`, `gateway_core_bridge.go`, `gateway_typed_hooks_bridge.go`, `provider_usage_conversion.go`
- Automatic generation-param extraction and operation-directed request hooks: `request_generation_params.go`, `operation_request_hooks.go`
- Non-stream response parsing for text generation and token count operations: `operation_response_hooks.go`
- SSE terminal classification and usage merging for OpenAI, Anthropic, and Gemini stream operations: `operation_stream_hooks.go`
- OpenAI native operation-set coverage, mismatched-target skipping, unsupported-wire rejection behavior, and planning diagnostics: `operation_translation.go`, `planning_snapshot.go`, `routing_plan*.go`, `runtime_test.go`
- Local OpenAI model-list response: `openai_models.go`
- Request-log and usage-event shaping plus `operation_name` persistence and v2 scoped/failure fields: `observability.go`, `attempt_lifecycle.go`, `../../../migrations/000001_initial_schema.sql`, `../../../migrations/000008_pricing_cost_trust_additive.sql`, `../../../migrations/000010_request_logs_audit_observability.sql`
- Telemetry, feedback, and runtime side-effect ownership: `telemetry_outbox.go`, `telemetry_outbox_v2.go`, `feedback_pipeline.go`, `runtime_side_effects.go`
- Safe failure diagnostics bottom line: `../../domain/safediag/` (scrub/extract/codes/metadata/limits)
- Partition ensuring and partition-cache behavior: `log_partitions.go`, `../../platform/logretention/`
- Internal runtime regression coverage: `operations_test.go`, `service_ingress_test.go`, `request_generation_params_test.go`, `request_generation_params_runtime_test.go`, `operation_hook_residency_test.go`, `operation_response_hooks_test.go`, `operation_response_overflow_classifier_test.go`, `gateway_typed_hooks_bridge_test.go`, `planning_snapshot_contract_test.go`, `routing_plan_test.go`, `runtime_test.go`
- Route-matrix, native compatibility, streaming, body-limit, and rejected-route coverage: `../../../tests/runtime/body_limits_test.go`, `../../../tests/runtime/operation_route_matrix_test.go`, `../../../tests/runtime/operation_route_matrix_openai_compatibility_test.go`, `../../../tests/runtime/runtime_streaming_buffering_test.go`, `../../../tests/runtime/rejected_route_isolation_test.go`, `../../../tests/runtime/request_generation_params_contract_test.go`, `../../../tests/integration/runtime_route_matrix_test.go`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `operations.go` as the single source of truth for supported runtime method/path pairs, hook collection ids, streaming flags, and model-binding sources.
- Keep management scope out of proxy traffic. Runtime request planning uses the current runtime snapshot, not `X-Profile-Id` management headers.
- Keep requested-model resolution exact. Runtime planning starts from `planningSnapshot.ModelsByID` using the client-supplied model ID exactly, then evaluates one mixed peer sequence in which Model Target and Terminal Target rows share authored `(position, id)` ordering — there is no model-first tier and no terminal fallback tier (see `docs/architecture.md`). Do not add regex matching or capability-metadata expansion in this package.
- Keep the three strategies (`single`, `fill-first`, `round-robin`) applied once to that mixed peer sequence; a Model Target row recursively resolves through the child model's own strategy and stays one contiguous block. Reordering, add, remove, or enable-set changes must change the round-robin target-set hash.
- Keep the `custom_request_parameters` overlay on the per-attempt materialized body: the shared `domain/terminaltarget` value applies a top-level shallow overlay after provider-native model/path rewrite, per-attempt generation-parameter snapshots are extracted from each attempt's final body, and any configured candidate forces the replayable-body path (Gemini probe planning stays two-phase: `rawBody == nil` never overlays or 400s).
- Keep unsupported or wrong-method requests rejecting before body reads, runtime admission, provider transport, telemetry, audit, feedback, or runtime side effects.
- Keep the shared execution core in `service.go` and `runtime.go`; provider-native differences belong in request, response, or stream hooks instead of forked executors.
- Keep retired exact-facade and context-fit preflight behavior out of runtime planning; preserve requested/resolved model observability through the ordinary target plan.
- Keep token-count operations out of generation-only parsing and usage assumptions.
- Keep OpenAI text routing native-only: the model's accepted operation set and the connection capability must intersect for an ingress operation, otherwise planning skips or rejects the attempt; never translate Chat Completions and Responses.
- Keep the OpenAI image dimension independent of the text dimension: image operations resolve against `openai_image_operations`/`openai_image_capability` with containment (target ⊇ model), text operations keep strict mode equality, and neither gate may answer for the other. `operation_capability_gate.go` is the single seam for both.
- Keep `GET /v1/models` local. It returns the OpenAI `object`/`data` list for enabled OpenAI models; query parameters do not select an alternate response shape.
- Keep telemetry, feedback, and runtime side-effect work on durable outboxes or worker seams instead of the hot request path.
- Keep failure diagnostics safe and bounded: persist scrubbed `error_detail`/`stream_error_detail` (4 KiB cap via `safediag`) with typed source/code/stage and lifecycle facts; launch ordinals and triggers freeze at launch site; the 64-launch cap is a gateway terminal code, never a data loss.
- Keep the v2 outbox identity contract: one metadata row per `{profile_id, ingress_request_id}` with idempotent enqueue retry, artifact rows keyed `{profile_id, ingress_request_id, component_key, artifact_kind}` with `ON CONFLICT` convergence, and metadata+artifacts ACKed together.
- Keep runtime partition ensuring here plus `../../platform/logretention/`; handlers must not create or drop partitions ad hoc.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`), Responses (`/v1/responses`), and image generations/edits (`/v1/images/generations`, `/v1/images/edits`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not describe mounted `/v1` and `/v1beta` prefixes as broad passthrough support.
- Do not add generic OpenAI or vendor fallback behavior outside the allowlist in `operations.go`.
- Do not inject management-only `X-Profile-Id` logic or auth-session state into runtime proxy handlers.
- Do not reintroduce exact facades, context-window preflight filtering, or facade-level response-body model rewriting.
- Do not reuse text-generation hooks for token-count operations.
- Do not reintroduce OpenAI Chat/Responses sibling translation, provider fallbacks, or best-effort request rewrites.
- Do not bypass the `custom_request_parameters` fail-closed boundaries: non-object ingress, non-identity `Content-Encoding` on configured Gemini path-bound candidates, and merged bodies over the 20 MiB limit must fail before admission/transport; invalid persisted configuration must reject the snapshot generation instead of being normalized to unconfigured.
- Do not bypass the telemetry outbox, feedback pipeline, or runtime side-effect manager with inline writes or sends.
- Do not duplicate upstream auth/header shaping, operation matching, or provider-path parsing in sibling packages.
- Do not run retention cleanup or partition maintenance outside `log_partitions.go` and `../../platform/logretention/`.
