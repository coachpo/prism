# Test suite trim phase 1

## Status
Approved

## Requirement Summary
Start the trim-first test-suite reduction work in an isolated delivery worktree and implement only the first low-risk frontend tranche.

## Scope
- In scope:
  - remove or minimally consolidate these five low-signal shell i18n tests:
    - `frontend/src/pages/models/__tests__/ModelsPageShell.i18n.test.tsx`
    - `frontend/src/pages/endpoints/__tests__/EndpointsPageShell.i18n.test.tsx`
    - `frontend/src/pages/proxy-api-keys/__tests__/ProxyApiKeysPageShell.i18n.test.tsx`
    - `frontend/src/pages/pricing-templates/__tests__/PricingTemplatesPageShell.i18n.test.tsx`
    - `frontend/src/pages/loadbalance-strategies/__tests__/LoadbalanceStrategiesPageShell.i18n.test.tsx`
  - keep stronger frontend anchors intact
  - save the approved plan in `.sisyphus/plans/`
- Out of scope:
  - backend test trimming
  - settings, statistics, and request-logs shell-i18n trimming
  - production-code changes unless tranche-1 assumptions fail
  - commit, rebase, push, or worktree cleanup

## Assumptions
- Shared locale/runtime anchors are sufficient for tranche 1 even if these five routes lose dedicated Chinese shell-copy tests.
- No backend files need to change in this tranche.
- No new test-suite bucket should be introduced; any preserved assertion must move into an existing stronger test.

## Base and Delivery Model
- Base branch: `main`
- Base ref used for worktree creation: `origin/main`
- Base SHA at worktree creation: `af605e300acbe54d1d96364cca4bf353a3b67635`
- Delivery branch: `sisyphus/trim-test-suite-phase-1-20260407`
- Delivery worktree path: `/Users/liqing/Documents/PersonalProjects/prism-trim-test-suite-phase-1-20260407`
- Published branch expectation: local-only unless explicitly pushed later
- Local base checkout for later fast-forward: `/Users/liqing/Documents/PersonalProjects/prism`

## Monorepo Scope
- Workspace root: `/Users/liqing/Documents/PersonalProjects/prism-trim-test-suite-phase-1-20260407`
- Packages or apps in scope: `frontend`
- Bootstrap or install in worktree: `pnpm install` already ran in `frontend/`

## Broader Strategy
- Tranche 1 removes the five easiest page-shell locale clones.
- Later frontend tranches may examine request-logs, settings, and statistics shell-i18n coverage separately.
- Backend remains a planning guardrail only because `backend/tests/conftest.py` makes most backend verification container-and-migration heavy.

## Implementation Steps
1. Run a characterization baseline on the five target tests in `frontend/`.
   - QA: run `pnpm exec vitest run src/pages/models/__tests__/ModelsPageShell.i18n.test.tsx src/pages/endpoints/__tests__/EndpointsPageShell.i18n.test.tsx src/pages/proxy-api-keys/__tests__/ProxyApiKeysPageShell.i18n.test.tsx src/pages/pricing-templates/__tests__/PricingTemplatesPageShell.i18n.test.tsx src/pages/loadbalance-strategies/__tests__/LoadbalanceStrategiesPageShell.i18n.test.tsx`.
   - Expected result: all five target tests pass before changes, confirming the current duplicate shell-copy baseline.
2. Audit each file for a uniquely valuable assertion before deleting it.
   - QA: inspect the five files directly and compare their assertions against the preserved anchors and adjacent domain tests named in this plan.
   - Expected result: every route-local assertion is classified as either low-signal duplicate, or as a uniquely valuable assertion that needs a stronger retained home before deletion.
3. Delete the file when it only rechecks `prism.locale` setup, mocked page data, rendered shell copy, and translated labels already considered low-signal.
   - QA: inspect the git diff after deletions and confirm only the intended five test files were removed at this step.
   - Expected result: deleted files disappear from the tree, and no non-test or production files change.
4. If one assertion must survive, move only that assertion into an existing stronger test in the same ownership tree, then delete the original shell test.
   - QA: run the preserved-anchor and adjacent-domain Vitest commands from the Verification section, plus conditional lint if a kept test file was edited.
   - Expected result: the promoted assertion is covered by an existing stronger test, all scoped commands exit 0, and no replacement shell-copy-only file is introduced.
5. Keep production files unchanged unless the uniqueness audit disproves tranche-1 assumptions; if it does, stop and re-plan instead of widening scope.
   - QA: inspect the git diff and confirm all changed paths remain under `frontend/src/**/__tests__/` and `.sisyphus/plans/` only.
   - Expected result: no production-code paths are modified in this tranche.
6. Run the exact scoped verification commands below and stop after green verification.
   - QA: execute every command in the Verification section exactly as written.
   - Expected result: every command exits 0, deleted files stay absent, preserved anchors stay green, and the tranche ends without commit, rebase, push, or cleanup.

## Verification
- Pre-change characterization:
  - `pnpm exec vitest run src/pages/models/__tests__/ModelsPageShell.i18n.test.tsx src/pages/endpoints/__tests__/EndpointsPageShell.i18n.test.tsx src/pages/proxy-api-keys/__tests__/ProxyApiKeysPageShell.i18n.test.tsx src/pages/pricing-templates/__tests__/PricingTemplatesPageShell.i18n.test.tsx src/pages/loadbalance-strategies/__tests__/LoadbalanceStrategiesPageShell.i18n.test.tsx`
- Preserved route, shell, and locale anchors:
  - `pnpm exec vitest run src/pages/__tests__/AppRouteSmoke.test.tsx src/components/layout/app-layout/__tests__/AppLayoutShell.test.tsx src/components/layout/app-layout/__tests__/useShellNavigation.test.tsx src/i18n/__tests__/LocaleProvider.test.tsx src/i18n/__tests__/format.test.ts`
- Adjacent domain behavior around the trimmed pages:
  - `pnpm exec vitest run src/pages/models/__tests__/useModelsPageData.test.tsx src/pages/endpoints/__tests__/EndpointsPage.review-mode.test.tsx src/pages/proxy-api-keys/__tests__/useProxyApiKeysPageData.test.tsx src/pages/pricing-templates/__tests__/PricingTemplatesTable.test.tsx src/pages/loadbalance-strategies/__tests__/LoadbalanceStrategiesTable.test.tsx`
- Non-adjacent guardrails preserved by this strategy:
  - `pnpm exec vitest run src/pages/request-logs/__tests__/useRequestLogPageState.test.tsx src/pages/request-logs/__tests__/RequestLogsPage.navigation.test.tsx src/pages/settings/__tests__/SettingsPage.test.tsx`
- Conditional lint only if a kept test file is edited during consolidation:
  - `pnpm exec eslint src/pages/__tests__/AppRouteSmoke.test.tsx src/components/layout/app-layout/__tests__/AppLayoutShell.test.tsx src/components/layout/app-layout/__tests__/useShellNavigation.test.tsx src/i18n/__tests__/LocaleProvider.test.tsx src/i18n/__tests__/format.test.ts src/pages/models/__tests__/useModelsPageData.test.tsx src/pages/endpoints/__tests__/EndpointsPage.review-mode.test.tsx src/pages/proxy-api-keys/__tests__/useProxyApiKeysPageData.test.tsx src/pages/pricing-templates/__tests__/PricingTemplatesTable.test.tsx src/pages/loadbalance-strategies/__tests__/LoadbalanceStrategiesTable.test.tsx src/pages/request-logs/__tests__/useRequestLogPageState.test.tsx src/pages/request-logs/__tests__/RequestLogsPage.navigation.test.tsx src/pages/settings/__tests__/SettingsPage.test.tsx`
- Explicitly excluded in this tranche:
  - no `pnpm run build`
  - no full `pnpm run test`
  - no backend verification commands

## Finish Workflow Contract
- Commit happens later on the delivery branch only.
- Create a safety ref before any history rewrite using format `safety/trim-test-suite-phase-1-20260407-pre-rebase-<YYYYMMDDHHMM>-<shortsha>`.
- Refresh `origin/main` before rebasing.
- Rebase direction later: delivery branch onto refreshed `origin/main` only.
- Re-run the exact scoped verification commands after the rebase.
- Update local `main` in `/Users/liqing/Documents/PersonalProjects/prism` by fast-forward only.
- Any push is explicit-request-only.

## Rewrite Safety
- Never rebase `main`.
- Never update the base branch by merge commit.
- Do not touch `backend/` in this tranche.
- Do not add replacement shell-copy test buckets.
- Do not broaden scope into settings, statistics, or request-logs shell-i18n trimming in this tranche.

## Risks and Rollback
- Risk: one of the five shell tests may hold a uniquely valuable route-local assertion.
  - Rollback: restore that file or revert the whole delivery-branch implementation commit and re-plan.
- Risk: deleting these tests may leave a route with weaker-than-expected locale confidence.
  - Rollback: promote the specific assertion into an existing stronger test and re-run scoped verification.
- Risk: a later rebase could invalidate the verified delivery branch.
  - Rollback: recover from the safety ref and keep `main` untouched.

## Approval Record
- Planner: Prometheus
- Critics: Momus
- Verdict: Approved
- Revision rounds: 1
- Review date: 2026-04-07

## Stop Point
- Stop after implementation: yes
- Commit in this skill: no
- Rebase in this skill: no
- Push in this skill: no
- Worktree cleanup in this skill: no
