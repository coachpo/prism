# Prism Endpoint Ordering Upgrade (Whole-App, Decision-Complete)

## Summary
Implement persisted endpoint ordering end-to-end (DB -> backend APIs -> frontend drag/drop UX -> config export/import -> docs/tests) using a zero-based contiguous `position` per profile. Backend remains source-of-truth after every drop, and config format stays `version: 2` with backward-compatible optional `position`.

## Public API / Interface Changes
- **Backend API**
  - Add `position: int` to `EndpointResponse` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`.
  - Add `PATCH /api/endpoints/{endpoint_id}/position` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/endpoints.py`.
  - Request body schema: `{"to_index": int}` with `to_index >= 0`.
  - Response schema: `list[EndpointResponse]` (raw ordered list, not wrapped).
- **Config contract (still version 2)**
  - Add optional `position: int | None` to `ConfigEndpointExport` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`.
- **Frontend contract updates**
  - Add `position: number` to `Endpoint` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/types.ts`.
  - Add optional `position?: number | null` to frontend `ConfigEndpointExport` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/types.ts`.
  - Add `api.endpoints.movePosition(id, to_index)` in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/lib/api.ts`.

## Implementation Workstreams

### 1) Data Model + Migration
- Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/models/models.py`:
  - Add `Endpoint.position` column (integer, non-null).
  - Add index metadata for `(profile_id, position)` (non-unique).
- Create `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/alembic/versions/0004_endpoint_position.py`:
  1. Add nullable `position`.
  2. Backfill using per-profile `id ASC` ranking (`0..N-1`).
  3. Alter to `NOT NULL`.
  4. Create index on `(profile_id, position)`.
- Downgrade removes the new index and column.

### 2) Backend Endpoint Ordering Behavior
- In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/endpoints.py`:
  - `GET /api/endpoints`: always `ORDER BY position ASC, id ASC`.
  - `POST /api/endpoints`: set `position = max(position)+1` within effective profile.
  - `PUT /api/endpoints/{id}`: unchanged metadata update only.
  - `DELETE /api/endpoints/{id}`: after successful in-use check + delete, decrement `position` for later rows in same profile.
  - Add `PATCH /api/endpoints/{endpoint_id}/position`:
    - Validate endpoint belongs to effective profile.
    - Validate `to_index <= total-1`; return `422` if out of range.
    - Reorder in one transaction: load ordered list, move item, rewrite contiguous positions, flush, return final ordered list.
    - If no-op move, return current ordered list unchanged.
- In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
  - Inline endpoint creation (`endpoint_create`) uses same append-position logic as endpoint create route.

### 3) Config Export/Import Compatibility
- In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/config.py`:
  - Export endpoints sorted by `position ASC, id ASC`.
  - Include `position` in exported endpoint objects.
  - Import accepts both:
    - legacy v2 entries without `position` (use file order),
    - v2 entries with `position` (use as ordering hint).
  - Deterministic import ordering rule:
    - sort by `(position if present else original_file_index, original_file_index)`,
    - then normalize persisted positions to contiguous `0..N-1`.
- Keep `version: 2` behavior and all existing import validations.

### 4) Frontend Upgrade for Drag-and-Drop
- Add DnD dependencies in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/package.json`:
  - `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities`.
- Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/EndpointsPage.tsx`:
  - Keep existing card layout, make cards sortable.
  - Trigger reorder API **only on drop**.
  - Use optimistic local reordering, then replace local list with backend response.
  - On API failure: rollback previous local order + show toast.
  - Add explicit drag handle and keyboard sensor support.
  - Prevent overlapping reorder requests (single in-flight move).
- No behavior changes required in other pages; they already consume `/api/endpoints` list order.

### 5) Documentation Synchronization
- Update:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/API_SPEC.md` (new PATCH route, endpoint `position`, config export example).
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/DATA_MODEL.md` (endpoints table + new index).
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/docs/SMOKE_TEST_PLAN.md` (route matrix + reorder scenarios).
  - Optional route list sync in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/README.md` and `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/README.md`.

## Test Cases and Scenarios

### Backend tests
- Update/add in:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py`
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_multi_profile_isolation.py`
- Required coverage:
  1. Create appends increasing positions.
  2. List is ordered by position.
  3. Move up/down persists correctly.
  4. No-op move returns stable order.
  5. Out-of-range move returns validation error.
  6. Cross-profile move cannot mutate other profile.
  7. Delete compacts positions.
  8. Export includes endpoint position.
  9. Import preserves position when provided.
  10. Legacy v2 import without position still valid and ordered by file order.
- Also update existing endpoint fixtures used by schema tests to include `position` where needed.

### Frontend validation
- Run:
  - `cd /Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend && pnpm run lint`
  - `cd /Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend && pnpm run build`
- Manual QA:
  - Drag reorder persists after refresh.
  - Create appends at end.
  - Delete compacts visual order.
  - Import file with/without `position` yields expected endpoint order in UI.

## Rollout / Compatibility
- Deploy order: **backend first**, then frontend.
- Old frontend remains compatible with new backend (extra response field is additive).
- New frontend requires backend move endpoint for DnD; backend-first avoids transient 404s.
- No config version bump and no provider/runtime routing behavior changes.

## Explicit Assumptions and Defaults
- Field name is `position`.
- Position is zero-based contiguous per profile.
- Move API returns raw ordered endpoint array.
- Frontend sends exactly one reorder request per drop event.
- No unique DB constraint on `(profile_id, position)` in this phase.
- Backend canonicalizes final ordering after every mutation (create/move/delete/import).
