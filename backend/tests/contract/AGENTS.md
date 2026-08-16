# BACKEND CONTRACT TEST KNOWLEDGE BASE

## OVERVIEW
`backend/tests/contract/` owns management API and observability contract tests that exercise Prism through HTTP handlers and PostgreSQL state. It guards auth, endpoints, models, Terminal Target compatibility, request-log/audit API shapes, route-scope behavior, and management route contracts.

## STRUCTURE
```text
contract/
├── harness.go                         # Shared server/database contract harness
├── auth_control_plane_test.go         # Operator auth and proxy-key control plane
├── model_contract_test.go             # Model CRUD and access-target contracts
├── connection_contract_test.go        # Terminal Target compatibility connection contracts
├── endpoint_contract_test.go          # Endpoint CRUD and dependency behavior
├── s11_management_contract_test.go    # Broad management route and mutation contracts
├── s15_observability_contract_test.go # Request/audit/stat observability contracts
└── *_test.go
```

## WHERE TO LOOK
- Shared harness and JSON helpers: `harness.go`, `json_helpers_test.go`
- Auth, session, proxy-key, and runtime-key usage contracts: `auth_control_plane_test.go`
- Model, access-target, Terminal Target, and removed-field guards: `model_contract_test.go`, `connection_contract_test.go`, `connection_s10_contract_test.go`
- Endpoint ownership and dependency checks: `endpoint_contract_test.go`
- Management route and observability contract breadth: `s11_management_contract_test.go`, `s15_observability_contract_test.go`
- Partition helper contract coverage: `log_partition_helpers_test.go`

## CONVENTIONS
- Test public Prism contracts through HTTP responses and persisted state, not handler internals.
- Keep removed management fields guarded only when absence is itself a shipped contract.
- Share package-level database setup through the existing harness; do not add per-test process or container startup.
- Route-scope and cache-invalidation expectations are owned by `managementRouteSpecs` in `../../internal/platform/http/admission.go`; this directory no longer duplicates contract assertions for management routes.

## ANTI-PATTERNS
- Do not duplicate runtime proxy route-matrix coverage from `../runtime/`.
- Do not assert frontend copy, table text, or internal SQL implementation details here.
- Do not add broad INSERT-then-SELECT mirror tests when one API contract assertion is enough.
