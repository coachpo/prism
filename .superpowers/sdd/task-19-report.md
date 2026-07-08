### Task 19 Report

Status: DONE_WITH_CONCERNS

Implemented `POST /api/pricing-templates/import` for Default-profile pricing-template JSON import.

Backend:
- Added `/api/pricing-templates/import` route and handler.
- Supports `mode: "upsert_by_name"` and `mode: "create_only"`.
- Uses `DisallowUnknownFields` through the existing `decodeJSONBody`.
- Reuses existing pricing-template normalization and creation helpers.
- Validates all rows before DB writes; invalid rows return `400` and no templates are created or updated.
- Commits imports in one transaction.
- Updates existing templates by normalized name for `upsert_by_name`; skips existing names for `create_only`.
- Added the required `management_route_contract.json` row with `invalidates_planning: true`.
- Updated runtime cache invalidation and admission route specs for the new route.

Frontend:
- Added `/route/pricing` JSON import dialog with paste/upload, mode select, client `JSON.parse`, typed API call, refresh, and result toast.
- Added typed import request/response contracts.
- Added missing-template badge on model detail terminal-target rows when `pricing_template_id === null`.
- Added English and Chinese i18n keys under `pricing.*`.

Docs:
- Documented the import endpoint in `docs/API_SPEC.md`.
- Updated `backend/internal/httpapi/management/connections/AGENTS.md`.

Verification:
- TDD red: `cd backend && go test ./internal/httpapi/management/connections -run TestPricingTemplateImportRouteUpsertValidationAndUnknownFields -count=1` failed before implementation with `undefined: pricingTemplateImportResponse`.
- `cd backend && go test ./internal/httpapi/management/connections -count=1` passed.
- `cd backend && go test ./internal/platform/http -count=1` passed.
- `cd backend && go test ./tests/contract -count=1` passed.
- `cd backend && go build ./cmd/prism-backend` passed.
- `cd frontend && pnpm run test -- --run` passed.
- `cd frontend && pnpm run test:lib` passed.
- `cd frontend && pnpm run test:server` passed.
- `cd frontend && pnpm run build` passed.
- `cd frontend && pnpm run lint` passed.
- Curl import smoke against `./start.sh headless` with isolated `PRISM_CONFIG_PATH=artifacts/evidence/task-19-curl-config.json`:
  - first import returned `{"created":2,"updated":0,"skipped":[],"errors":[]}` with HTTP 200.
  - second import returned `{"created":0,"updated":2,"skipped":[],"errors":[]}` with HTTP 200.

Concerns:
- `cd backend && go test ./tests/priority/... -count=1` failed in `backend/tests/priority/cache`: `runtime cache generation implementation missing "LoadFreshActiveRuntimePlan"`. This test inspects runtime cache source text and is unrelated to Task 19 changes.
- `cd backend && go test ./tests/runtime -count=1` failed on existing runtime-suite issues: route matrix expected 11 POST operations but got 9, and `TestRuntimePhase1Snapshot_PinsPlanningToDefaultProfile` hit a SQL parameter type error. Task 19 did not touch runtime operation registration or the failing snapshot test.
- `cd backend && go test ./tests/integration -run TestBackendDockerfileContract -count=1` passed with `[no tests to run]`; full integration suite was not rerun after the longer runtime/priority failures.
