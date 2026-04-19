# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level Go regression surface. It holds contract, integration, and runtime checks around management APIs, proxy behavior, startup sequencing, and persistence semantics.

## COVERAGE CLUSTERS
- Contract coverage for auth, config bundles, endpoints, models, profiles, vendors, and observability.
- Integration coverage for migrations, startup sequencing, canonical seeding, and reconciliation paths.
- Runtime coverage for profile scoping, realtime delivery, and request-log contracts.

## WHERE TO LOOK
- Contract packages: `contract/`
- Integration packages: `integration/migrations_test.go`, `integration/startup_test.go`
- Runtime packages: `runtime/profile_scope_test.go`, `runtime/realtime_test.go`, `runtime/request_logs_contract_test.go`

## CONVENTIONS
- Keep this doc at the test-tree root, not the leaf level.
- Do not invent child test AGENTS files.
- Keep regression notes grounded in current Go package boundaries and live backend ownership docs.
