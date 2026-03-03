## Failover Auto-Recovery V2 Upgrade Plan

### Summary
Upgrade failover auto-recovery to reduce endpoint flapping, recover primary endpoints more predictably, and avoid premature circuit opens, while keeping external API behavior stable.  
This plan targets internal routing logic only (no breaking HTTP contract changes).

### Goals
- Improve stability under transient upstream failures.
- Reduce unnecessary cooldowns from one-off errors.
- Recover preferred (higher-priority) endpoints faster and more safely.
- Preserve current `/v1/*` and `/v1beta/*` endpoint contracts.

### Non-Goals (this iteration)
- No new load-balancing strategy names (still only `single` and `failover`).
- No Redis/distributed state yet (keep in-memory per process for now).
- No changes to provider list or proxy chaining rules.

---

## 1) Core Algorithm Upgrades

### 1.1 Replace fixed cooldown with thresholded exponential backoff + jitter
**Current anchor:** `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py:126`

- Introduce per-connection recovery state entry (internal type), replacing the tuple-only shape:
  - `consecutive_failures: int`
  - `blocked_until_mono: float | None`
  - `last_cooldown_seconds: float`
  - `last_failure_kind: str | None`
- New behavior for `mark_connection_failed(...)`:
  - Use `model.failover_recovery_cooldown_seconds` as `base_cooldown`.
  - First failure does **not** open circuit (threshold default: `2` consecutive transient failures).
  - On threshold hit and beyond: `cooldown = min(base * multiplier^(n-threshold), max_cooldown)`.
  - Add jitter (`±jitter_ratio`) before writing `blocked_until_mono`.
- On successful upstream response, `mark_connection_recovered(...)` clears state as today.

**Default tuning (global env settings):**
- `failover_failure_threshold=2`
- `failover_backoff_multiplier=2.0`
- `failover_max_cooldown_seconds=900`
- `failover_jitter_ratio=0.2`
- `failover_auth_error_cooldown_seconds=1800`

### 1.2 Prioritized probe re-entry (cooldown-expired endpoints)
**Current anchor:** `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py:77`

- Change attempt-plan construction to preserve priority order while skipping only currently blocked connections.
- If a blocked connection’s cooldown is expired, include it again in normal priority order (acts as half-open probe request).
- Keep `single` strategy unchanged.

### 1.3 Deprioritize known unhealthy connections
**Current anchor:** `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py:62`

- In failover planning, sort active connections by:
  1) `health_status != "unhealthy"` first,
  2) then `priority`,
  3) then `id`.
- Rationale: existing `health_status` from health-check should influence routing without DB schema/API changes.

---

## 2) Proxy Failure Classification and State Transitions

### 2.1 Centralize failure classification
**Current anchors:**  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py:305`  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py:567`  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/proxy_service.py:276`

- Add internal helper to classify failover-triggered failures:
  - `transient_http` (e.g. 429/5xx/529)
  - `auth_like` (403 + auth/permission message heuristics)
  - `connect_error`
  - `timeout`
- Keep failover trigger set unchanged for compatibility (`403,429,500,502,503,529`), but apply longer cooldown for `auth_like` failures.

### 2.2 Stop clearing recovery state on non-failover 4xx
**Current anchors:**  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py:400`  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py:668`

- Change rule:
  - `2xx/3xx` => recover/clear state.
  - failover-triggered failures => mark failed (with classification).
  - non-failover client errors (e.g. 400/404/422) => return immediately **without** forcing recovery clear.

---

## 3) State Lifecycle Hooks (Faster Manual Recovery)

### 3.1 Health-check success should clear failover state
**Current anchor:** `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py:608`

- When `/api/connections/{id}/health-check` returns `healthy`, call `mark_connection_recovered(profile_id, connection.id)` so manual checks immediately re-enable normal routing.

### 3.2 Clear state on connection/endpoint config changes
**Current anchors:**  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py:430`  
- `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/endpoints.py:84`

- Keep existing clear-on-`is_active` change.
- Add clear when connection routing/auth surface changes:
  - `endpoint_id`, `auth_type`, `custom_headers`.
- On endpoint update (`base_url` or `api_key`), clear state for all connections referencing that endpoint in the same profile.

---

## 4) Interface and Config Changes

### External APIs
- **No HTTP endpoint shape changes** for proxy routes or management routes.
- **No schema version bump** for config export/import in this iteration.

### Internal Interfaces/Types
- Update recovery state structure in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py:11`.
- Add classification helper(s) in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py:150` path.
- Extend settings in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/core/config.py:4` with failover-v2 tuning fields (validated ranges).

---

## 5) Implementation Steps (Decision-Complete)

1. **Config wiring**
   - Add new failover tuning settings in `core/config.py`.
   - Load settings once where needed (avoid repeated `get_settings()` calls in hot path where possible).

2. **Load balancer refactor**
   - Replace tuple state with structured entry.
   - Implement threshold/backoff/jitter logic in `mark_connection_failed`.
   - Update `build_attempt_plan` ordering rules (health + priority + cooldown state).
   - Keep `mark_connection_recovered` semantics (full clear).

3. **Proxy refactor**
   - Add failure-classification helper reused by stream and non-stream branches.
   - Apply differentiated cooldown policy (`auth_like` uses longer cooldown).
   - Adjust recovery-clear behavior to success-only (not generic non-failover 4xx).

4. **Router lifecycle hooks**
   - `connections.py`: clear state on successful health check and on endpoint/auth-header mutation.
   - `endpoints.py`: clear states for attached connections when base URL/key changes.

5. **Observability**
   - Add structured logs for circuit transition events:
     - `opened`, `extended`, `recovered`, `probe_eligible`.
   - Include `profile_id`, `connection_id`, `failure_kind`, `cooldown_seconds`.

---

## 6) Test Plan

### Unit tests (load balancer)
- Consecutive-failure threshold behavior (`1st fail no block`, `2nd fail blocked`).
- Exponential cooldown growth and max-cap enforcement.
- Jitter bounded within configured ratio.
- Attempt-plan ordering with:
  - mixed priority,
  - blocked/unblocked,
  - unhealthy vs healthy connections.

### Proxy behavior tests
- Failover-triggered failures mark failed and continue to next endpoint.
- Non-failover 4xx does not clear existing failure state.
- Auth-like 403 applies longer cooldown than generic transient failure.
- Success clears state and resumes preferred endpoint path after cooldown expiry.

### Router/state lifecycle tests
- Health-check success clears recovery state.
- Updating connection `endpoint_id`/`auth_type`/`custom_headers` clears state.
- Updating endpoint `base_url`/`api_key` clears states of dependent connections.

### Regression/update tests
- Update tests that directly touch `_recovery_state` (notably `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_multi_profile_isolation.py:1181`) to assert via helper behavior rather than raw dict shape assumptions.
- Keep existing DEF-010/DEF-011 behavior coverage intact.

---

## 7) Rollout and Risk Control

- Ship as internal behavior change with conservative defaults.
- Monitor logs for:
  - fallback rate,
  - repeated auth-like opens,
  - long-lived blocked connections.
- If instability is observed, temporarily reduce aggressiveness by:
  - lowering multiplier,
  - lowering max cooldown,
  - or raising threshold to 3.

---

## Assumptions and Defaults
- Existing failover status trigger set remains unchanged for compatibility.
- `failover_recovery_cooldown_seconds` remains the per-model base cooldown source.
- Recovery state remains in-memory and process-local in this iteration.
- No DB migration required for V2; distributed/shared failover state is deferred to a follow-up plan.
