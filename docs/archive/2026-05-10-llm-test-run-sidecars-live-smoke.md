# 2026-05-10 Sidecars Live Smoke Test Run

## Scope
- Local Prism page: `http://localhost:15173/sidecars`.
- Live sidecar under test: `CPA` at `http://192.168.1.222:8317`.
- Constraint: read-only live testing except allowed sidecar priority changes. No implementation fixes were made.
- Secret handling: management password was not written to this note.

## Acceptance Criteria
- Prism sidecars page loads and shows the registered sidecar.
- Connection test succeeds through Prism backend APIs.
- Manual sync succeeds through Prism backend APIs.
- Auth inventory, provider inventory, retired policy panel, and retired activity panel could be read during this historical run.
- Priority mutation is attempted only if auth inventory exposes a safe auth target.
- Implemented sidecar regression/function tests pass.

## Live Smoke Results
- Page load: passed. Chrome DevTools snapshot showed the Sidecars route, one healthy sidecar, and detail panels for auth files, provider inventory, retired policy, and retired activity.
- Sidecar list API: passed. `GET /api/sidecars` returned one enabled sidecar with `management_auth_state: "valid"`, masked credential state, and private-network/HTTP flags visible.
- Connection test: passed. `POST /api/sidecars/1/test-connection` returned `state: "succeeded"`, `management_auth_state: "valid"`, `status_code: 200`.
- Manual sync: passed with partial inventory. `POST /api/sidecars/1/sync` returned `state: "succeeded"`, `auth_snapshot_count: 0`, `provider_snapshot_count: 4`.
- Browser action smoke: passed for allowed read/sync actions. Clicking `Test connection` showed `Connection to CPA succeeded with HTTP 200`; clicking `Sync now` showed `Manual sync accepted for CPA`.
- Console/network: passed. Chrome DevTools reported no console warnings/errors; fetch/XHR calls to sidecar APIs returned HTTP 200.

## Confirmed Issue
- `GET /api/sidecars/1/auth-files` and `GET /api/sidecars/1/auth-snapshots` both returned `{"items":[]}` after successful sync.
- At the same time, `GET /api/sidecars/1/provider-snapshots` returned 4 masked `codex-api-key` provider entries with provider item keys and priority metadata.
- The UI therefore shows `No auth snapshots` even though provider inventory displays configured sidecar auth/provider entries.
- Evidence screenshot: `docs/archive/2026-05-10-sidecars-live-smoke.png`.

## Root-Cause Evidence Collected
- Historical finding: the sync code was reading `/auth-files` with a stale local envelope mapping, so it missed the live CLIProxyAPI top-level `files` array.
- The finding was merged into the live owner: `backend/internal/httpapi/management/sidecars/AGENTS.md` now owns the `/auth-files` envelope rule.
- The same sync pass separately reads provider-specific endpoints and successfully normalizes provider snapshots from `/codex-api-key` and related provider routes.
- Live evidence showed provider inventory while the auth snapshot path stayed empty, which identified an auth-files envelope mismatch rather than provider inventory being the supported fallback.
- Because no auth snapshots existed during this historical run, there was no safe auth target for the allowed priority-adjustment mutation.

## Function Test Results
- `cd backend && go test -count=1 ./internal/httpapi/management/sidecars`: passed.
- `cd backend && go test -count=1 ./tests/contract ./tests/integration ./tests/priority/...`: passed.
- `cd frontend && pnpm run test:e2e -- --grep sidecars`: passed, 4 tests.

## Not Exercised Live
- Create, edit, delete, enable/disable, retired policy save, and auth disable/enable were not executed against the live sidecar because they exceed the read/priority-only testing constraint.
- Auth priority mutation was not executed because the live sync produced zero auth snapshots, so Prism exposed no auth row to mutate.
- These paths remain covered by the automated sidecar tests listed above.

## Follow-Up Resolution
- This historical gap is addressed by the current backend sidecars contract documented in `backend/internal/httpapi/management/sidecars/AGENTS.md`.
- Later evidence for the fix is stored under `.sisyphus/evidence/cliproxyapi-live-sync.txt`, `.sisyphus/evidence/cliproxyapi-read-only-sync.txt`, and `.sisyphus/evidence/cliproxyapi-auth-table-large.png`.
- Priority adjustment was not smoked in this archived run because the stale envelope mapping produced no auth snapshots at that time.

## Files Not Modified Outside Docs
- This run intentionally changed only docs/archive evidence files.
- No backend or frontend implementation fixes were applied.
