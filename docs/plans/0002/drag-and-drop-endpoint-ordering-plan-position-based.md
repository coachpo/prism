# Drag-and-Drop Endpoint Ordering Plan (Position-Based)

## Summary
Implement endpoint ordering specifically for drag-and-drop UX by adding a persisted `position` field and a **single-item move API** (more natural for mouse drag “drop” actions than full-list resubmits).  
The backend will keep positions contiguous per profile (`0..N-1`) so frontend indexes always match server state.

## Public API / Interface Changes
- Add read-only `position: int` to endpoint response model in:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py` (`EndpointResponse`)
- Add new route in:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/endpoints.py`
- New endpoint:
  - `PATCH /api/endpoints/{endpoint_id}/position`
  - Request body: `{ "to_index": <int>=0 }`
  - Response body: ordered endpoint list (or wrapper containing `items`) so frontend can immediately resync after drop.
- Keep existing CRUD routes unchanged in path shape:
  - `GET /api/endpoints`
  - `POST /api/endpoints`
  - `PUT /api/endpoints/{endpoint_id}`
  - `DELETE /api/endpoints/{endpoint_id}`

## Data Model and Migration
- Update endpoint ORM model in:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/models/models.py`
- Add `position` column to `endpoints` (integer, non-null after backfill).
- Add migration:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/alembic/versions/0004_endpoint_position.py`
- Migration steps:
  1. Add nullable `position`.
  2. Backfill by profile using existing creation order (`id ASC`) to `0..N-1`.
  3. Set `position` to NOT NULL.
  4. Add non-unique index on `(profile_id, position)` for ordered reads.
- No unique constraint on `(profile_id, position)` initially (avoid complex transient conflicts during shifts); app logic enforces normalized contiguous ordering.

## Backend Behavior Changes
### `/api/endpoints` list/create/update/delete
- `GET /api/endpoints`: order by `position ASC, id ASC`.
- `POST /api/endpoints`: append at end (`position = current_count` or max+1).
- `PUT /api/endpoints/{id}`: keep for metadata updates only (`name`, `base_url`, `api_key`), not ordering.
- `DELETE /api/endpoints/{id}`:
  - keep existing “in-use by connections” protection;
  - on successful delete, compact positions of later endpoints (`position - 1`) in same profile.

### New move endpoint (`PATCH /api/endpoints/{endpoint_id}/position`)
- Validate:
  - endpoint exists in effective profile;
  - `to_index` within `[0, total_endpoints-1]`.
- Reorder flow (single transaction):
  - load profile endpoints in current order;
  - move target endpoint from old index to `to_index`;
  - rewrite affected `position` values to contiguous order;
  - return final ordered list.
- This directly matches drag-and-drop “drop result” semantics (one API call per drop).

### Inline endpoint creation in connections
- In `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`:
  - when `endpoint_create` is used, assign appended `position` consistently with endpoint create route.

## Config Export/Import Compatibility
- Update config schema in:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py` (`ConfigEndpointExport`)
- Add optional `position: int | None` (keep `version: 2`).
- Export behavior in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/config.py`:
  - export endpoints in `position` order and include `position`.
- Import behavior:
  - accept old v2 files without `position`;
  - if `position` present, use it as ordering hint;
  - if absent, use file order;
  - normalize to contiguous `0..N-1` when persisting.

## Test Plan
- Update/add tests in:
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py`
  - `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_multi_profile_isolation.py`
- Required scenarios:
  1. New endpoints append with increasing `position`.
  2. List endpoint order follows `position`.
  3. Move endpoint up/down via `PATCH /position` and verify persisted order.
  4. Move no-op (`to_index == current`) returns stable order.
  5. Out-of-range index returns validation error.
  6. Cross-profile move attempt cannot affect other profile data.
  7. Delete endpoint compacts following positions.
  8. Config export includes endpoint `position`.
  9. Config import preserves order when `position` exists.
  10. Config import remains valid for legacy v2 payloads without `position`.

## Assumptions and Defaults
- Field name: `position` (more drag/drop-friendly than `sequence`).
- Positioning is zero-based and contiguous per profile.
- Frontend sends reorder request on **drop** (not every hover/move).
- Backend is source of truth: frontend replaces local list with API response after each successful move.
- No API version bump; config stays at version `2` with backward-compatible optional ordering field.
