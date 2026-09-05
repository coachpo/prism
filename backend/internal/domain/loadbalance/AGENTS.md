# Loadbalance Domain

`runtime_local_state.go`, `runtime_local_admission.go`, `runtime_local_feedback.go`, and `runtime_local_round_robin.go` own runtime-local state and transitions. `runtime_strategy.go` and `canonical_strategies.go` own shared strategy math/defaults; `retry_preview.go` must use the same policy as runtime feedback.

- Keep retry-cycle and cumulative-ban thresholds inclusive (`>=`). Preserve profile/connection scope and deterministic state transitions; runtime-local state and retained UNLOGGED compatibility tables are not durable request history.
- `runtime_events.go` owns event snapshots for retry scheduled/exhausted, banned/unbanned, recovered, and admission rejected. Keep persisted event types and operator summaries aligned.
- `current_state_global.go` owns cohort completeness and cursor paging. `event_query.go`, `event_cursor.go`, and `event_projection.go` own event filter validation, cursor binding, and retained projections; preserve explicit missing evidence rather than deriving UI state from absent events.
- Management parsing stays in [management/loadbalance](../../httpapi/management/loadbalance/AGENTS.md); provider transport and feedback dispatch stay in [runtime](../../httpapi/runtime/AGENTS.md).
