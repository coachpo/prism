# Runtime Regressions

This suite exercises the public proxy/retained-state boundary. Provider adapter units belong in `../../internal/gateway/provider`; runtime hook/planning units belong in `../../internal/httpapi/runtime`.

- Reuse `runtime_database_harness.go`, `runtime_database_lifecycle.go`, `runtime_runtime_setup.go`, and `runtime_domain_seeds.go`. Use `runtime_upstream_fakes.go` and `runtime_http_assertions.go` for upstream coordination and concurrency assertions.
- `operation_route_matrix_test.go`, OpenAI compatibility matrix tests, and `rejected_route_isolation_test.go` follow `../../internal/httpapi/runtime/operations.go`. Preserve unsupported/wrong-method/non-entry rejection before provider transport and durable side effects; route-level absence is a permanent contract.
- Request-log contract files are split by list/detail/filter, attribution/failover, pricing, provider stream usage, and audit diagnostics. Reuse `request_logs_contract_harness_test.go`; assert pricing selection state and card role independently and require snapshots only on the priced selected-card path.
- `pricing_peak_valley_runtime_test.go` owns real-request persisted evidence and half-open timing boundaries; pure pricing/clock/digest cases belong in package-local runtime tests.
- Keep selector, admission, failover, lease/recovery, cache, partition, streaming-buffering, and telemetry-outbox tests at their distinct runtime boundaries. Include direct-entry cache transitions and model-list filtering when changing entry qualification.
- Preserve deterministic upstream coordination and observable lease/side-effect results rather than testing provider implementation details or duplicating a contract from another layer.
