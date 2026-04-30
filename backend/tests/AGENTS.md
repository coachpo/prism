# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level Go regression surface. It holds contract, integration, runtime, and priority checks around management APIs, proxy behavior, startup sequencing, bootstrap config, request-log contracts, persistence semantics, DB lane isolation, and durable side-effect ownership.

## COVERAGE CLUSTERS
- Contract coverage for auth, bootstrap config, config bundles, endpoints, models, profiles, vendors, and observability.
- Integration coverage for migrations, startup sequencing, canonical seeding, bootstrap config, and reconciliation paths.
- Runtime coverage for profile scoping, realtime delivery, request-log contracts, and related runtime persistence semantics.
- Priority coverage for admission budgets, physical DB lane isolation, scheduler ownership, async side effects, outboxes, failure semantics, and no-inline-fallback regressions.

## WHERE TO LOOK
- Contract packages and bootstrap-config schema or semantic coverage: `contract/`, `contract/bootstrap_config_contract_test.go`
- Integration packages for migrations, startup, and bootstrap-config seeding: `integration/migrations_test.go`, `integration/startup_test.go`, `integration/bootstrap_config_test.go`
- Runtime packages for profile scope, realtime delivery, and request-log contracts: `runtime/profile_scope_test.go`, `runtime/realtime_test.go`, `runtime/request_logs_contract_test.go`
- Priority packages for concurrency and side-effect isolation: `priority/unit/`, `priority/db/`, `priority/integration/`, `priority/admission/`, `priority/scheduler/`, `priority/sideeffects/`, `priority/outbox/`, `priority/failure/`, `priority/cache/`, `priority/load/`, `priority/async/`, `priority/auditstats/`
- Request-log and audit contract fixtures: `../testdata/requests/`, `runtime/request_logs_contract_test.go`, `../internal/httpapi/proxykeyusage/record.go`
- Bootstrap and bundle fixtures: `../testdata/bootstrap/`, `../testdata/bundles/`

## CONVENTIONS
- Keep this doc at the test-tree root, not the leaf level.
- Do not invent child test AGENTS files.
- Keep regression notes grounded in current Go package boundaries and live backend ownership docs.
- Treat `tests/priority/` as the guardrail for no-inline side effects, scheduler-owned background work, and DB lane isolation.
- Keep bootstrap tests aligned with the plaintext v1 contract: required `runtime.transport.requestTimeout`, unsupported legacy encrypted files, metadata-only safe secret responses, and fail-fast enabled SMTP.

## ANTI-PATTERNS
- Do not bypass `tests/priority/` when changing admission, scheduler, outbox, DB pool, cache invalidation, or after-commit behavior.
- Do not collapse contract, integration, runtime, and priority package purposes into one generic test bucket.
