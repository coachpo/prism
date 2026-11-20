# Loadbalance Domain

`runtime_strategy.go` and `canonical_strategies.go` own shared strategy math/defaults; `retry_preview.go` must use the same policy as runtime feedback.

- Keep retry-cycle and cumulative-ban thresholds inclusive (`>=`). Preserve profile/connection scope and deterministic state transitions; runtime-local state and retained UNLOGGED compatibility tables are not durable request history.
- Keep persisted event types and operator summaries aligned.
- Preserve explicit missing evidence rather than deriving UI state from absent events.
- Provider transport and feedback dispatch stay in [runtime](../../httpapi/runtime/AGENTS.md).
