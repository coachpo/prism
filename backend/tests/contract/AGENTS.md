# Management Contract Tests

Test public management behavior through HTTP responses and persisted state.

- Reuse `contract_database_harness.go` and `contract_server_harness.go` for database/server ownership, `contract_auth_setup.go` for authentication, and the shared HTTP/persistence assertion helpers.
- Put model/Terminal Target authoring regressions beside `model_contract_test.go` and `connection_contract_test.go`; direct-entry field/projection contracts belong in `direct_request_entry_contract_test.go`.
- Management route tier, profile scope, and invalidation contract generation belongs to `../../internal/platform/http/server_route_contract_test.go` from `managementRouteSpecs`. Do not create a second route registry assertion here.
- Do not replace behavior checks with handler-internal SQL assertions.
