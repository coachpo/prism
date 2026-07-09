# Task 20 Report: Latency p50/p95 Trends

## Summary
- Added `latency_trends` to `GET /api/stats/usage-snapshot` with hourly and daily p50/p95 `response_time_ms` buckets.
- Rendered a new Latency Trends chart in the analytics usage trends grid using the existing `UsageTrendChart` component.
- Updated frontend types, persisted chart granularity state, i18n, API docs, and ownership notes.

## Precheck
- `rg -n "type snapshotEvent" -A 25 backend/internal/domain/stats/snapshot.go` returned no match because `snapshotEvent` now lives in `backend/internal/domain/stats/common.go`.
- `snapshotEvent` already carries `ResponseTimeMS *int`.
- `loadUsageEventRecords` already selects `usage_request_events.response_time_ms` and `scanUsageEventRecord` already hydrates it, so no query change was needed.

## TDD
- Added `backend/internal/domain/stats/snapshot_latency_trends_test.go` first.
- Red run:
  - `cd backend && go test ./internal/domain/stats -run TestBuildLatencyTrendSeriesAlignsBucketsAndPercentiles -count=1`
  - Failed as expected with `undefined: buildLatencyTrendSeries`.
- Green runs passed after implementation.

## Dataviz Skill
- Consulted `ui-ux-pro-max` chart guidance with:
  - `python3 /Users/qingli/.agents/skills/ui-ux-pro-max/scripts/search.py "analytics dashboard latency p50 p95 trend" --domain chart -n 5`
- Result recommended a time-series line/area chart for trend data. I followed the existing Recharts area-chart pattern to avoid new chart code.

## Implementation Notes
- Backend:
  - Added `UsageLatencyTrendPoint`, `UsageLatencyTrendSeries`, and `UsageLatencyTrends`.
  - Added `UsageSnapshotResponse.LatencyTrends`.
  - Added `buildLatencyTrendSeries` and `makeUsageLatencyTrendPoints`.
  - Percentiles use existing `percentileContInt`.
  - Empty buckets keep their bucket and return null p50/p95.
- Frontend:
  - Added `latency_trends` TypeScript contracts.
  - Added `latencyTrends` chart granularity state.
  - Localized latency series labels alongside request/token trend labels.
  - Added a Latency Trends card to `UsageTrendsSection` with p50 and p95 as the two existing chart series.
- Docs:
  - Updated `docs/API_SPEC.md`.
  - Updated stats and statistics page AGENTS notes.

## Verification
- `cd backend && go test ./internal/domain/stats -run TestBuildLatencyTrendSeriesAlignsBucketsAndPercentiles -count=1` -> pass.
- `cd backend && go test ./tests/contract -run TestUsageSnapshot -count=1` -> pass.
- `cd backend && go test ./internal/domain/stats ./tests/contract -run 'TestBuildLatencyTrendSeriesAlignsBucketsAndPercentiles|TestUsageSnapshot' -count=1` -> pass.
- `cd frontend && pnpm run test -- src/pages/statistics/useUsageStatisticsPageData.test.tsx` -> pass, 15 files / 38 tests.
- `cd frontend && pnpm run lint` -> pass.
- `cd frontend && pnpm run build` -> pass; Vite reported existing chunk-size warnings.
- `cd frontend && pnpm run test:e2e -- shared-chart-statistics.spec.ts` -> pass, 6 tests.

## Notes
- I did not run the manual `curl /api/stats/usage-snapshot | jq ...` check because no local Prism backend server was started for this task. The backend contract and domain tests cover the same response field and bucket behavior.
- An attempted `pnpm run test:lib -- --run src/pages/statistics/useUsageStatisticsPageData.test.tsx` was the wrong runner for TSX Vitest tests and failed with `ERR_UNKNOWN_FILE_EXTENSION`; the correct Vitest command above passed.
