# BACKEND RUNTIME TEST KNOWLEDGE BASE

## OVERVIEW

`backend/tests/runtime/` owns black-box and DB-backed runtime proxy regressions around operation-registered routes, rejected-route isolation, request logs, route planning, streaming, runtime cache invalidation, telemetry outboxes, runtime partitions, and priority-sensitive request behavior.

## STRUCTURE

```text
runtime/
├── runtime_database_harness.go         # PostgreSQL test database and harness lifecycle
├── runtime_runtime_setup.go            # Runtime service, cache, and HTTP server composition
├── runtime_database_lifecycle.go       # Docker/PostgreSQL lifecycle primitives
├── runtime_upstream_fakes.go           # Recorder, scripted, and blocking upstream fakes
├── runtime_domain_seeds.go             # Model, connection, and runtime-state seed setup
├── runtime_http_assertions.go          # HTTP, concurrency, and runtime-state assertions
├── proxy_key_record.go                 # Shared proxy-key record helper used by attribution tests
├── testdata/                           # Runtime fixtures
├── operation_route_matrix_test.go      # Supported method/path matrix and operation names
├── rejected_route_isolation_test.go    # Unsupported/wrong-method isolation before side effects
├── request_logs_list_detail_filter_contract_test.go # Request-log list, detail, and filter contracts
├── request_logs_runtime_attribution_identity_failover_contract_test.go # Runtime attribution, identity, and failover-row contracts
├── request_logs_pricing_unpriced_tier_contract_test.go # Pricing, unpriced, and tier-evidence contracts
├── request_logs_provider_stream_usage_contract_test.go # Provider stream and token-usage normalization contracts
├── request_logs_audit_diagnostics_contract_test.go # Audit and safe-diagnostic request-log contracts
├── request_logs_contract_harness_test.go # Request-log database harness and fixture ownership
├── pricing_peak_valley_runtime_test.go # Real runtime peak/offpeak requests, SQL evidence symmetry, and half-open boundary
├── proxy_forwarding_parity_test.go    # Runtime proxy forwarding and parity contracts
├── runtime_operation_request_body_test.go # Runtime operation request-body builders
├── proxy_selector_test.go              # Access-target selection and private ownership
├── proxy_admission_test.go             # Admission exhaustion and per-connection admission limits
├── proxy_failover_test.go              # Retry, failover, round-robin, and retry-window behavior
├── proxy_lease_test.go                 # Response-body lease and in-flight exclusivity
├── proxy_recovery_test.go              # Ban recovery, probe success, and restart persistence
├── runtime_streaming_buffering_test.go # Streaming buffering and terminal behavior
├── runtime_telemetry_outbox_test.go    # Durable telemetry outbox behavior
├── telemetry_outbox_poison_test.go     # Poison-row classification, quarantine, and backoff
├── proxy_key_attribution_failure_test.go # Proxy-key attribution derivation and ingestion-failure behavior
├── direct_request_entry_runtime_test.go # Non-entry ingress isolation and recursive parent routing
└── *_test.go                           # Focused runtime regressions
```

## WHERE TO LOOK

- Harness database/runtime setup: `runtime_database_harness.go`, `runtime_runtime_setup.go`, `runtime_database_lifecycle.go`
- Domain seed helpers, including selector route seeds: `runtime_domain_seeds.go`
- Runtime proxy forwarding and parity: `proxy_forwarding_parity_test.go`
- Runtime operation request bodies: `runtime_operation_request_body_test.go`
- Upstream test server plumbing: `runtime_upstream_fakes.go`
- HTTP, concurrency, and state assertions: `runtime_http_assertions.go`
- Operation route matrix and OpenAI native-compatibility coverage: `operation_route_matrix_test.go`, `operation_route_matrix_openai_compatibility_test.go`
- Rejected-route isolation before body reads, provider transport, telemetry, audit, feedback, or side effects: `rejected_route_isolation_test.go`
- Request-log list/detail/filter contracts: `request_logs_list_detail_filter_contract_test.go`
- Runtime attribution, identity, and failover-row contracts: `request_logs_runtime_attribution_identity_failover_contract_test.go`
- Pricing, unpriced, and tier-evidence contracts: `request_logs_pricing_unpriced_tier_contract_test.go`
- Provider stream and token-usage normalization contracts: `request_logs_provider_stream_usage_contract_test.go`
- Audit and safe-diagnostic request-log contracts: `request_logs_audit_diagnostics_contract_test.go`
- Request-log harness and fixture setup: `request_logs_contract_harness_test.go`
- Peak/valley runtime selection with frozen clock, IANA wall-clock evidence, and independent persisted-row checks: `pricing_peak_valley_runtime_test.go`
- Runtime pricing invariants also have process-local coverage for accepted planning time, half-open peak/valley boundaries, and snapshot digest/child-shape failure in `../../internal/httpapi/runtime/pricing_operation_matrix_test.go` and `runtime_pricing_test.go`.
- Runtime target selection and private ownership: `proxy_selector_test.go`
- Runtime admission exhaustion and limits: `proxy_admission_test.go`
- Runtime retry/failover and round-robin behavior: `proxy_failover_test.go`
- Runtime response-body lease behavior: `proxy_lease_test.go`
- Runtime recovery and restart persistence: `proxy_recovery_test.go`
- Runtime-created log partitions and helper parity: `runtime_partitioned_logs_test.go`, `log_partition_helpers_test.go`
- Cache invalidation and runtime snapshots: `runtime_cache_invalidation_test.go`, `runtime_phase1_snapshot_test.go`
- Direct-entry lookup versus all-node recursive routing and `/v1/models` filtering: `direct_request_entry_runtime_test.go`
- Streaming and telemetry durability: `runtime_streaming_buffering_test.go`, `runtime_telemetry_outbox_test.go`, `telemetry_outbox_poison_test.go`, `proxy_key_attribution_failure_test.go`

## CONVENTIONS

- Keep this suite aligned with `../../internal/httpapi/runtime/operations.go`; route additions require matrix and hook-residency coverage.
- Use the shared harness and seed helpers instead of hand-rolled database setup.
- Keep request-log assertions behavior-focused. Prefer golden or helper-normalized shapes for wide payloads. For pricing, assert `pricing_selection_state` and `pricing_card_role` separately and require price snapshots only on the priced selected-card path.
- Runtime unsupported-route tests are permanent absence guards because they protect the shipped allowlist contract.

## ANTI-PATTERNS

- Do not turn this suite into provider-unit tests; provider-native adapter units live under `internal/gateway/provider` and runtime hook units live under `internal/httpapi/runtime`.
- Do not duplicate one behavior across contract, integration, and runtime suites without a distinct black-box boundary.
- Do not run build, docker, or external process commands inside test functions.
