# Request-Centric Audit UX Redesign Plan (Decision Complete)

## Summary
- Redesign observability UX so `Request Logs` is the primary and only browse surface for request/audit investigation.
- Remove standalone `Audit` page/table workflow and move linked audit inspection into `RequestLogsPage` detail experience.
- Keep `/audit` only as compatibility redirect to `/request-logs`.
- This is a deliberate frontend breaking upgrade in navigation and page semantics.

## Public UX and Interface Changes
1. Navigation and routes:
- Remove `Audit` entry from sidebar navigation.
- Remove dashboard quick action that opens `/audit`.
- Replace `/audit` page behavior with redirect to `/request-logs`.

2. Request Logs page behavior:
- Add linked-audit affordance from each request detail.
- Replace split cross-page workflow with one detail surface in `RequestLogsPage`.
- Request detail UI becomes tabbed (`Overview` + `Audit`) and loads audit payload via request linkage.

3. API/filter contracts (additive):
- `GET /api/stats/requests` adds optional `request_id` filter (exact request lookup).
- `GET /api/audit/logs` adds optional `request_log_id` filter (exact linked audit lookup).
- Frontend types add:
  - `StatsRequestParams.request_id?: number`
  - `AuditLogParams.request_log_id?: number`

## Implementation Plan
### 1) Routing and app shell redesign
- Update `frontend/src/App.tsx`:
  - Remove lazy-loaded `AuditPage` route component usage.
  - Add route-level redirect for `/audit` to `/request-logs`.
- Update `frontend/src/components/layout/AppLayout.tsx`:
  - Remove `Audit` nav link.
  - Remove `/audit` from `PROFILE_SCOPED_PREFIXES`.
- Update `frontend/src/pages/DashboardPage.tsx`:
  - Remove `Open Audit Viewer` quick action button.
  - Keep request log quick action as the investigation entry point.

### 2) Request Logs as single investigation surface
- Update `frontend/src/pages/RequestLogsPage.tsx`:
  - Support URL query params for deep-link opening (`request_id`, optional audit-focus params).
  - Fetch exact request row when `request_id` is provided.
  - Auto-open selected request detail from deep link.
- Update `frontend/src/pages/request-logs/RequestLogDetailSheet.tsx`:
  - Redesign to tabbed detail experience:
    - `Overview`: existing request metrics/actions.
    - `Audit`: request/response headers/body/status/duration from linked audit record.
  - Add action `Open linked audit` that resolves via `api.audit.list({ request_log_id, limit: 1, offset: 0 })`.
  - Provide explicit empty states:
    - no audit recorded,
    - audit deleted/orphaned,
    - loading/error states.
  - Keep connection navigation action intact.

### 3) Audit page decommissioning
- Replace `frontend/src/pages/AuditPage.tsx` implementation with a thin redirect component, or remove its route usage entirely and keep file only if needed for compatibility import cleanup.
- Decommission audit-specific table/detail composition (`path`, `model`, `provider`, `duration` browse columns are removed by removing the page workflow).

### 4) API and type additions
- Backend:
  - `backend/app/routers/stats.py`: accept `request_id` query param and pass to request-log service.
  - `backend/app/services/stats/request_logs.py`: filter by `RequestLog.id` when `request_id` provided.
  - `backend/app/routers/audit.py`: accept `request_log_id` query param and filter by `AuditLog.request_log_id`.
- Frontend:
  - `frontend/src/lib/types.ts`: add optional params in `StatsRequestParams` and `AuditLogParams`.
  - `frontend/src/lib/api.ts`: existing query-builder path supports new optional params without structural change.

## Test Plan
### Backend tests
1. `GET /api/stats/requests?request_id=<id>` returns only the exact row in current effective profile.
2. `GET /api/audit/logs?request_log_id=<id>` returns only linked row(s) in current effective profile.
3. Existing request/audit list behavior unchanged when new params omitted.

### Frontend validation
1. From request row -> open detail -> open linked audit tab -> payload renders correctly when link exists.
2. Missing link (`request_log_id` null / no row found) shows clear empty state and no crash.
3. `/audit` URL redirects to `/request-logs` safely.
4. Sidebar no longer shows `Audit`; dashboard no longer offers `Open Audit Viewer`.
5. Existing request log filtering/search/pagination behavior remains intact.

### Verification commands
1. `cd backend && ./venv/bin/python -m pytest tests/ -v`
2. `cd frontend && pnpm run lint`
3. `cd frontend && pnpm run build`

## Assumptions and Defaults
- Standalone global audit browsing is intentionally removed in this upgrade.
- `/audit` compatibility is redirect-only; old audit-specific filter semantics are not preserved.
- Linked audit lookup is request-centric and uses `request_log_id` relation as source of truth.
- No DB schema migration is required for this plan.
