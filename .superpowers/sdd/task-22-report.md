# Task 22 Report

## Status

Implemented Task 22 on branch `codex/prism-core-simplification` from base commit `dae7a94c`.

Required commit message:

```text
fix!: enforce spend coherence at write time, drop dead stat rollup tables
```

## Requirements Coverage

- Step 1: Moved spend coherence to the runtime write path with `enforceRuntimeSpendCoherence`, applied after pricing snapshot persistence enrichment and before both request-log and usage-event inserts consume the shared pricing result.
- Step 1: Removed read-side request log, dashboard recent activity, usage snapshot, spending, and endpoint/model normalization patches.
- Step 2: Added `000009_stats_write_coherence.sql` to backfill historical `request_logs` and `usage_request_events` rows to the same priced/unpriced/FX coherence contract.
- Step 3: Added `000010_drop_management_stat_rollups.sql`, removed the dead dashboard rollup implementation, and kept dashboard freshness/coverage helper types in `dashboard_health.go`.
- Step 3: Removed rollup-only integration and priority assertions.
- Step 4: Added the requested `ponytail` comment at the request-log list count.
- Step 5: Verified the requested `rg` checks are zero outside migration history.

## Implementation Notes

- Runtime coherence rules now run once on `runtimePricingResult`, so `request_logs` and `usage_request_events` receive the same `priced_flag`, `unpriced_reason`, and `fx_rate_*` state.
- Cost-bearing successful billable rows are forced to `priced=true`; same-currency rows default missing FX to `1` / `DEFAULT_1_TO_1`.
- Explicit unpriced reasons win, are trimmed, force `priced=false`, and clear FX fields.
- Successful billable rows marked priced without a persisted user-currency total degrade to `priced=false` with `MISSING_PRICE_DATA`.
- Historical migration trims explicit reasons, fixes priced state, and clears or defaults FX snapshots using the same persisted-row coherence contract.
- The dead rollup table code was deleted; the final schema drops `management_stat_buckets` and `management_stat_refresh_state`.

## TDD Evidence

- Added `TestEnforceRuntimeSpendCoherence` first; it failed before the helper existed, then passed after the write-side helper was implemented.
- Added `TestStatsWriteCoherenceMigrationBackfillsHistoricalRows` first; it failed before migrations `000009` and `000010` existed, then passed after the migrations were added.
- Extended runtime/request-log coverage so a runtime path with priced-but-no-cost persistence returns `priced=false` / `MISSING_PRICE_DATA` without read-side normalization.

## Verification

Passed:

```bash
cd backend && go test ./internal/domain/stats ./internal/httpapi/runtime -count=1
cd backend && go test ./tests/priority/unit ./tests/priority/auditstats -count=1
cd backend && go build ./cmd/prism-backend
git diff --check
rg -n "normalizeObserved|normalizeUsageEvent|normalizeRequestLogListSpendState" backend/internal/domain/stats
rg -n "management_stat_buckets|management_stat_refresh_state|LoadDashboardRollupStats|RefreshDashboardStatsRollup" backend --glob '!backend/migrations/000001_initial_schema.sql' --glob '!backend/migrations/000010_drop_management_stat_rollups.sql'
rg -n "management_stat_buckets|management_stat_refresh_state|LoadDashboardRollupStats|RefreshDashboardStatsRollup" backend/internal backend/tests
```

Fresh Docker/Postgres-backed verification attempted but blocked by the local container harness not producing ready Postgres containers:

```bash
cd backend && go test ./tests/integration -run 'TestSingleBaselineAppliesToFreshDatabase|TestBaselineSecondRunNoop|TestStatsWriteCoherenceMigrationBackfillsHistoricalRows' -count=1
# FAIL: postgres container on ports 33869, 33870, and 33871 did not become ready in time

cd backend && go test ./tests/runtime -run 'TestRequestLogListContract|TestRequestLogListPricingFilters|TestRuntimeRequestLogPreservesUnpricedPricingPathways|TestRuntimeRequestLogDegradesInvalidUsedConcreteComponentPrice' -count=1
# FAIL: postgres container on port 33872 did not become ready in time

cd backend && go test ./tests/contract -run 'TestEndpointModelStatistics|TestObservabilityTreatsSuccessfulMissingCostRowsAsUnpriced' -count=1
# FAIL: postgres container on port 33873 did not become ready in time
```

Follow-up verification after pruning stale Prism test containers and unused Docker volumes:

```bash
cd backend && go test ./tests/integration -run 'TestSingleBaselineAppliesToFreshDatabase|TestBaselineSecondRunNoop|TestStatsWriteCoherenceMigrationBackfillsHistoricalRows' -count=1
# PASS

cd backend && go test ./tests/runtime -run 'TestRequestLogListContract|TestRequestLogListPricingFilters|TestRuntimeRequestLogPreservesUnpricedPricingPathways|TestRuntimeRequestLogDegradesInvalidUsedConcreteComponentPrice' -count=1
# PASS

cd backend && go test ./tests/contract -run 'TestEndpointModelStatistics|TestObservabilityTreatsSuccessfulMissingCostRowsAsUnpriced' -count=1
# PASS
```

Earlier targeted Docker-backed red/green runs passed before the local Postgres harness started failing readiness:

```bash
cd backend && go test ./tests/integration -run TestStatsWriteCoherenceMigrationBackfillsHistoricalRows
cd backend && go test ./tests/integration -run 'TestSingleBaselineAppliesToFreshDatabase|TestBaselineSecondRunNoop|TestStatsWriteCoherenceMigrationBackfillsHistoricalRows'
cd backend && go test ./tests/runtime -run 'TestRequestLogListContract|TestRequestLogListPricingFilters|TestRuntimeRequestLogPreservesUnpricedPricingPathways|TestRuntimeRequestLogDegradesInvalidUsedConcreteComponentPrice'
cd backend && go test ./tests/contract -run 'TestEndpointModelStatistics|TestObservabilityTreatsSuccessfulMissingCostRowsAsUnpriced' -count=1
```

## Changed Files

- `.superpowers/sdd/task-22-report.md`
- `backend/internal/domain/stats/AGENTS.md`
- `backend/internal/domain/stats/aggregates.go`
- `backend/internal/domain/stats/dashboard_health.go`
- `backend/internal/domain/stats/dashboard_recent_activity.go`
- `backend/internal/domain/stats/request_logs.go`
- `backend/internal/domain/stats/rollups.go`
- `backend/internal/domain/stats/snapshot.go`
- `backend/internal/httpapi/runtime/observability.go`
- `backend/internal/httpapi/runtime/runtime_pricing.go`
- `backend/internal/httpapi/runtime/runtime_test.go`
- `backend/migrations/000009_stats_write_coherence.sql`
- `backend/migrations/000010_drop_management_stat_rollups.sql`
- `backend/tests/contract/s15_observability_contract_test.go`
- `backend/tests/integration/management_audit_stats_phase7_test.go`
- `backend/tests/integration/migrations_test.go`
- `backend/tests/priority/auditstats/auditstats_priority_test.go`
- `backend/tests/priority/unit/unit_priority_contract_test.go`
- `backend/tests/runtime/request_logs_contract_test.go`
- `docs/DATA_MODEL.md`
- `docs/DEVELOPMENT_DIRECTION.md`

## Concerns

- The local Docker/Postgres harness readiness issue was resolved by pruning stale Prism test containers and unused Docker volumes; the focused migration/runtime/contract database-backed commands now pass.
- No production `pg_dump` or production dry-run impact SELECT was run from this local task environment; that remains an operator preflight before applying `000009_stats_write_coherence.sql` to live data.
