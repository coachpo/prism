# Backend Upgrade Plan: Remove `round_robin` and Add Failover Auto-Recovery

## Summary
This upgrade will make `failover` the only multi-endpoint balancing strategy, remove `round_robin` everywhere without backward compatibility, and add a configurable per-model automatic recovery policy for `failover` using a passive half-open probe model.

Scope is backend only. No frontend changes.

## Locked Decisions
1. Keep strategy name `failover` (no rename to `fail_safe`).
2. Recovery policy is configured per model.
3. Recovery execution is passive half-open probe (no background polling).

## Public API / Interface / Type Changes
1. `lb_strategy` accepted values become exactly `single | failover` in all backend schemas and validations.
2. Add model-level fields (create, update, get, list, config export/import):
   - `failover_recovery_enabled: bool` (default `true`)
   - `failover_recovery_cooldown_seconds: int` (default `60`, validation `>=1` and `<=3600`)
3. Config import/export version changes from `1` to `2`.
4. Config import will reject:
   - `version != 2`
   - any model with `lb_strategy = "round_robin"`

## Data Model and Migration Plan
1. Update model in `backend/app/models/models.py`:
   - Keep `lb_strategy` column, but allowed app-level values become `single` and `failover`.
   - Add `failover_recovery_enabled` column (`BOOLEAN NOT NULL DEFAULT 1`).
   - Add `failover_recovery_cooldown_seconds` column (`INTEGER NOT NULL DEFAULT 60`).
2. Extend startup migration in `backend/app/main.py` (`_add_missing_columns`):
   - Add missing `model_configs.failover_recovery_enabled`.
   - Add missing `model_configs.failover_recovery_cooldown_seconds`.
   - One-way data migration: `UPDATE model_configs SET lb_strategy='failover' WHERE lb_strategy='round_robin'`.
3. No backward compatibility promise:
   - Old payload/config values with `round_robin` are rejected at API boundary after upgrade.

## Backend Function-Level Changes
1. `backend/app/services/loadbalancer.py`
   - Remove `_rr_counters` and all `round_robin` branches.
   - Add in-memory recovery state map keyed by endpoint id (blocked-until monotonic timestamp).
   - Add functions:
     - `build_attempt_plan(model_config, now_mono)` for strategy-aware ordered endpoint attempts.
     - `mark_endpoint_failed(endpoint_id, cooldown_seconds, now_mono)` to start cooldown.
     - `mark_endpoint_recovered(endpoint_id)` to clear cooldown.
   - Behavior:
     - `single`: return only the highest-priority active endpoint.
     - `failover`: return active endpoints not in cooldown first, then cooldown-expired endpoints as probe candidates.
2. `backend/app/routers/proxy.py`
   - Replace unconditional failover attempt list with strategy-aware plan from load balancer.
   - `single` strategy:
     - do not fail over to secondary endpoints.
     - return first response/error outcome directly.
   - `failover` strategy:
     - on failover-triggering status (`429/500/502/503/529`) or connect/timeout errors, mark endpoint failed and try next candidate.
     - on non-failover responses (including non-triggering 4xx and 2xx), mark endpoint recovered.
   - If all failover endpoints are currently in cooldown and none are probe-eligible, return `503` with clear detail.
3. `backend/app/schemas/schemas.py`
   - Change `lb_strategy` fields to strict literal enum `single | failover` in:
     - `ModelConfigBase`, `ModelConfigUpdate`, `ModelConfigResponse`, `ModelConfigListResponse`, `ConfigModelExport`.
   - Add recovery fields to the same model/config schemas.
4. `backend/app/routers/models.py`
   - Validate and persist new recovery fields.
   - For proxy models, force `lb_strategy="single"` and persist default recovery values.
5. `backend/app/routers/config.py`
   - Export `version=2`.
   - Include recovery fields in `ConfigModelExport`.
   - Import validation requires `version=2` and strict strategy values (`single`, `failover` only).
6. `backend/app/core/config.py`
   - Keep `failover_cooldown_seconds` as default source for model creation/migration defaults (no env contract rename).

## Routing and Recovery Behavior Spec
1. Endpoint ordering remains by `priority` ascending.
2. `single`:
   - Use only top-priority active endpoint.
   - No retries to other endpoints.
3. `failover`:
   - Try healthy/non-cooldown endpoints first in priority order.
   - Cooldown-expired endpoints are eligible as passive probes (tried after immediately healthy candidates, or first if none healthy).
4. Failure classification for cooldown:
   - Failover status codes (`429, 500, 502, 503, 529`) and connect/timeout errors start/reset cooldown.
5. Recovery classification:
   - Any non-failover response marks endpoint recovered and clears cooldown.
6. Cooldown timing:
   - `blocked_until = now + failover_recovery_cooldown_seconds`.
   - If `failover_recovery_enabled=false`, failover still works but no cooldown suppression/recovery tracking is applied.

## Test Plan (Backend)
1. Unit tests in `backend/tests`:
   - Schema validation rejects `lb_strategy="round_robin"`.
   - New recovery fields serialize/deserialize and validate bounds.
   - Config import rejects `version=1`.
2. Load-balancer behavior tests:
   - `single` returns only one endpoint even when multiple are active.
   - `failover` skips cooldown endpoints.
   - cooldown-expired endpoint becomes probe-eligible.
   - success clears cooldown; failover-trigger failure re-blocks.
3. Proxy integration tests (mocked upstream):
   - `single`: no secondary endpoint attempts.
   - `failover`: secondary endpoint attempted on failover-trigger status and on timeout/connect errors.
   - Recovery flow: endpoint fails, is skipped during cooldown, then is retried after cooldown and recovered on success.
   - All-attempts-failed behavior remains `502` with last error detail.
   - All endpoints cooling down returns `503` with cooldown detail.
4. Regression updates to `backend/tests/test_smoke_defect_regressions.py`:
   - replace any `round_robin` expectations with new strategy set.
   - add field-coverage assertions for new recovery fields in config schemas.

## Documentation Updates (Backend-Facing)
1. `docs/API_SPEC.md`: remove `round_robin`, add recovery fields, set config version to 2.
2. `docs/ARCHITECTURE.md`: update load-balancing section and new passive recovery flow.
3. `docs/DATA_MODEL.md`: update `model_configs` schema and strategy description.
4. `docs/SMOKE_TEST_PLAN.md`: replace/remove round-robin cases; add cooldown/recovery scenarios.

## Acceptance Criteria
1. No backend API accepts or emits `round_robin`.
2. Existing DB rows with `round_robin` are converted to `failover` during upgrade.
3. `single` no longer retries alternate endpoints.
4. `failover` performs automatic cooldown plus passive recovery probes according to per-model policy.
5. Config export/import fully round-trips new recovery fields with `version=2`.
6. Backend tests pass for new behavior and regressions.

## Assumptions and Defaults
1. `failover` remains the canonical strategy name.
2. Recovery policy is per-model only (not global-only or per-endpoint).
3. Recovery mode is passive half-open probe, no background scheduler.
4. Default cooldown is 60 seconds (`settings.failover_cooldown_seconds`).
5. No backward compatibility for old config format (`version=1`) or old strategy value (`round_robin`).
