# Task 21 Report

## Summary

Implemented the failover incidents read endpoint, dashboard incident banner, and alert webhook delivery through a durable background outbox.

## Backend

- Added `GET /api/loadbalance/incidents` as a profile-scoped management read route.
- Added `loadbalance.ListIncidents` with recent incident events and active runtime ban snapshots.
- Extended runtime state with `SnapshotActiveBans`.
- Added `alert_webhook_outbox` migration and `backend/internal/platform/alerting` worker.
- Enqueued alert webhook rows in the runtime feedback transaction for `banned`, `unbanned`, and `recovered` events only.
- Wired the alert webhook outbox through production background-job lanes and background scheduler.
- Added hot-applied `alerting.webhookUrl` bootstrap config validation. Empty disables alerting; non-empty values must be `http` or `https`.
- Updated management route contracts, migration inventory, and backend contract/unit/priority tests.

## Frontend

- Added typed API support for `/api/loadbalance/incidents`.
- Added dashboard bootstrap fetch of recent incidents.
- Added an overview warning banner when active bans or recent failover signals exist.
- Banner action opens the Ban Policies page, where loadbalance events are visible.
- Added English and Simplified Chinese dashboard copy.
- Updated frontend seam contracts for dashboard bootstrap, incident banner rendering, and profile scoping.

## Docs

- Updated `docs/API_SPEC.md` with the incidents endpoint.
- Updated `docs/DATA_MODEL.md` with `alert_webhook_outbox`.
- Updated `docs/ARCHITECTURE.md` to describe durable webhook alerting.

## Verification

Passed:

- `cd backend && go test -count=1 ./internal/platform/config ./internal/httpapi/runtime ./internal/httpapi/management/loadbalance ./internal/platform/alerting ./internal/domain/loadbalance ./internal/platform/lifecycle ./internal/platform/http ./tests/contract ./tests/integration ./tests/priority/outbox`
- `cd backend && go build ./cmd/prism-backend`
- `cd frontend && pnpm run test:lib`
- `cd frontend && pnpm run build`
- `cd frontend && pnpm run lint`
- `git diff --check`

Known failing broader checks:

- `cd backend && go test -count=1 ./tests/runtime`
  - `TestRuntimeOperationRouteMatrixSupportedOperations`: expected 11 registered POST operations, got 9.
  - `TestRuntimePhase1Snapshot_PinsPlanningToDefaultProfile`: PostgreSQL reports inconsistent parameter types while inserting `Phase1 Shadow Profile`.
  - Several runtime telemetry close warnings appeared before suite failure.
- `cd backend && go test -count=1 ./tests/priority/...`
  - `tests/priority/cache TestGenerationInvalidationRace`: runtime cache generation implementation missing `LoadFreshActiveRuntimePlan`.
  - Other priority packages passed, including the new outbox priority package.

## Concerns

- The broader runtime and priority failures are outside the Task 21 surfaces and were not fixed here.
- Live manual `curl`, `nc`, and browser banner checks were not run. The endpoint, outbox worker, webhook POST, and dashboard banner are covered by automated contract/unit/frontend checks.
- Known unrelated dirty/untracked files were left unstaged and unchanged by this task.
