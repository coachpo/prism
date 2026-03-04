# Connection Priority Drag-and-Drop Upgrade Plan (Decision Complete)

## Summary
Upgrade Prism so connection priority is controlled by drag-and-drop ordering (not numeric input), with backend-enforced contiguous priorities per `(profile_id, model_config_id)`.
This plan is locked to:
1. `Drag Only` UI behavior.
2. `Strict Move API` for management routes.
3. `Model Detail Only` frontend scope for this release.

## Scope
1. Implement drag-and-drop ordering in Model Detail connection cards.
2. Add backend move API for connection ordering.
3. Make create/delete/move preserve contiguous `priority` (`0..N-1`) per model in each profile.
4. Remove manual `priority` writes from connection create/update API contracts.
5. Keep runtime routing semantics based on `priority`.

Out of scope:
1. Drag-and-drop in other pages.
2. Changing provider/runtime behavior beyond ordering source.
3. Config format version bump.

## Public API / Interface Changes
1. Add new backend route:
`PATCH /api/models/{model_config_id}/connections/{connection_id}/priority`
Request body:
```json
{ "to_index": 0 }
```
Response body:
```json
[ConnectionResponse, ...] // ordered by priority asc, id asc
```

2. Add new backend request schema in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`:
`ConnectionPriorityMoveRequest` with `to_index: int` and `ge=0`.

3. Change backend connection write contracts in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`:
1. Remove `priority` from `ConnectionCreate`.
2. Remove `priority` from `ConnectionUpdate`.
3. Keep `priority` in `ConnectionResponse`.

4. Frontend type updates in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/types.ts`:
1. Remove `priority` from `ConnectionCreate`.
2. Remove `priority` from `ConnectionUpdate`.
3. Keep `Connection.priority` as read-only display data.

5. Frontend API client update in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/api.ts`:
Add `api.connections.movePriority(modelConfigId, connectionId, toIndex)`.

## Backend Implementation Plan
1. Add priority resequencing helpers in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
1. Load connections ordered by `(priority, id)`.
2. Rewrite priorities to contiguous indexes.
3. Optionally lock rows with `FOR UPDATE` during move operations.

2. Add move endpoint in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
1. Validate model exists in effective profile.
2. Validate connection belongs to the model and profile.
3. Validate `to_index` in `[0, total_connections - 1]`, else `422`.
4. No-op move returns current ordered list.
5. Reorder in one transaction and persist contiguous priorities.
6. Return final ordered list with endpoint/pricing relations loaded.

3. Update create behavior in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
1. Append new connections at end (`priority = current_count`).
2. Do not accept `priority` in create payload (schema-level strictness).
3. Keep inline endpoint creation flow unchanged except append position logic.

4. Update update behavior in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
1. Treat update as metadata/auth/pricing/activation only.
2. Reject `priority` in update payload via schema `extra="forbid"`.

5. Update delete behavior in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
1. Capture deleted connection priority.
2. Decrement later rows (`priority > deleted_priority`) in same profile/model.
3. Preserve failover recovery-state cleanup behavior.

6. Ensure deterministic routing tie-break in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py`:
Sort active connections by `(priority, id)`.

7. Ensure deterministic model-detail ordering in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/models.py`:
Sort `config.connections` by `(priority, id)` before response (or enforce equivalent deterministic order in serialization path).

8. Config import/export adjustments in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/config.py`:
1. Keep export sorted by `(priority, id)`.
2. After import for each model, normalize persisted priorities to contiguous `0..N-1` (preserve relative order by imported `(priority, connection_id/file order)`).

## Data Migration Plan
1. Add data migration in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/alembic/versions`:
`0004_connection_priority_normalization.py`.
2. Migration behavior:
1. For each `(profile_id, model_config_id)`, rank by `(priority, id)`.
2. Rewrite to contiguous `0..N-1`.
3. No schema shape changes required.

## Frontend Implementation Plan (Model Detail Only)
1. Add DnD deps in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/package.json`:
`@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities`.

2. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/ModelDetailPage.tsx`:
1. Replace static mapped list with sortable card list.
2. Add explicit drag handle on each connection card.
3. Trigger reorder API only on drop.
4. Use optimistic local reorder, then replace from backend response.
5. Roll back on API failure and show toast.
6. Keep single in-flight reorder guard to prevent overlapping moves.
7. Disable drag while search filter is active; show short helper text to clear filter before reordering.
8. Keep `P{priority}` badge as read-only display.

3. Remove manual priority editing from dialog in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/ModelDetailPage.tsx`:
1. Remove priority input state and field.
2. Update dialog description to indicate order is managed by dragging cards.

4. API client and types:
1. Add move call in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/api.ts`.
2. Remove `priority` from create/update request types in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/types.ts`.

## Documentation Updates
1. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/API_SPEC.md`:
1. New move route.
2. Create/update payloads no longer accept `priority`.

2. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/DATA_MODEL.md`:
1. Connection priority invariant is contiguous per profile+model.
2. Ordering semantics for routing and UI.

3. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/SMOKE_TEST_PLAN.md`:
1. Add move scenarios.
2. Add create-append and delete-compact expectations.

## Test Cases and Scenarios

### Backend tests
Update/add in:
`/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py`
`/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_multi_profile_isolation.py`

Required scenarios:
1. Create appends at end (`priority == count_before`).
2. Move up and move down persist correctly.
3. No-op move returns unchanged order.
4. Out-of-range `to_index` returns `422`.
5. Wrong profile/model/connection combination returns `404`.
6. Delete compacts following priorities.
7. Update payload containing `priority` is rejected (`422`).
8. Create payload containing `priority` is rejected (`422`).
9. Import with duplicate/gapped priorities is normalized to contiguous sequence.
10. Load balancer chooses in `(priority, id)` order.

### Frontend validation
1. Drag reorder updates badges and persists after refresh.
2. Drag reorder survives page reload and profile switch.
3. Reorder API failure rolls back UI order and shows error.
4. Search-active state disables dragging and explains why.
5. Add connection appears at end automatically.
6. Edit connection does not expose numeric priority field.

Commands:
1. `cd /Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend && ./venv/bin/python -m pytest tests/ -v`
2. `cd /Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend && pnpm run lint`
3. `cd /Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend && pnpm run build`

## Rollout and Compatibility
1. This is a deliberate breaking API change for old clients that send `priority` in create/update.
2. Deploy backend and frontend in one coordinated release window.
3. If staged rollout is required, gate new frontend drag feature behind backend-capability check to avoid transient mismatch.
4. No config version bump required; config still uses `version: 2`.

## Explicit Assumptions and Defaults
1. `priority` remains the persisted ordering field for connections.
2. Priority is zero-based contiguous per `(profile_id, model_config_id)`.
3. Frontend issues exactly one move request per drop event.
4. Backend is source of truth; frontend always replaces local order from API response.
5. Drag-and-drop is supported only in `ModelDetailPage` for this release.
6. Manual numeric priority editing is removed from UI and disallowed in create/update APIs.
