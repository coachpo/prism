# Prism App-Wide Upgrade Plan: Drag-and-Drop Connection Priority

## Summary
- Upgrade connection priority management from numeric edits to drag-and-drop ordering, with backend-enforced contiguous priorities per `(profile_id, model_config_id)`.
- Keep locked decisions from `0003`: `Drag Only` UI, `Strict Move API`, and `Model Detail Only` frontend scope.
- Preserve runtime semantics: routing still uses persisted `priority`, now guaranteed contiguous and deterministic.

## Public API and Core Behavior Changes
- Add management route: `PATCH /api/models/{model_config_id}/connections/{connection_id}/priority` with body `{ "to_index": <int>=0 }`, returning full ordered `ConnectionResponse[]`.
- Backend schemas:
  - Add `ConnectionPriorityMoveRequest(to_index: int, ge=0)`.
  - Remove `priority` from `ConnectionCreate` and `ConnectionUpdate` (schema-forbidden on input).
  - Keep `priority` in `ConnectionResponse`.
- Frontend contracts:
  - Remove `priority` from `ConnectionCreate` and `ConnectionUpdate` request types.
  - Add `api.connections.movePriority(modelConfigId, connectionId, toIndex)`.

## Implementation Plan
- Backend ordering invariant enforcement:
  - In `connections` router, add a shared ordered-load/resequence path using `(priority, id)` and row locking (`FOR UPDATE`) for move/create/delete operations.
  - `create_connection`: append at end after in-transaction normalization.
  - `update_connection`: metadata/auth/pricing/activation only; `priority` rejected by schema.
  - `delete_connection`: compact priorities in same transaction after delete; keep failover recovery-state cleanup.
  - `move_priority` endpoint: validate model+profile ownership, validate `to_index` bounds, support no-op move, persist contiguous priorities, return ordered list with needed relations loaded.
- Deterministic runtime and response ordering:
  - `loadbalancer.get_active_connections` sorts by `(priority, id)`.
  - Model detail response path returns connections sorted by `(priority, id)`.
- Config import/export:
  - Keep export order `(priority, id)`.
  - On import, normalize each model's persisted priorities to contiguous `0..N-1`, preserving relative order by imported priority then payload order.
- Data migration:
  - Add `alembic` revision `0004_connection_priority_normalization.py`.
  - One-time normalization using window ranking by `(priority, id)` per `(profile_id, model_config_id)`.
- Frontend (Model Detail only):
  - Add `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities`.
  - Implement sortable connection cards with explicit drag handle.
  - On drop: optimistic reorder -> one move API call -> replace local list from backend response.
  - On failure: rollback + error toast.
  - Enforce single in-flight reorder guard.
  - Disable drag when search filter is active and show helper text to clear filter.
  - Remove numeric priority field/state from connection dialog; keep `P{priority}` badge read-only.

## Test Plan
- Backend automated scenarios:
  - Create appends at end (`priority == count_before`).
  - Move up/down persists, no-op move unchanged, out-of-range `to_index` returns `422`.
  - Wrong profile/model/connection combinations return `404`.
  - Delete compacts priorities.
  - Create/update payloads containing `priority` return `422`.
  - Import with duplicate/gapped priorities normalizes contiguous sequence.
  - Load balancer tie behavior uses `(priority, id)`.
- Frontend validation:
  - Drag reorder updates UI and persists after refresh.
  - Reorder survives profile switch/reload.
  - API error rolls back and shows toast.
  - Search-active disables dragging with explanation.
  - New connections appear at end.
  - Edit/create dialog has no priority input.
- Verification commands:
  - `cd backend && ./venv/bin/python -m pytest tests/ -v`
  - `cd frontend && pnpm run lint`
  - `cd frontend && pnpm run build`

## Assumptions and Defaults
- Breaking change is intentional: old clients sending `priority` in create/update are unsupported after rollout.
- API route parameter remains `model_config_id` (matching current route conventions).
- Backend is source of truth for final ordering; frontend always reconciles to response order.
- Config format remains `version: 2` (no version bump).
- Coordinated release window: backend (with migration) then frontend in same release event.
