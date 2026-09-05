# Load-Balance Management Guidance

Keep strategy authoring in `routes.go`/`store.go`, process-state reads and reset in `current_state_observability.go`, and retained event/incident reads in `event_routes.go`/`incident_routes.go`. Shared Ban Policy behavior belongs to `../../../domain/loadbalance/`.

- Preserve the serialized default-strategy contract, set-default CAS, and deletion guards for defaults and attached models. Use canonical policy normalization for every write and import.
- Current state is process-local evidence from `LocalRuntimeStateStore`, merged with configured targets. Preserve process identity, configuration generation, completeness, cursor scope, and private/no-store semantics; resetting state does not mutate the strategy.
- Event paging uses the signed query-context and retention-owner validation in `event_query_context_routes.go`/`events_query_context.go`. Do not substitute current runtime state for retained event evidence.
- Effect previews stay side-effect-free. Requests/statistics reads remain in `../stats/`.
