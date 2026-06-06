# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level Go regression surface. It holds contract, integration, runtime, and priority checks around management APIs, operation-registered proxy behavior, startup sequencing, bootstrap config, launcher behavior, request-log contracts, context overflow promotion, partitioned log retention, container ownership, persistence semantics, DB lane isolation, and durable side-effect ownership.

## COVERAGE CLUSTERS
- Contract coverage for auth, bootstrap config, config bundles, endpoints, models, profiles, vendors, observability, sidecars, and partitioned-log helper contracts.
- Integration coverage for migrations, startup sequencing, launcher/bootstrap preservation, canonical seeding, sidecar sync or mutation persistence, partitioned log retention, runtime route-matrix forwarding, audit or stats persistence, and Dockerfile ownership.
- Runtime coverage for operation route matrices, rejected-route isolation, hook residency, profile scoping, realtime delivery, request-log contracts, request-generation params, context overflow promotion, runtime-created log partitions, published runtime snapshots, cache invalidation, telemetry outboxes, streaming buffering, responses parity, and `operation_name` persistence.
- Priority coverage for admission budgets, physical DB lane isolation, scheduler ownership, async side effects, outboxes, failure semantics, and no-inline-fallback regressions.

## WHERE TO LOOK
- Contract packages and bootstrap-config schema, observability, or partition-helper coverage: `contract/`, `contract/bootstrap_config_contract_test.go`, `contract/log_partition_helpers_test.go`
- Integration packages for migrations, startup, launcher/bootstrap preservation, sidecar sync or mutation persistence, partitioned log retention, audit or stats persistence, runtime route matrix, and Dockerfile ownership: `integration/migrations_test.go`, `integration/startup_test.go`, `integration/launcher_startup_contract_test.go`, `integration/bootstrap_config_test.go`, `integration/management_audit_stats_phase7_test.go`, `integration/runtime_route_matrix_test.go`, `integration/sidecars_integration_test.go`, `integration/partitioned_log_retention_test.go`, `integration/dockerfile_contract_test.go`
- Runtime packages for route matrices, rejected-route isolation, request-generation params, request-log contracts, context overflow promotion, realtime delivery, runtime partition creation, cache invalidation, runtime snapshots, telemetry outboxes, streaming buffering, responses parity, and phase-4 health checks: `runtime/operation_route_matrix_test.go`, `runtime/rejected_route_isolation_test.go`, `runtime/request_generation_params_contract_test.go`, `runtime/request_logs_contract_test.go`, `runtime/context_overflow_promotion_test.go`, `runtime/proxy_selector_test.go`, `runtime/realtime_test.go`, `runtime/runtime_partitioned_logs_test.go`, `runtime/runtime_cache_invalidation_test.go`, `runtime/runtime_phase1_snapshot_test.go`, `runtime/runtime_telemetry_outbox_test.go`, `runtime/runtime_streaming_buffering_test.go`, `runtime/responses_parity_test.go`, `runtime/runtime_phase4_health_check_test.go`
- Internal runtime suites for hook residency, ingress rejection, observability, tracing, metrics, cache, overflow classification, and generation behavior: `../internal/httpapi/runtime/operations_test.go`, `../internal/httpapi/runtime/service_ingress_test.go`, `../internal/httpapi/runtime/request_generation_params_test.go`, `../internal/httpapi/runtime/operation_hook_residency_test.go`, `../internal/httpapi/runtime/operation_media_hooks_test.go`, `../internal/httpapi/runtime/operation_response_hooks_test.go`, `../internal/httpapi/runtime/operation_response_overflow_classifier_test.go`, `../internal/httpapi/runtime/observability_test.go`, `../internal/httpapi/runtime/runtime_metrics_test.go`, `../internal/httpapi/runtime/runtime_tracing_test.go`, `../internal/httpapi/runtime/generations_test.go`, `../internal/httpapi/runtime/cache_test.go`
- Sidecar contract and priority coverage: `contract/sidecars_contract_test.go`, `integration/sidecars_integration_test.go`, `priority/sidecar_worker_priority_test.go`
- Priority packages for concurrency and side-effect isolation, usually run with `go test ./tests/priority/...`: `priority/unit/`, `priority/db/`, `priority/integration/`, `priority/admission/`, `priority/scheduler/`, `priority/sideeffects/`, `priority/outbox/`, `priority/failure/`, `priority/cache/`, `priority/load/`, `priority/async/`, `priority/auditstats/`
- Request-log and audit contract fixtures: `../testdata/requests/`, `runtime/request_logs_contract_test.go`, `../internal/httpapi/proxykeyusage/record.go`
- Bootstrap and bundle fixtures: `../testdata/bootstrap/`, `../testdata/bundles/`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this doc at the test-tree root, not the leaf level.
- Do not invent child test AGENTS files.
- Keep regression notes grounded in current Go package boundaries and live backend ownership docs.
- Treat `tests/priority/` as the guardrail for no-inline side effects, scheduler-owned background work, and DB lane isolation.
- Keep partitioned log tests aligned with `internal/platform/logretention/`, runtime partition ensuring, and the baseline migration `000001_initial_schema.sql`.
- Keep context overflow promotion tests aligned with model CRUD validation, config-bundle import/export validation, runtime replay decisions, request-log context routing, and migration `000002_context_overflow_promotion_target.sql`.
- Keep Dockerfile contract tests aligned with non-root `prism:prism` execution, `/app/config` ownership, and `/app/config/config.json` defaults.
- Keep bootstrap tests aligned with the plaintext v1 contract: required `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout`, unsupported legacy encrypted files, metadata-only safe secret responses, apply-capability reporting, failed-hot-apply surfaces, and fail-fast enabled SMTP.
- Keep runtime contract tests aligned with `internal/httpapi/runtime/operations.go`, hook residency, rejected-route isolation, streaming or non-streaming parity, and persisted `operation_name` fields.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not bypass `tests/priority/` when changing admission, scheduler, outbox, DB pool, cache invalidation, or after-commit behavior.
- Do not skip operation route matrix, rejected-route isolation, streaming or parity, or `operation_name` persistence tests when changing runtime contract files.
- Do not skip partitioned-log, launcher-startup, or Dockerfile contract tests when changing retention migrations, bootstrap seeding, `Dockerfile`, or container startup paths.
- Do not collapse contract, integration, runtime, and priority package purposes into one generic test bucket.
