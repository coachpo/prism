# Task 17 Report

Status: DONE_WITH_CONCERNS

## Summary
- Added request-log backend filters for `status_family=2xx`, exact `status_code`, and `error_text` substring matching against `error_detail`.
- Switched request-log and usage-statistics frontend defaults from `1h` to `24h`; default request-log URLs now omit `time_range=24h`.
- Added request-log UI controls for 2xx, status code, and error text filters.
- Added current-page CSV export for request-log table rows.
- Updated i18n, API docs, request-log page ownership docs, and frontend route/filter contract tests.

## TDD
- Added `TestRequestLogListStatusAndErrorFilters` before backend implementation.
- Red result:
  - `go test ./tests/runtime -run TestRequestLogListStatusAndErrorFilters -count=1`
  - Failed as expected because all three filters returned all seeded rows.
- Green result:
  - `go test ./tests/runtime -run TestRequestLogListStatusAndErrorFilters -count=1`
  - Passed.
- Added frontend route/filter tests before frontend implementation.
- Red result:
  - `pnpm exec vitest run src/test/route-helpers.test.ts && node --test tests/lib/request_log_filter_state_contract.test.mjs`
  - Failed as expected with `status=success` falling back to `all` and `time_range` falling back to `1h`.
- Green result:
  - Same command passed after frontend implementation.

## Verification
- `go test ./tests/runtime -run 'TestRequestLogList(StatusAndErrorFilters|TimeWindowHonorsToTime)' -count=1` - PASS.
- `go test ./internal/domain/stats ./internal/httpapi/management/stats` - PASS.
- `go build ./cmd/prism-backend` - PASS.
- `pnpm exec vitest run src/test/route-helpers.test.ts` - PASS.
- `node --test tests/lib/request_log_filter_state_contract.test.mjs` - PASS.
- `pnpm run test:lib` - PASS, 72 tests.
- `pnpm run test -- run` - PASS, 14 files / 37 tests.
- `pnpm run test:server` - PASS, 4 tests.
- `pnpm run build` - PASS.
- `pnpm run lint` - PASS.
- `git diff --check` - PASS.

## Full Suite Concern
The standard backend regression command was run and failed in areas not touched by Task 17:

`go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...`

Results:
- `tests/contract` - PASS.
- `tests/integration` - PASS.
- `tests/runtime` - FAIL:
  - `TestRuntimeOperationRouteMatrixSupportedOperations`: route matrix expected 11 registered POST operations, got 9.
  - `TestRuntimePhase1Snapshot_PinsPlanningToDefaultProfile`: SQL parameter type mismatch while inserting a profile.
- `tests/priority/cache` - FAIL:
  - `TestGenerationInvalidationRace`: expected runtime cache generation implementation to contain `LoadFreshActiveRuntimePlan`.

These failures are outside the Task 17 request-log filter/default/export surface.

## Files Changed
- `.superpowers/sdd/task-17-report.md`
- `backend/internal/domain/stats/types.go`
- `backend/internal/domain/stats/request_logs.go`
- `backend/internal/httpapi/management/stats/service.go`
- `backend/tests/runtime/request_logs_contract_test.go`
- `docs/API_SPEC.md`
- `frontend/src/app/router/appRouter.tsx`
- `frontend/src/app/router/rewriteRoutes.ts`
- `frontend/src/i18n/messages/en.ts`
- `frontend/src/i18n/messages/zh-CN.ts`
- `frontend/src/lib/types/model-stats.ts`
- `frontend/src/pages/request-logs/AGENTS.md`
- `frontend/src/pages/request-logs/FiltersBar.constants.ts`
- `frontend/src/pages/request-logs/FiltersBarPrimaryFilters.tsx`
- `frontend/src/pages/request-logs/RequestLogsTable.tsx`
- `frontend/src/pages/request-logs/queryParams.ts`
- `frontend/src/pages/request-logs/requestLogsCsv.ts`
- `frontend/src/pages/request-logs/useRequestLogPageState.ts`
- `frontend/src/pages/request-logs/useRequestLogsPageData.ts`
- `frontend/src/pages/statistics/usageStatisticsStorage.ts`
- `frontend/src/test/route-helpers.test.ts`
- `frontend/tests/lib/request_log_filter_state_contract.test.mjs`
