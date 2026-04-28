# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level Go regression surface. It holds contract, integration, and runtime checks around management APIs, proxy behavior, startup sequencing, bootstrap config, request-log contracts, and persistence semantics.

## COVERAGE CLUSTERS
- Contract coverage for auth, bootstrap config, config bundles, endpoints, models, profiles, vendors, and observability.
- Integration coverage for migrations, startup sequencing, canonical seeding, bootstrap config, and reconciliation paths.
- Runtime coverage for profile scoping, realtime delivery, request-log contracts, and related runtime persistence semantics.

## WHERE TO LOOK
- Contract packages and bootstrap-config schema or semantic coverage: `contract/`, `contract/bootstrap_config_contract_test.go`
- Integration packages for migrations, startup, and bootstrap-config seeding: `integration/migrations_test.go`, `integration/startup_test.go`, `integration/bootstrap_config_test.go`
- Runtime packages for profile scope, realtime delivery, and request-log contracts: `runtime/profile_scope_test.go`, `runtime/realtime_test.go`, `runtime/request_logs_contract_test.go`

## CONVENTIONS
- Keep this doc at the test-tree root, not the leaf level.
- Do not invent child test AGENTS files.
- Keep regression notes grounded in current Go package boundaries and live backend ownership docs.
