# Task 18 Report

## Summary
- Added backend request-log list filters for `priced=true|false` and exact `unpriced_reason`.
- Added request-log frontend URL state, route schema, API params, and filter controls for pricing state and unpriced reason.
- Linked the dashboard 30-day unpriced count to `/observe/requests?priced=false&time_range=30d`.
- Updated request-log docs, frontend request-log ownership notes, and focused regression coverage.

## Step Notes
- Step 0: `rg -n "unpricedBreakdown" frontend/src` found the existing locale key in `frontend/src/i18n/messages/en.ts` and `frontend/src/i18n/messages/zh-CN.ts`. `UsageBreakdownSection.tsx` already renders unpriced request counts and spend-trust messaging, so this task stayed scoped to drill-down links and filters.
- Step 1: Added failing backend coverage in `backend/tests/runtime/request_logs_contract_test.go` before production changes. The red run failed because `priced` and `unpriced_reason` were ignored and invalid values returned 200.
- Step 2: Added `RequestLogListParams.PricedFlag` and `RequestLogListParams.UnpricedReason`, query parsing/validation in management stats, and SQL WHERE clauses in the stats request-log builder.
- Step 3: Converted the dashboard unpriced count detail into a TanStack `Link` targeting the filtered request-log route.
- Step 4: Added frontend pricing filter URL state, router validation, fetch params, and filter UI. The unpriced reason dropdown is only enabled when `priced=false`.
- Step 5: Added request-log i18n labels, updated `docs/API_SPEC.md`, and refreshed `frontend/src/pages/request-logs/AGENTS.md`.
- Step 6: Focused automated verification passed. Live curl checks could not run because no backend was listening on `127.0.0.1:8000`.

## Verification
- RED backend: `cd backend && go test ./tests/runtime -run TestRequestLogListPricingFilters -count=1` failed as expected before implementation.
- RED frontend: `cd frontend && node --test tests/lib/request_log_filter_state_contract.test.mjs` failed on missing `priced` state; `node --test tests/lib/dashboard_contract.test.mjs` failed on missing dashboard link.
- GREEN focused backend: `cd backend && go test ./tests/runtime -run 'TestRequestLogList(PricingFilters|StatusAndErrorFilters)' -count=1` passed.
- Backend request-log suite: `cd backend && go test ./tests/runtime -run TestRequestLog -count=1` passed.
- Backend package checks: `cd backend && go test ./internal/domain/stats ./internal/httpapi/management/stats` passed.
- Backend build: `cd backend && go build ./cmd/prism-backend` passed.
- Frontend lib contracts: `cd frontend && pnpm run test:lib` passed.
- Frontend unit tests: `cd frontend && pnpm run test` passed.
- Frontend lint: `cd frontend && pnpm run lint` passed.
- Frontend build: `cd frontend && pnpm run build` passed with existing Vite chunk-size warnings.
- Diff hygiene: `git diff --check` passed.

## Concerns
- `cd backend && go test ./tests/runtime` failed outside this task's changed surface:
  - `TestRuntimeOperationRouteMatrixSupportedOperations`: route matrix expected 11 registered POST operations but got 9.
  - `TestRuntimePhase1Snapshot_PinsPlanningToDefaultProfile`: existing fixture insert failed with PostgreSQL `inconsistent types deduced for parameter $1`.
- Curl verification failed because the local backend was not running on `127.0.0.1:8000`.

## Reviewer Fix 2026-07-08
- Replaced request-log `priced` parsing with trimmed exact lowercase `true`/`false` acceptance. Values like `TRUE` and `1` now return 400.
- Added regression coverage for `priced=TRUE` and `priced=1`.

## Reviewer Verification 2026-07-08
- RED: `cd backend && go test ./tests/runtime -run TestRequestLogListPricingFilters -count=1` failed on `priced=TRUE` and `priced=1` returning 200.
- GREEN: `cd backend && go test ./tests/runtime -run TestRequestLogListPricingFilters -count=1` passed.
- `cd backend && go test ./internal/httpapi/management/stats` passed.
- `git diff --check` passed.

## Reviewer Fix 2 2026-07-08
- Updated request-log `priced` and `unpriced_reason` SQL filters to use the same normalized pricing semantics as returned list rows. Rows stored with `priced_flag=true` but missing user-currency cost now filter as unpriced with `MISSING_PRICE_DATA`.
- Extended `TestRequestLogListPricingFilters` with a raw-priced missing-cost row covering `priced=false`, `priced=true`, and `unpriced_reason=MISSING_PRICE_DATA`.

## Reviewer Fix 2 Verification 2026-07-08
- RED: `cd backend && go test ./tests/runtime -run TestRequestLogListPricingFilters -count=1` failed on the raw-priced missing-cost row being excluded from `priced=false` and `unpriced_reason=MISSING_PRICE_DATA`, and included in `priced=true`.
- GREEN: `cd backend && go test ./tests/runtime -run TestRequestLogListPricingFilters -count=1` passed.
- `cd backend && go test ./internal/domain/stats ./internal/httpapi/management/stats -count=1` passed.
- `cd backend && go test ./tests/runtime -run TestRequestLog -count=1` passed.
- `git diff --check` passed.
