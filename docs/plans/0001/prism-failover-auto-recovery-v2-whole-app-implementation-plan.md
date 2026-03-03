# Prism Failover Auto-Recovery V2 (Whole-App) Implementation Plan

## Summary

- Deliver the backend V2 failover behavior exactly as an internal routing upgrade, while keeping `/v1/*`, `/v1beta/*`, and `/api/*` request/response contracts stable.
- Extend operational tuning via environment settings only, with no DB migration and no config export/import schema bump.
- Align frontend wording, architecture/docs, regression tests, and rollout runbooks so behavior and operator expectations stay consistent across backend, frontend, and root docs.
- Ship as coordinated submodule updates (`backend/`, `frontend/`) plus root repo docs/submodule-pointer updates.

## Public Interfaces / Contracts

- HTTP API contracts: no endpoint additions, removals, or payload shape changes in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py`, `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`, and `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/endpoints.py`.
- Config import/export: keep `version: 2` behavior unchanged in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/config.py` and frontend validator `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/configImportValidation.ts`.
- New operational interface (env settings in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/core/config.py`): `FAILOVER_FAILURE_THRESHOLD=2`, `FAILOVER_BACKOFF_MULTIPLIER=2.0`, `FAILOVER_MAX_COOLDOWN_SECONDS=900`, `FAILOVER_JITTER_RATIO=0.2`, `FAILOVER_AUTH_ERROR_COOLDOWN_SECONDS=1800`.
- Internal type/interface change: `_recovery_state` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py` moves from tuple values to a structured entry object.

## Implementation Workstreams

## 1) Backend settings and config wiring

1. Add the five V2 failover settings to `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/core/config.py` with explicit numeric bounds and defaults; keep existing fields for compatibility.
2. Cache settings resolution (single-process cached accessor) so failover hot paths do not repeatedly instantiate settings.
3. Document env vars in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/README.md` and root `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/README.md`.

## 2) Load balancer V2 state model and algorithm

1. Replace `_recovery_state: dict[(profile_id, connection_id), tuple]` with `RecoveryStateEntry` containing `consecutive_failures`, `blocked_until_mono`, `last_cooldown_seconds`, `last_failure_kind` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py`.
2. Update active connection ordering in failover mode to `(connection.health_status == "unhealthy", connection.priority, connection.id)` while keeping `single` strategy behavior unchanged.
3. Update `build_attempt_plan(...)` to skip only currently-blocked entries; when cooldown is expired, mark as probe-eligible and include in normal sorted order for that request.
4. Refactor `mark_connection_failed(...)` to accept `failure_kind` and compute cooldown as: auth-like fixed cooldown; otherwise thresholded exponential backoff from model base cooldown; apply jitter before writing `blocked_until_mono`; first transient failure only increments counters and does not block.
5. Keep `mark_connection_recovered(...)` as full-state clear, with structured recovery log event.

## 3) Proxy failure classification and transition rules

1. Add a shared internal classifier in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py` used by streaming, non-streaming, connect-error, and timeout branches.
2. Keep failover trigger set from `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/proxy_service.py` unchanged (`403,429,500,502,503,529`) for compatibility.
3. Implement failure kinds: `transient_http`, `auth_like`, `connect_error`, `timeout`; classify `auth_like` only for `403` with auth/permission keyword heuristics from upstream error text.
4. Apply recovery transitions uniformly: `2xx/3xx => mark_connection_recovered`, failover-triggered failures => `mark_connection_failed`, non-failover `4xx` => return without clearing existing failover state.
5. Ensure current request/audit logging behavior stays intact for each attempt.

## 4) Router lifecycle hooks for faster manual recovery

1. In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`, call `mark_connection_recovered(profile_id, connection.id)` when health check result is `healthy`.
2. In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`, clear failover state when connection routing/auth surface changes (`is_active`, `endpoint_id`, `auth_type`, `custom_headers`).
3. In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/endpoints.py`, on endpoint `base_url` or `api_key` change, clear failover state for all same-profile connections referencing that endpoint.

## 5) Observability and logging

1. Emit structured transition logs from load balancer with event types `opened`, `extended`, `recovered`, `probe_eligible`.
2. Include `profile_id`, `connection_id`, `failure_kind`, `cooldown_seconds`, and `consecutive_failures` in every transition log.
3. Keep log volume bounded by logging `probe_eligible` only on cooldown-expiry transition, not every routing call.

## 6) Frontend alignment (no contract changes)

1. Update recovery policy wording in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/ModelsPage.tsx` from fixed/periodic language to “base cooldown + automatic backoff/jitter” semantics.
2. Update model detail summary text in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/ModelDetailPage.tsx` to display “Base cooldown” wording so UI matches backend behavior.
3. Keep frontend types/API client unchanged unless backend contract changes are introduced (none planned here).

## 7) Documentation synchronization

1. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/ARCHITECTURE.md` failover section with threshold/backoff/jitter, health-status ordering, probe re-entry, and non-failover-4xx state behavior.
2. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/PRD.md` failover requirements to describe V2 recovery behavior and auth-like cooldown handling.
3. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/SMOKE_TEST_PLAN.md` scenarios C07/C08/C09 (and add explicit non-failover-4xx persistence scenario).
4. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/DATA_MODEL.md` runtime-state notes to describe new in-memory state fields (still process-local, no schema migration).

## Test Cases and Scenarios

| Area                 | Required scenarios                                                                                                                                                           | Target files                                                                                                                                                                                |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Load balancer unit   | threshold behavior, exponential growth, max cap, jitter bounds, health+priority ordering, probe re-entry ordering, single strategy unchanged                                 | `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py` (or new focused failover test module)                                               |
| Proxy behavior       | failover statuses continue to next endpoint, auth-like 403 gets longer cooldown, connect/timeout classification, non-failover 4xx does not clear state, success clears state | `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py`                                                                                     |
| Router lifecycle     | health-check success clears state, connection mutation clears state, endpoint credential/url change clears dependent states                                                  | `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py`                                                                                     |
| Isolation regression | avoid asserting raw `_recovery_state` dict shape; assert behavior via public helpers and profile namespacing                                                                 | `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_multi_profile_isolation.py`                                                                                      |
| Frontend/build       | failover copy renders correctly; lint/build pass                                                                                                                             | `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/ModelsPage.tsx`, `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/ModelDetailPage.tsx` |

## Rollout and Risk Control

1. Deploy as behavior-only backend change with conservative defaults and no migration window requirement.
2. Monitor transition logs and request metrics for fallback rate, repeated auth-like opens, and long-lived blocked connections.
3. If instability appears, tune env vars without code rollback (increase threshold, lower multiplier, lower max cooldown, reduce jitter).
4. Coordinate release commits across `backend` submodule, `frontend` submodule, then root repo pointer/docs update.

## Explicit assumptions and defaults

- `FAILOVER_STATUS_CODES` remains unchanged for compatibility.
- `failover_recovery_cooldown_seconds` remains the per-model base cooldown source.
- `FAILOVER_MAX_COOLDOWN_SECONDS` is treated as a hard cap per formula, even if a model base cooldown is higher.
- Auth-like detection uses `403` plus auth/permission keyword heuristics from upstream error text; unmatched `403` remains `transient_http`.
- Recovery state stays in-memory and process-local; Redis/distributed state is deferred.
- No HTTP schema changes and no config version bump in this iteration.
