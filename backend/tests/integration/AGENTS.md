# BACKEND INTEGRATION TEST KNOWLEDGE BASE

## OVERVIEW
`backend/tests/integration/` owns startup, migration, launcher, Dockerfile, partitioned-retention, runtime route-matrix, alerting outbox, and cross-service integration checks. These tests verify process and persistence contracts that are broader than one handler or package.

## STRUCTURE
```text
integration/
├── harness.go                              # Shared integration database/process helpers
├── startup_test.go                         # Startup config, seeding, bootstrap preservation
├── launcher_startup_contract_test.go       # Root launcher and local bootstrap contract
├── migrations_test.go                      # Fresh-install baseline and schema-history guards
├── dockerfile_contract_test.go             # Backend image ownership/path contract
├── partitioned_log_retention_test.go       # Partitioned runtime/audit/usage/loadbalance retention
├── alerting_outbox_test.go                 # Webhook outbox persistence contract
├── runtime_route_matrix_test.go            # Integration route matrix smoke
└── *_test.go
```

## WHERE TO LOOK
- Startup bootstrap, canonical defaults, and parse-compatible retired fields: `startup_test.go`
- Launcher-local config, PostgreSQL host port, and preservation behavior: `launcher_startup_contract_test.go`
- Migration baseline, schema history, and normalized dumps: `migrations_test.go`, `testdata/migrations/schema.sql`
- Container ownership and backend bootstrap path: `dockerfile_contract_test.go`
- Partitioned log retention and platform store behavior: `partitioned_log_retention_test.go`, `logretention_store_test.go`
- Alert webhook outbox durability: `alerting_outbox_test.go`
- Runtime route matrix integration coverage: `runtime_route_matrix_test.go`

## CONVENTIONS
- Use these tests for cross-cutting process and persistence contracts that unit or contract suites cannot cover cleanly.
- Keep Dockerfile tests aligned with non-root `prism:prism` UID/GID `1000:1000` and `/app/config/config.json` defaults.
- Keep startup tests aligned with plaintext bootstrap v1, CPU-derived pool defaults, parse-only mail/telemetry compatibility, and restart-required external edits.
- Keep partitioned-retention tests scoped to `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.

## ANTI-PATTERNS
- Do not move narrow handler assertions here when `../contract/` owns the API surface.
- Do not assert PostgreSQL implementation minutiae unless the migration/schema contract is the product behavior under test.
- Do not add test-local migrations or schema rewrites outside the checked-in migration runner path.
