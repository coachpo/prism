# BACKEND RUNTIME TEST KNOWLEDGE BASE

## OVERVIEW

`backend/tests/runtime/` owns black-box and DB-backed runtime proxy regressions around operation-registered routes, rejected-route isolation, request logs, route planning, streaming, runtime cache invalidation, telemetry outboxes, runtime partitions, and priority-sensitive request behavior.

## STRUCTURE

```text
runtime/
├── runtime_harness.go                  # Shared runtime test harness and seed helpers
├── proxy_key_record.go                 # Shared proxy-key record helper used by attribution tests
├── testdata/                           # Runtime fixtures
├── operation_route_matrix_test.go      # Supported method/path matrix and operation names
├── rejected_route_isolation_test.go    # Unsupported/wrong-method isolation before side effects
├── request_logs_contract_test.go       # Request-log list/detail, generic pricing evidence, CSV, and filter contracts
├── pricing_peak_valley_runtime_test.go # Real runtime peak/offpeak requests, SQL evidence symmetry, and half-open boundary
├── proxy_selector_test.go              # Access-target planning, failover, Ban Policy, admission
├── runtime_streaming_buffering_test.go # Streaming buffering and terminal behavior
├── runtime_telemetry_outbox_test.go    # Durable telemetry outbox behavior
├── telemetry_outbox_poison_test.go     # Poison-row classification, quarantine, and backoff
├── proxy_key_attribution_failure_test.go # Proxy-key attribution derivation and ingestion-failure behavior
└── *_test.go                           # Focused runtime regressions
```

## WHERE TO LOOK

- Harness setup, seed helpers, and upstream test server plumbing: `runtime_harness.go`, `runtime_harness_test.go`
- Operation route matrix and OpenAI native-compatibility coverage: `operation_route_matrix_test.go`, `operation_route_matrix_openai_compatibility_test.go`
- Rejected-route isolation before body reads, provider transport, telemetry, audit, feedback, or side effects: `rejected_route_isolation_test.go`
- Request-log contracts, final-target filters, client-rule filters, grouped ingress rows, CSV, and typed kind/state/role pricing evidence: `request_logs_contract_test.go`
- Peak/valley runtime selection with frozen clock, IANA wall-clock evidence, and independent persisted-row checks: `pricing_peak_valley_runtime_test.go`
- Runtime pricing invariants also have process-local coverage for accepted planning time, half-open peak/valley boundaries, and snapshot digest/child-shape failure in `../../internal/httpapi/runtime/pricing_operation_matrix_test.go` and `runtime_test.go`.
- Runtime planning, failover, current-state mutation, recovery, and admission exhaustion: `proxy_selector_test.go`
- Runtime-created log partitions and helper parity: `runtime_partitioned_logs_test.go`, `log_partition_helpers_test.go`
- Cache invalidation and runtime snapshots: `runtime_cache_invalidation_test.go`, `runtime_phase1_snapshot_test.go`
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
