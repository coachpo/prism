# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level Go regression surface. It holds contract, integration, runtime, and priority checks around management APIs, operation-registered proxy behavior, startup sequencing, bootstrap file loading, launcher behavior, request-log contracts, partitioned log retention, container ownership, persistence semantics, DB lane isolation, and durable side-effect ownership.

## CHILD DOCS
- `contract/AGENTS.md`: management API, auth, model, endpoint, Terminal Target, and observability contracts.
- `integration/AGENTS.md`: startup, migrations, launcher, Dockerfile, retention, and cross-service integration checks.
- `runtime/AGENTS.md`: operation-registered proxy, request-log, streaming, cache, and telemetry runtime regressions.

## COVERAGE CLUSTERS
- Contract coverage for auth, endpoints, models, observability, and partitioned-log helper contracts.
- Integration coverage for migrations, startup sequencing, launcher/bootstrap preservation, canonical seeding, partitioned log retention, runtime route-matrix forwarding, audit or stats persistence, and Dockerfile ownership.
- Runtime coverage for operation route matrices, rejected-route isolation, hook residency, profile scoping, request-log contracts, request-generation params, runtime-created log partitions, published runtime snapshots, cache invalidation, telemetry outboxes, streaming buffering, responses parity, and `operation_name` persistence.
- Priority coverage for admission budgets, physical DB lane isolation, scheduler ownership, async side effects, outboxes, failure semantics, and no-inline-fallback regressions.
- CI runs `go test ./internal/... ./cmd/...`, `go test ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...`, and `go build ./cmd/prism-backend`.

## WHERE TO LOOK
- Contract packages for observability or partition-helper coverage: `contract/`, `contract/log_partition_helpers_test.go`
- Runtime packages for route matrices, rejected-route isolation, request-generation params, request-log contracts, runtime partition creation, cache invalidation, runtime snapshots, telemetry outboxes, streaming buffering, and responses parity: `runtime/operation_route_matrix_test.go`, `runtime/rejected_route_isolation_test.go`, `runtime/request_generation_params_contract_test.go`, `runtime/request_logs_contract_test.go`, `runtime/proxy_selector_test.go`, `runtime/runtime_partitioned_logs_test.go`, `runtime/runtime_cache_invalidation_test.go`, `runtime/runtime_phase1_snapshot_test.go`, `runtime/runtime_telemetry_outbox_test.go`, `runtime/runtime_streaming_buffering_test.go`, `runtime/responses_parity_test.go`
- Internal runtime suites for hook residency, ingress rejection, cache, response parsing, and generation behavior: `../internal/httpapi/runtime/operations_test.go`, `../internal/httpapi/runtime/service_ingress_test.go`, `../internal/httpapi/runtime/request_generation_params_test.go`, `../internal/httpapi/runtime/operation_hook_residency_test.go`, `../internal/httpapi/runtime/operation_response_hooks_test.go`, `../internal/httpapi/runtime/generations_test.go`, `../internal/httpapi/runtime/cache_test.go`
- Priority packages for concurrency and side-effect isolation, usually run with `go test ./tests/priority/...`: root `priority/*.go` plus `priority/admission/`, `priority/db/`, `priority/load/`, `priority/sideeffects/`, and `priority/unit/`.
- Request-log and audit contract fixtures: `../testdata/requests/`, `runtime/request_logs_contract_test.go`, `../internal/httpapi/proxykeyusage/record.go`
- Bootstrap fixtures: `../testdata/bootstrap/`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this doc at the test-tree root, not the leaf level.
- Keep child test AGENTS files focused on package-level suite boundaries; do not add smaller child docs without a distinct runner or ownership split.
- Keep regression notes grounded in current Go package boundaries and live backend ownership docs.
- Treat `tests/priority/` as the guardrail for no-inline side effects, scheduler-owned background work, and DB lane isolation.
- Keep partitioned log tests aligned with `internal/platform/logretention/`, runtime partition ensuring, and the baseline migration `000001_initial_schema.sql`.
- Keep hard-delete guardrail tests aligned with model CRUD validation, runtime request-log final-target attribution, and the live baseline migration.
- Keep Dockerfile contract tests aligned with non-root `prism:prism` execution, `/app/config` ownership, and `/app/config/config.json` defaults.
- Keep bootstrap tests aligned with the plaintext v1 contract: required `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout`, unsupported legacy encrypted files, restart-required external edits, preserved existing valid files, and parse-only mail config compatibility.
- Keep runtime contract tests aligned with `internal/httpapi/runtime/operations.go`, hook residency, rejected-route isolation, streaming or non-streaming parity, and persisted `operation_name` fields.
- When changing runtime upstream request/response logic, run both package-local runtime hook tests under `internal/httpapi/runtime` and the external `tests/runtime` suite.
- Keep test ownership single-layer: process-local backend unit tests own pricing, planning, and stream classification without DB; DB contract suites own one API surface; frontend Vitest/lib owns pure frontend logic. Do not duplicate one behavior across layers or add INSERT-then-SELECT mirror tests.
- Respect the frontend browser budget when backend changes need browser coverage: Playwright stays near five journey specs, adding one deletes one, and browser tests must not assert table-cell text or i18n fallback behavior.
- Keep setup before the first act within 10 lines; use defaulted builders when it grows. Share Postgres per package through `TestMain` and template-DB cloning, and never run `docker run`, `go build`, or `go run` inside test functions.
- Use baseline-plus-override helpers or golden files when expectations exceed eight fields. Use golden files for large shapes, inline assertions only for behavior the test cares about, and normalized `pg_dump` diffs instead of per-column DDL assertions.
- Table-drive three or more cases that share the same act/assert shape with `t.Run`, and keep at most one narrative story test per resource.
- Test Prism behavior, not platform internals or dependency output: do not grep production Go or TS source text, test Postgres internals, assert chart/graph library rendering, or manually recalculate aggregation internals.
- Let proves-not tests expire with removal PRs. Permanent exceptions are route-contract parity guards, because they own route-level absence, and Dockerfile contract tests, because they guard the shipped container contract.
- Tests that remain must run in CI or document why they do not. Wire commands by glob, including `go test ./internal/... ./cmd/...` and the top-level `tests/...` suites, not hand-written file lists.
- Prevent rebound: feature PR test line additions must not exceed product line additions, no plan-numbered test names, and a new shared helper must delete the copy/paste it replaces in the same PR.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not bypass `tests/priority/` when changing admission, scheduler, outbox, DB pool, cache invalidation, or after-commit behavior.
- Do not skip operation route matrix, rejected-route isolation, streaming or parity, or `operation_name` persistence tests when changing runtime contract files.
- Do not skip partitioned-log, launcher-startup, or Dockerfile contract tests when changing retention migrations, bootstrap seeding, `Dockerfile`, or container startup paths.
- Do not collapse contract, integration, runtime, and priority package purposes into one generic test bucket.
