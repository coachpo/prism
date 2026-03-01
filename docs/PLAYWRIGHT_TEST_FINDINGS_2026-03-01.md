# Playwright Test Findings - 2026-03-01

## Scope Executed

- Started stack via `./start.sh full`.
- Ran multi-page WebUI sweep across:
  - Dashboard
  - Models
  - Endpoints
  - Statistics
  - Request Logs
  - Audit
  - Settings
- Ran profile-isolation validation across profile A (`id=7`, ISO-B-1772319229) and profile B (`id=8`, Dummyprofile).
- Captured evidence screenshots.

## Defect 1 (Resolved In-Session) - Stats endpoints failed for ISO-8601 `Z` timestamps

### Severity

High (previously broke Statistics and Request Logs fetches under normal UI datetime serialization)

### Symptom

Frontend showed browser CORS failures for stats requests, while backend was returning HTTP 500 for timezone-suffixed (`Z`) timestamps. The CORS message was a downstream symptom of the backend error path.

### Reproduction

Previously worked (no timezone suffix):

```bash
curl -i -H "Origin: http://localhost:3000" \
  "http://localhost:8000/api/stats/requests?from_time=2026-02-28T03:29:06.216&limit=50"
```

Returns `200 OK` with `access-control-allow-origin: *`.

Previously failed (UTC timezone suffix `Z`):

```bash
curl -i -H "Origin: http://localhost:3000" \
  "http://localhost:8000/api/stats/requests?from_time=2026-02-28T03:29:06.216Z&limit=50"
```

Previously returned `500 Internal Server Error`.

Also previously failed:

```bash
curl -i -H "Origin: http://localhost:3000" \
  "http://localhost:8000/api/stats/summary?from_time=2026-02-28T03:29:06.216Z"
```

Previously returned `500 Internal Server Error`.

### Root Cause

Timezone-aware parsed query datetimes (`...Z`) were being passed into comparisons against timezone-naive DB datetimes (`created_at`) in stats filters.

### Fix Implemented

- Added datetime normalization in `backend/app/routers/stats.py` to convert incoming aware datetimes to UTC-naive before service filtering.
- Applied normalization for all stats endpoints that accept datetime filters (`/requests`, `/summary`, `/connection-success-rates`, `/spending`).
- Result: `from_time=...Z` requests now return `200` and include CORS headers on successful responses.
  - `frontend/src/pages/StatisticsPage.tsx`
  - `frontend/src/pages/RequestLogsPage.tsx`

## Profile Isolation Validation - Passed

### Endpoint Data Isolation

Created profile-specific endpoints:

- Profile 7: `ISO-A-ONLY-1772336045`
- Profile 8: `ISO-B-ONLY-1772336045`

Observed in UI and API:

- When selected profile is 7 (`ISO-B-1772319229`), only profile-7 endpoint set is shown.
- When selected profile is 8 (`Dummyprofile`), only profile-8 endpoint set is shown.

### Settings Isolation

Updated per-profile costing settings:

- Profile 7 -> `EUR`, `€`, timezone `Europe/Berlin`
- Profile 8 -> `USD`, `$`, timezone `America/New_York`

Verified via API and UI for profile 8 settings page:

- Profile 8 values persisted and rendered correctly.
- Profile 7 retained distinct values via API fetch.

## Notes

- The apparent browser "CORS blocked" errors are secondary effects of backend 500 responses on stats endpoints for `Z` timestamps.
- CORS is functioning on successful responses.

## Post-Fix Verification

Confirmed with direct API checks after the fix:

```bash
curl -i -H "Origin: http://localhost:3000" \
  "http://localhost:8000/api/stats/requests?from_time=2026-02-28T03:29:06.216Z&limit=50"

curl -i -H "Origin: http://localhost:3000" \
  "http://localhost:8000/api/stats/summary?from_time=2026-02-28T03:29:06.216Z"
```

Both now return `200 OK`.

## Evidence Artifacts

- `~/Downloads/round2-profile7-endpoints-2026-03-01T03-36-46-382Z.png`
- `~/Downloads/round2-profile8-settings-2026-03-01T03-37-42-683Z.png`
