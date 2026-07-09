# Task 23 Report

## Changed Files
- `docs/ARCHITECTURE.md`
- `frontend/AGENTS.md`
- `frontend/package.json`
- `frontend/pnpm-lock.yaml`
- `frontend/src/App.tsx`
- `frontend/src/app/AGENTS.md`
- `frontend/src/app/router/appRouter.tsx`
- `frontend/src/app/router/authGates.ts`
- `frontend/src/app/router/rewriteRoutes.ts`
- `frontend/src/components/SpendTrustIndicator.tsx`
- `frontend/src/components/layout/app-layout/AGENTS.md`
- `frontend/src/components/layout/app-layout/AppSidebar.tsx`
- `frontend/src/components/layout/app-layout/SiteHeader.tsx`
- `frontend/src/components/layout/app-layout/useAppLayoutState.ts`
- `frontend/src/components/layout/app-layout/useShellNavigation.ts`
- `frontend/src/components/layout/page.tsx`
- `frontend/src/features/models/ModelsTable.tsx`
- `frontend/src/features/models/detail/ModelDetailFeaturePage.tsx`
- `frontend/src/features/models/detail/useModelDetailFeatureData.ts`
- `frontend/src/features/request-logs/RequestLogAuditFeaturePage.tsx`
- `frontend/src/pages/DashboardPage.tsx`
- `frontend/src/pages/LoginPage.tsx`
- `frontend/src/pages/dashboard/queryParams.ts`
- `frontend/src/pages/dashboard/useDashboardPageState.ts`
- `frontend/src/pages/model-detail/useConnectionFocus.ts`
- `frontend/src/pages/request-logs/FiltersBar.tsx`
- `frontend/src/pages/request-logs/RequestLogAuditPage.tsx`
- `frontend/src/pages/request-logs/RequestLogDetailSheet.tsx`
- `frontend/src/pages/request-logs/queryParams.ts`
- `frontend/src/pages/request-logs/useRequestLogPageState.ts`
- `frontend/src/pages/settings/useAuthenticationSettingsData.ts`
- `frontend/src/pages/settings/useSettingsPageData.ts`
- `frontend/src/pages/statistics/sections/UsageBreakdownSection.tsx`
- `frontend/src/test/route-helpers.test.ts`
- `.superpowers/sdd/task-23-report.md`

## Summary
- Finished the `react-router-dom` removal and kept app routing on TanStack Router only.
- Replaced the request-log clear-filter state source with the router location search object. This prevents the canonicalization effect from writing stale `client_rule_id` and `resolved_target_model_id` back after clearing filters on the same route.
- Kept request-log URL parsing and serialization inside the existing route/query-param helpers.

## Verification
- `pnpm exec playwright test tests/e2e/request-logs-filter-options-loading.spec.ts -g "clear filters removes client"`: red before fix with timeout waiting for the unfiltered refetch; passed after fix, 1/1.
- `pnpm exec playwright test tests/e2e/request-logs-filter-options-loading.spec.ts`: passed, 6/6.
- `rg -l "react-router-dom" frontend/src frontend/package.json`: no matches.
- `cd frontend && pnpm run build`: passed. Vite kept the existing large-chunk warning.
- `cd frontend && pnpm run lint`: passed.
- `cd frontend && pnpm run test`: passed, 15 files / 38 tests.
- `cd frontend && pnpm run test:lib`: passed, 75 tests.
- `cd frontend && pnpm run test:server`: passed, 4 tests.
- Previous worker had already run and passed the focused router e2e set before the Clear Filters blocker.

## Concerns
- Full Playwright e2e was not run; only the request-log filter file was rerun after the fix.
- Known unrelated dirty/untracked files were left unstaged: Task 9/12 reports, `docs/IMPLEMENTATION_PLAN.md`, and `docs/TEST_REDUCTION_*.md`.

## Reviewer Docs Follow-up

### Summary
- Removed stale active-doc frontend stack wording that still named the old router compatibility layer.
- Replaced the active development-direction note that described both routers as mounted with a completed-state TanStack Router note.
- Left `docs/IMPLEMENTATION_PLAN.md` planning references untouched.

### Verification
- `rg -n "react-router-dom|React Router|BrowserRouter" docs frontend/AGENTS.md frontend/src -S`: only `docs/IMPLEMENTATION_PLAN.md` planning references remain.
