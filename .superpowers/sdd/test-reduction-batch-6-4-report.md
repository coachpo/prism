# Test Reduction Batch 6.4 Report

## Scope

- Branch: `codex/prism-core-simplification`
- Base HEAD: `bb86220f`
- Touched only:
  - `backend/tests/integration/management_audit_stats_phase7_test.go`
  - `backend/tests/contract/s15_observability_contract_test.go`

## Result

Compressed Phase 7 audit/stats integration coverage by removing tests duplicated by S15 observability contract coverage and moving the Phase 7-only dashboard cache freshness/invalidation checks into S15.

## C6 Counts

| File | Before LOC | After LOC | Before `func Test` | After `func Test` |
|---|---:|---:|---:|---:|
| `backend/tests/integration/management_audit_stats_phase7_test.go` | 1650 | 846 | 33 | 19 |
| `backend/tests/contract/s15_observability_contract_test.go` | 1575 | 1648 | 36 | 37 |
| Total | 3225 | 2494 | 69 | 56 |

Net: -731 LOC and -13 top-level tests.

## Coverage Mapping

| Removed / merged Phase 7 test | Coverage destination |
|---|---|
| `TestManagementAuditKeysetPagination` | `TestManagementAuditCursorIntegrity` in S15 covers HTTP keyset pagination, next cursor continuation, and tamper rejection. |
| `TestManagementDashboardStatsRouteReturnsAggregateSnapshot` | `TestManagementDashboardStatsReturnsCanonicalSnapshotWithoutWindow` and `TestManagementDashboardStatsSnapshotSections` in S15 cover canonical dashboard shape, legacy-key absence, and seeded aggregate snapshot sections. |
| `TestDashboardStatsEmptyProfileStatsOnlyContract` | `TestManagementDashboardStatsReturnsCanonicalSnapshotWithoutWindow` and `TestManagementDashboardHealthReportsSnapshotFreshness` in S15 cover empty/default dashboard shape and health metadata. |
| `TestDashboardRecentActivityDoesNotPolluteSnapshot` | S15 canonical dashboard shape asserts the stats snapshot excludes legacy/recent-activity fields; Phase 7 still keeps recent-activity route coverage in `TestDashboardRecentActivityEmptyContract` and `TestDashboardRecentActivityBoundedContract`. |
| `TestDashboardSnapshotStatsOnlyBuilderWithUsageEventsWithoutRequestLogs` | S15 usage-event observability coverage lives in `TestUsageSnapshot`, `TestManagementDashboardStatsSnapshotSections`, and spending/model-stat contract tests. |
| `TestDashboardSnapshotIgnoresRequestLogOnlyFixtures` | S15 separates dashboard usage-event contracts from request-log summary/throughput contracts via `TestManagementDashboardStatsSnapshotSections`, `TestStatsSummary`, and `TestThroughput`. |
| `TestStatsSummaryFromUsageEvents` | S15 stats/usage coverage: `TestUsageSnapshot`, `TestStatsSummary`, and dashboard snapshot section assertions. |
| `TestThroughputFromUsageEvents` | S15 throughput coverage: `TestThroughput` plus dashboard snapshot contract coverage. |
| `TestUsageEventAggregateMixedOutcomes` | S15 mixed priced/unpriced/error coverage: `TestUsageSnapshot`, `TestSpending`, and `TestObservabilityTreatsSuccessfulMissingCostRowsAsUnpriced`. |
| `TestManagementAuditStatsTopologyGraphDistinguishesTerminalRouteAndEndpointBinding` | `TestObservabilityDashboardTopologyGraphIncludesDisabledAndInactiveNodes` in S15 covers disabled/inactive nodes, terminal target edge kinds, telemetry, and health-status omission. |
| `TestManagementDashboardStatsKeepsCachedAggregateAtStaleThreshold` | Merged into new S15 `TestManagementDashboardStatsCacheFreshnessModes`. |
| `TestManagementDashboardStatsRebuildsStaleCachedAggregate` | Merged into new S15 `TestManagementDashboardStatsCacheFreshnessModes`. |
| `TestDashboardSnapshotInvalidationEvictsCachedProfiles` | Merged into new S15 `TestManagementDashboardStatsCacheFreshnessModes`. |
| `TestManagementRolloutRejectsLegacyUnboundedAuditRequests` | `TestManagementAuditListRequiresWindow` in S15 covers the shipped HTTP contract for bounded audit windows. |

## Retained Phase 7 Coverage

- Log-retention partition drop and global route/cancel behavior.
- Dashboard recent-activity route empty/bounded/limit behavior.
- Usage-event-backed routing health and topology telemetry.
- Structured management job logging.
- Audit delete job chunking, retry, active/stale lease, resume, cancel, and job-audit preservation.

## Verification

- `cd backend && go test ./tests/integration -run 'Audit|Dashboard|Stats|LogRetention' -count=1`
  - `ok  	github.com/coachpo/prism/backend/tests/integration	45.001s`
- `cd backend && go test ./tests/contract -run 'S15|Observability|Dashboard|Stats|Audit' -count=1`
  - `ok  	github.com/coachpo/prism/backend/tests/contract	11.825s`
- `cd backend && go test -count=1 ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...`
  - `ok  	github.com/coachpo/prism/backend/tests/contract	79.331s`
  - `ok  	github.com/coachpo/prism/backend/tests/integration	126.278s`
  - `ok  	github.com/coachpo/prism/backend/tests/runtime	113.285s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority	3.574s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority/admission	2.086s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority/db	1.300s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority/load	1.537s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority/outbox	1.033s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority/sideeffects	1.317s`
  - `ok  	github.com/coachpo/prism/backend/tests/priority/unit	1.308s`
- `cd backend && go test -count=1 ./internal/... ./cmd/...`
  - passed; all packages reported `ok` or `[no test files]`
- `cd backend && go build ./cmd/prism-backend`
  - passed with no output

## Concerns

- None.
