# Settings Statistics Cleanup

## Status

Approved by Momus on 2026-04-05.

## Requirement Summary

- Add a new cleanup action beside request logs, loadbalance events, and audit logs from the settings retention and deletion flow.
- The new action must clean persisted statistics data without reusing the existing request-log cleanup path.

## Decision Summary

- Keep the settings shell thin and extend the existing helper, hook, section, dialog, and typed API boundaries.
- Treat "statistics data" as the persisted usage-statistics rows that power the unified statistics snapshot and endpoint-model breakdowns, centered on `UsageRequestEvent`.
- Keep request-log cleanup separate so the existing `requests` option continues to govern `RequestLog` deletion only.
- Add a dedicated backend cleanup contract for statistics persistence with the same retention and `delete_all` semantics as the existing batch delete actions.
- Expect both `frontend` and `backend` submodules to change, followed by the root gitlink updates.

## Worktree and Branch Record

- Root worktree: `/Users/liqing/Documents/PersonalProjects/prism-workspace/model-share-feature-settings-clean-statistics-data`
- Root branch: `feature/settings-clean-statistics-data`
- Touched submodules: `frontend`, `backend`
- Frontend submodule path: `/Users/liqing/Documents/PersonalProjects/prism-workspace/model-share-feature-settings-clean-statistics-data/frontend`
- Frontend submodule branch: `feature/settings-clean-statistics-data`
- Backend submodule path: `/Users/liqing/Documents/PersonalProjects/prism-workspace/model-share-feature-settings-clean-statistics-data/backend`
- Backend submodule branch: `feature/settings-clean-statistics-data`

## Goals

- Expose a fourth cleanup type on the settings retention section for statistics data.
- Add a typed frontend API call for statistics cleanup.
- Add a backend batch delete route and background cleanup helper for persisted statistics data.
- Preserve the existing request-log, audit-log, and loadbalance-event cleanup behavior.
- Cover the new cleanup contract with targeted frontend and backend tests.

## Non-Goals

- No changes to the request-logs route UI or behavior.
- No redesign of the statistics route presentation beyond reflecting emptied data after cleanup.
- No compatibility shims or dual cleanup paths for the old contract.
- No root-owned feature code beyond plan and submodule pointer updates.

## Scope

### In scope

- `frontend/src/pages/settings/settingsPageHelpers.ts`
- `frontend/src/pages/settings/useRetentionDeletionData.ts`
- `frontend/src/pages/settings/sections/RetentionDeletionSection.tsx`
- `frontend/src/pages/settings/dialogs/DeleteConfirmDialog.tsx`
- `frontend/src/pages/settings/__tests__/useRetentionDeletionData.test.tsx`
- `frontend/src/lib/api/observability.ts`
- `frontend/src/i18n/messages/en.ts`
- `frontend/src/i18n/messages/zh-CN.ts`
- `backend/app/routers/stats.py`
- `backend/app/routers/stats_domains/` statistics cleanup handler surface
- `backend/app/services/background_cleanup.py`
- `backend/tests/smoke_defect_regressions/test_startup_cases/stats_timezone_batch_delete_and_endpoint_mapping_tests.py`

### Supporting facts from exploration

- `frontend/src/pages/settings/useRetentionDeletionData.ts` currently branches cleanup work to `api.stats.delete`, `api.audit.delete`, and `api.loadbalance.deleteEvents`.
- `frontend/src/pages/settings/sections/RetentionDeletionSection.tsx` currently renders only `requests`, `audits`, and `loadbalance_events` in the cleanup select.
- `frontend/src/pages/statistics/useUsageStatisticsPageData.ts` loads the statistics route from `api.stats.usageSnapshot()` and `api.stats.endpointModelStatistics()`.
- `backend/app/routers/stats.py` exposes `DELETE /api/stats/requests` for request-log cleanup, but no dedicated delete route for persisted statistics data.
- `backend/app/services/stats/usage_snapshot.py` and `backend/app/services/stats/endpoint_model_statistics.py` read persisted `UsageRequestEvent` rows.
- `backend/app/services/stats/throughput.py`, `summary.py`, `spending.py`, and `model_metrics.py` still read `RequestLog`, so the new statistics cleanup must not silently replace the existing request-log cleanup contract.

## Binary Acceptance Criteria

- PASS if the settings cleanup type select exposes a `statistics` option beside the existing three options.
- PASS if the statistics option calls a dedicated backend cleanup contract instead of `DELETE /api/stats/requests`.
- PASS if the backend cleanup deletes `UsageRequestEvent` rows for the effective profile using the same `older_than_days` or `delete_all` shape as the existing batch delete routes.
- PASS if the existing request-log, audit-log, and loadbalance-event cleanup actions keep calling their current endpoints.
- PASS if the statistics page snapshot and endpoint-model breakdown clear after a statistics cleanup while request logs remain governed by the separate request-log cleanup action.
- PASS if the targeted frontend and backend tests pass.
- PASS if frontend diagnostics are clean on changed files and the frontend build exits 0.
- PASS if manual QA from settings and statistics confirms the new behavior.

## Implementation Waves

### Wave 1 - backend statistics cleanup contract

- Files:
  - `backend/app/routers/stats.py`
  - `backend/app/routers/stats_domains/` stats cleanup handler surface
  - `backend/app/services/background_cleanup.py`
  - `backend/tests/smoke_defect_regressions/test_startup_cases/stats_timezone_batch_delete_and_endpoint_mapping_tests.py`
- Changes:
  1. Add a dedicated background cleanup helper for `UsageRequestEvent` rows.
  2. Add a stats-domain delete handler that keeps `stats.py` thin and returns `BatchDeleteResponse`.
  3. Preserve the existing request-log delete route and validation behavior.
  4. Keep the delete operation profile-scoped and compatible with `older_than_days` and `delete_all`.
  5. Add regression coverage for custom-day delete, delete-all, and invalid mode combinations.
- QA tool:
  - `uv run pytest tests/smoke_defect_regressions/test_startup_cases/stats_timezone_batch_delete_and_endpoint_mapping_tests.py -k delete`
- Expected result:
  - The backend accepts and enqueues statistics cleanup requests without altering the request-log delete contract.

### Wave 2 - frontend settings cleanup wiring

- Files:
  - `frontend/src/lib/api/observability.ts`
  - `frontend/src/pages/settings/settingsPageHelpers.ts`
  - `frontend/src/pages/settings/useRetentionDeletionData.ts`
  - `frontend/src/pages/settings/sections/RetentionDeletionSection.tsx`
  - `frontend/src/pages/settings/dialogs/DeleteConfirmDialog.tsx`
  - `frontend/src/pages/settings/__tests__/useRetentionDeletionData.test.tsx`
  - `frontend/src/i18n/messages/en.ts`
  - `frontend/src/i18n/messages/zh-CN.ts`
- Changes:
  1. Add a dedicated frontend API client method for statistics cleanup.
  2. Extend the cleanup-type registry and labels with a `statistics` option.
  3. Route the new option through `useRetentionDeletionData.ts` without changing the existing three branches.
  4. Render the new option in the retention section and the delete confirmation dialog summary.
  5. Update targeted hook coverage to prove the statistics option hits the new API method.
- QA tool:
  - `pnpm exec vitest run src/pages/settings/__tests__/useRetentionDeletionData.test.tsx`
- Expected result:
  - The settings cleanup flow exposes a fourth option and dispatches it through the dedicated statistics cleanup client.

### Wave 3 - end-to-end verification and gitlink updates

- QA tools:
  - `uv run pytest tests/smoke_defect_regressions/test_startup_cases/stats_timezone_batch_delete_and_endpoint_mapping_tests.py -k delete`
  - `pnpm exec vitest run src/pages/settings/__tests__/useRetentionDeletionData.test.tsx`
  - `pnpm run build`
  - `lsp_diagnostics` on all changed frontend files
  - manual QA in the worktree app
- QA steps:
  1. Run the targeted backend delete-contract coverage.
  2. Run the targeted frontend retention-hook test.
  3. Run the frontend build in the worktree.
  4. Open `/settings`, choose the new statistics cleanup option, and confirm the delete request is accepted.
  5. Open `/statistics`, refresh, and confirm the snapshot-backed sections clear while the request-log cleanup remains separate.
- Expected result:
  - The new cleanup path works end to end and the repo remains ready for focused submodule commits plus root gitlink updates.

## Atomic Commit Strategy

1. Root repo commit: approved plan file only.
2. Backend submodule commit: statistics cleanup contract and backend regression coverage.
3. Frontend submodule commit: settings cleanup option, typed API wiring, locale updates, and frontend test coverage.
4. Root repo commit: updated `backend` and `frontend` submodule pointers.

## Plan Review Workflow

1. Save this draft plan under `.sisyphus/plans/` in the worktree.
2. Send the saved plan file to Momus for a blocking review.
3. Revise the saved plan until Momus approves it.
4. Only after approval, begin code changes in the branched submodules listed above.

## Worktree Workflow

1. Keep implementation work inside the root worktree and the `frontend` and `backend` submodule branches listed above.
2. Treat the root repo as the coordination layer and the submodules as the code-edit surfaces.
3. Run backend verification from the backend submodule worktree and frontend verification from the frontend submodule worktree.
4. Update the root repo gitlinks only after the submodule commits are in place.

## Rebase and Cleanup Guardrails

1. Do not rebase or remove the worktree until post-commit user approval is given.
2. If implementation changes the data-scope decision materially, update this plan and rerun the Momus review.
3. Keep any cleanup scoped to the created worktree; do not disturb the existing main checkout or other worktrees.
