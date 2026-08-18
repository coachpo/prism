# BACKEND LOADBALANCE DOMAIN KNOWLEDGE BASE

## OVERVIEW
`domain/loadbalance/` owns HTTP-neutral Ban Policy runtime state, the global current-state projection, loadbalance event DTOs/query/context, the shared canonical strategy specs, the deterministic retry preview calculator, and reset/delete helpers used by management handlers and runtime feedback. Runtime planning and provider transport consume this state through explicit interfaces.

## STRUCTURE
```text
loadbalance/
├── service.go                  # Management-facing current-state, reset, event, incident, and delete helpers
├── runtime_strategy.go         # Ban Policy and routing strategy math
├── runtime_state.go            # Persisted runtime state projection helpers
├── runtime_local_state.go      # Runtime-local state lifecycle
├── runtime_local_admission.go  # Runtime-local admission and observations
├── runtime_local_feedback.go   # Runtime-local feedback transitions
├── runtime_local_round_robin.go # Runtime-local round-robin cursors
├── runtime_events.go           # Event creation, summaries, and payload snapshots
├── current_state_global.go     # Global current-state cohort, completeness and cursor paging
├── event_query.go              # Event timeline query orchestration and filter validation
├── event_cursor.go             # Event cursor scope binding and codec
├── event_projection.go         # Event row loading and read projections
├── event_dto.go                # Event wire shapes shared with the management layer
├── canonical_strategies.go     # Canonical strategy definitions and defaults
├── retry_preview.go            # Retry/backoff preview math for the strategy UI
└── *_test.go                   # Strategy and local-state tests
```

## WHERE TO LOOK
- Current-state list, reset, incident, and retention helpers: `service.go`
- Retry windows, ban thresholds, ban modes, and route-state transitions: `runtime_strategy.go`, `runtime_state.go`
- Runtime-local state lifecycle, admission, feedback, and cursors: `runtime_local_state.go`, `runtime_local_admission.go`, `runtime_local_feedback.go`, `runtime_local_round_robin.go`
- Event query orchestration, cursor binding, row loading, and operator projections: `event_query.go`, `event_cursor.go`, `event_projection.go`
- Event types and operator summaries: `runtime_events.go`
- Management routes consuming this domain: `../../httpapi/management/loadbalance/AGENTS.md`
- Runtime feedback and planner consumers: `../../httpapi/runtime/AGENTS.md`

## CONVENTIONS
- Keep this package HTTP-neutral. Route parsing, Default-profile resolution, JSON response shaping, and admission errors stay under `httpapi/`.
- Keep Ban Policy thresholds inclusive: retry-cycle exhaustion uses `cycle_retry_attempts >= cycle_retry_attempt_limit`, and bans use `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`.
- Keep current-state snapshots profile and connection scoped. Runtime state is intentionally ephemeral where backed by UNLOGGED tables or local provider state.
- Keep loadbalance event types aligned with persisted events: `retry_scheduled`, `retry_exhausted`, `banned`, `unbanned`, `recovered`, and `admission_rejected`.

## ANTI-PATTERNS
- Do not add provider-specific request handling here.
- Do not borrow database lane ownership or run retention/partition cleanup from domain helpers.
- Do not infer frontend display state that is not backed by retained runtime state or events.
