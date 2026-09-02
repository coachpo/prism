# BACKEND CONTRACT TEST KNOWLEDGE BASE

## OVERVIEW
`backend/tests/contract/` owns management API and observability contract tests that exercise Prism through HTTP handlers and PostgreSQL state. It guards auth, endpoints, models, Terminal Target compatibility, request-log/audit API shapes, route-scope behavior, and management route contracts.

## STRUCTURE
```text
contract/
├── contract_database_harness.go       # Shared PostgreSQL and Docker lifecycle
├── contract_server_harness.go          # HTTP server and dependency composition
├── contract_auth_setup.go              # Auth setup and login mutation helpers
├── contract_persistence_assertions.go # Persistence snapshots and scan assertions
├── contract_http_assertions.go         # HTTP response and envelope assertions
├── auth_control_plane_test.go         # Operator auth and proxy-key control plane
├── model_contract_test.go             # Model CRUD and access-target contracts
├── direct_request_entry_contract_test.go # Direct-entry field, warning, and Model Target projection contracts
├── connection_contract_test.go        # Terminal Target compatibility connection contracts
├── endpoint_contract_test.go          # Endpoint CRUD and dependency behavior
├── s11_costing_timezone_contract_test.go # Costing and timezone contracts
├── s11_audit_settings_contract_test.go # Audit settings contracts
├── s11_retention_contract_test.go      # Retention policy and job contracts
├── s11_loadbalance_contract_test.go    # Loadbalance strategy contracts
├── s11_config_rules_contract_test.go   # Config-rule contracts
├── s11_currency_migration_contract_test.go # Currency migration contracts
├── s15_stats_aggregates_contract_test.go # Stats aggregate and drill-down contracts
├── s15_audit_retention_contract_test.go # Audit and retention contracts
├── s15_loadbalance_contract_test.go    # Loadbalance observability contracts
├── s15_request_log_chain_contract_test.go # Request-log and chain contracts
├── s15_observability_harness_test.go   # Shared S15 server, seed, and fixture harness
└── *_test.go
```

## WHERE TO LOOK
- Shared database/server harness: `contract_database_harness.go`, `contract_server_harness.go`
- Auth setup: `contract_auth_setup.go`
- Persistence snapshots and scan assertions: `contract_persistence_assertions.go`
- HTTP response and envelope assertions: `contract_http_assertions.go`, `json_helpers_test.go`
- Auth, session, proxy-key, and runtime-key usage contracts: `auth_control_plane_test.go`
- Model, direct-entry, access-target, Terminal Target, and removed-field guards: `model_contract_test.go`, `direct_request_entry_contract_test.go`, `connection_contract_test.go`, `connection_s10_contract_test.go`
- Endpoint ownership and dependency checks: `endpoint_contract_test.go`
- S11 management resource contracts: `s11_costing_timezone_contract_test.go`, `s11_audit_settings_contract_test.go`, `s11_retention_contract_test.go`, `s11_loadbalance_contract_test.go`, `s11_config_rules_contract_test.go`, `s11_currency_migration_contract_test.go`
- S15 observability contracts: `s15_stats_aggregates_contract_test.go`, `s15_audit_retention_contract_test.go`, `s15_loadbalance_contract_test.go`, `s15_request_log_chain_contract_test.go`, `s15_observability_harness_test.go`
- Partition helper contract coverage: `log_partition_helpers_test.go`

## CONVENTIONS
- Test public Prism contracts through HTTP responses and persisted state, not handler internals.
- Keep removed management fields guarded only when absence is itself a shipped contract.
- Share package-level database setup through the existing harness; do not add per-test process or container startup.
- Route-scope and cache-invalidation expectations are owned by `managementRouteSpecs` in `../../internal/platform/http/management_route_specs.go`; this directory no longer duplicates contract assertions for management routes.

## ANTI-PATTERNS
- Do not duplicate runtime proxy route-matrix coverage from `../runtime/`.
- Do not assert frontend copy, table text, or internal SQL implementation details here.
- Do not add broad INSERT-then-SELECT mirror tests when one API contract assertion is enough.
