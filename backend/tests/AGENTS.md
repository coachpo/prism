# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level Go regression surface. It holds contract, integration, runtime, and priority checks around management APIs, proxy behavior, startup sequencing, bootstrap config, request-log contracts, partitioned log retention, container ownership, persistence semantics, DB lane isolation, and durable side-effect ownership.

## COVERAGE CLUSTERS
- Contract coverage for auth, bootstrap config, config bundles, endpoints, models, profiles, vendors, observability, sidecars, and partitioned-log helper contracts.
- Integration coverage for migrations, startup sequencing, canonical seeding, bootstrap config, sidecar sync/mutation persistence, partitioned log retention, Dockerfile ownership, and reconciliation paths.
- Runtime coverage for profile scoping, realtime delivery, request-log contracts, runtime-created log partitions, published runtime snapshots, cache-generation invalidation, telemetry outboxes, and related runtime persistence semantics.
- Priority coverage for admission budgets, physical DB lane isolation, scheduler ownership, async side effects, outboxes, failure semantics, and no-inline-fallback regressions.

## WHERE TO LOOK
- Contract packages and bootstrap-config schema, observability, or partition-helper coverage: `contract/`, `contract/bootstrap_config_contract_test.go`, `contract/log_partition_helpers_test.go`
- Integration packages for migrations, startup, bootstrap-config seeding, sidecar sync/mutation persistence, partitioned log retention, and Dockerfile ownership: `integration/migrations_test.go`, `integration/startup_test.go`, `integration/bootstrap_config_test.go`, `integration/sidecars_integration_test.go`, `integration/partitioned_log_retention_test.go`, `integration/dockerfile_contract_test.go`
- Runtime packages for profile scope, realtime delivery, request-log contracts, runtime partition creation, cache invalidation, runtime snapshots, and telemetry outboxes: `runtime/profile_scope_test.go`, `runtime/realtime_test.go`, `runtime/request_logs_contract_test.go`, `runtime/runtime_partitioned_logs_test.go`, `runtime/runtime_cache_invalidation_test.go`, `runtime/runtime_phase1_snapshot_test.go`, `runtime/runtime_telemetry_outbox_test.go`
- Sidecar contract and priority coverage: `contract/sidecars_contract_test.go`, `integration/sidecars_integration_test.go`, `priority/sidecar_worker_priority_test.go`
- Priority packages for concurrency and side-effect isolation, usually run with `go test ./tests/priority/...`: `priority/unit/`, `priority/db/`, `priority/integration/`, `priority/admission/`, `priority/scheduler/`, `priority/sideeffects/`, `priority/outbox/`, `priority/failure/`, `priority/cache/`, `priority/load/`, `priority/async/`, `priority/auditstats/`
- Request-log and audit contract fixtures: `../testdata/requests/`, `runtime/request_logs_contract_test.go`, `../internal/httpapi/proxykeyusage/record.go`
- Bootstrap and bundle fixtures: `../testdata/bootstrap/`, `../testdata/bundles/`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- Keep this doc at the test-tree root, not the leaf level.
- Do not invent child test AGENTS files.
- Keep regression notes grounded in current Go package boundaries and live backend ownership docs.
- Treat `tests/priority/` as the guardrail for no-inline side effects, scheduler-owned background work, and DB lane isolation.
- Keep partitioned log tests aligned with `internal/platform/logretention/`, runtime partition ensuring, and migration `000013_partitioned_log_retention.sql`.
- Keep Dockerfile contract tests aligned with non-root `prism:prism` execution, `/etc/prism` ownership, and `/etc/prism/config.json` defaults.
- Keep bootstrap tests aligned with the plaintext v1 contract: required `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout`, unsupported legacy encrypted files, metadata-only safe secret responses, and fail-fast enabled SMTP.

## ANTI-PATTERNS
- Do not bypass `tests/priority/` when changing admission, scheduler, outbox, DB pool, cache invalidation, or after-commit behavior.
- Do not skip partitioned-log or Dockerfile contract tests when changing retention migrations, log writers, `Dockerfile`, or container startup paths.
- Do not collapse contract, integration, runtime, and priority package purposes into one generic test bucket.
