# BACKEND LOADBALANCE DOMAIN KNOWLEDGE BASE

## OVERVIEW
`domain/loadbalance/` owns HTTP-neutral Ban Policy runtime state, current-state snapshots, loadbalance event DTOs, and reset/delete helpers used by management handlers and runtime feedback. Runtime planning and provider transport consume this state through explicit interfaces.

## STRUCTURE
```text
loadbalance/
├── service.go                  # Management-facing current-state, reset, event, incident, and delete helpers
├── runtime_strategy.go         # Ban Policy and routing strategy math
├── runtime_state.go            # Persisted runtime state projection helpers
├── runtime_local_state.go      # In-process/runtime-local state provider behavior
├── runtime_events.go           # Event creation, summaries, and payload snapshots
└── *_test.go                   # Strategy and local-state tests
```

## WHERE TO LOOK
- Current-state list, reset, incident, and retention helpers: `service.go`
- Retry windows, ban thresholds, ban modes, and route-state transitions: `runtime_strategy.go`, `runtime_state.go`
- Runtime-local state provider and recovery behavior: `runtime_local_state.go`
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
