# Frontend Autofill Semantics Remediation

## Status

Approved by Momus on 2026-04-04.

## Decision Summary

- Treat this as a frontend-wide field-semantics fix, not a UI styling task.
- Prefer field-specific semantic `autoComplete` tokens when the field has real browser meaning.
- Use `autoComplete="off"` only for app-internal text, search, and secret fields that should not map to saved profile or credential data.
- Keep fixes local to pages and dialogs; do not add a global autocomplete policy in `frontend/src/components/ui/input.tsx`.

## Goals

- Remove misleading saved-info/autofill prompts across Prism frontend forms by classifying fields intentionally.
- Preserve correct password-manager and browser behavior on auth flows.
- Keep search and filter inputs behaving like search and filter controls, not profile or credential fields.

## Non-Goals

- Styling, skinning, or otherwise trying to control the browser-native popup UI.
- Blanket `autoComplete="off"` across all inputs or across whole forms.
- Adding backward-compatibility shims or alternate form paths.
- Claiming automated tests can prove native popup rendering.

## Scope

- Auth/public flows: `frontend/src/pages/LoginPage.tsx`, `frontend/src/pages/ForgotPasswordPage.tsx`, `frontend/src/pages/ResetPasswordPage.tsx`
- Authentication settings review: `frontend/src/pages/settings/sections/authentication/OperatorEmailCard.tsx`, `frontend/src/pages/settings/sections/authentication/RecoveryEmailCard.tsx`
- Shared input audit only: `frontend/src/components/ui/input.tsx`
- Existing explicit cases to review for consistency, not redesign: `frontend/src/pages/models/ModelDialog.tsx`, `frontend/src/pages/model-detail/ConnectionDialog.tsx`
- High-impact named-input dialogs: `frontend/src/pages/endpoints/EndpointDialog.tsx`, `frontend/src/pages/model-detail/ModelSettingsDialog.tsx`, `frontend/src/pages/settings/dialogs/VendorDialog.tsx`, `frontend/src/pages/settings/dialogs/RuleDialog.tsx`, `frontend/src/pages/loadbalance-strategies/LoadbalanceStrategyDialog.tsx`, `frontend/src/pages/pricing-templates/PricingTemplateDialog.tsx`, `frontend/src/pages/proxy-api-keys/ProxyKeyCreateCard.tsx`, `frontend/src/pages/proxy-api-keys/EditProxyKeyDialog.tsx`
- Search/filter clusters: `frontend/src/pages/models/ModelsToolbar.tsx`, `frontend/src/pages/EndpointsPage.tsx`, `frontend/src/pages/model-detail/ConnectionsList.tsx`, `frontend/src/pages/request-logs/FiltersBarPrimaryFilters.tsx`

## Field Classification Policy

- Use `username` for the login identifier, even if the user enters an email address there.
- Use `current-password` for the login password field.
- Use `new-password` for reset, set, and confirm-new-password fields.
- Use `one-time-code` for OTP or recovery code fields.
- Use `email` only for true contact or recovery email fields outside the login identifier case.
- Use `url` for endpoint, connection, or provider URL fields.
- Use `organization` only if a field is truly a company or organization name, not an internal vendor nickname.
- Use `autoComplete="off"` for API keys, secret values, internal names and labels such as vendor nickname, rule name, strategy name, pricing template name, endpoint display name, model name, and free-text search/filter fields.
- Classify each input individually. Do not apply a blanket form-level or dialog-level value.
- Do not rely on `off` to suppress auth autofill. Browsers may ignore it on login/password flows.
- Do not add a default autocomplete rule to `frontend/src/components/ui/input.tsx`; keep it as pass-through unless a later failing test proves a tiny shared helper is necessary.

## Shared vs Page-Specific Changes

- Put semantic decisions at the page/dialog field site, because field meaning lives there.
- Preserve existing correct explicit semantics in auth/settings/model-detail code and only normalize them if they are inconsistent.
- Only touch `frontend/src/components/ui/input.tsx` if a minimal shared prop or helper is required after page-level coverage is complete.

## TDD-First Implementation Order

1. Add or extend auth-page tests first.
2. Start with `frontend/src/pages/__tests__/LoginPage.test.tsx`.
3. Add or extend forgot/reset tests near the auth page tests to assert actual field meaning, including `username` vs `email`, `one-time-code`, and `new-password`.
4. Extend `frontend/src/pages/settings/__tests__/AuthenticationSetupGrid.test.tsx`.
5. Extend `frontend/src/pages/models/__tests__/ModelDialogs.i18n.test.tsx`.
6. Extend `frontend/src/pages/model-detail/__tests__/ConnectionDialog.limiterFields.test.tsx`.
7. Add focused dialog tests for endpoint, model-settings, vendor, rule, load-balance, pricing-template, and proxy-key forms.
8. Add search/filter tests for the toolbar and filter clusters to assert intended neutral or `off` semantics and absence of auth/contact tokens.
9. Only after failing tests exist, implement field-level autocomplete updates cluster by cluster.

## Verification Steps

1. Run targeted frontend tests for each touched cluster.
2. Run the broader frontend form-related test suite that covers touched pages and dialogs.
3. Run a frontend build after semantic changes are complete.
4. Perform manual QA in a browser profile with saved names, addresses, emails, usernames, and passwords.
5. Verify create and edit variants for dialogs with prefilled values.
6. Verify search and filter behavior still works exactly as before.
7. Treat automated verification as DOM-semantics coverage only; treat native popup behavior as manual QA only.

## Task Waves and QA Scenarios

### Wave 1 — auth tests first

- Files: `frontend/src/pages/__tests__/LoginPage.test.tsx`, auth-adjacent tests for forgot/reset flows, `frontend/src/pages/settings/__tests__/AuthenticationSetupGrid.test.tsx`
- QA tool: `pnpm exec vitest run`
- QA steps:
  1. Add failing assertions for login identifier, password, forgot-password, reset-password, operator account, and recovery-email fields.
  2. Run only the touched auth test files.
  3. Confirm the failures are specifically missing or incorrect `autocomplete` attributes, not unrelated render failures.
- Expected result: red tests that name the missing or wrong field semantics.

### Wave 2 — auth implementation

- Files: `frontend/src/pages/LoginPage.tsx`, `frontend/src/pages/ForgotPasswordPage.tsx`, `frontend/src/pages/ResetPasswordPage.tsx`, and auth settings files only if test evidence requires changes.
- QA tools: `pnpm exec vitest run`, browser manual QA.
- QA steps:
  1. Re-run the touched auth tests and confirm they pass.
  2. Open the login flow in the browser and inspect the rendered DOM or visible form behavior.
  3. Confirm login uses `username` + `current-password`, forgot/reset flows use intended email/OTP/new-password semantics, and no auth field is forced to `off` without test-backed reason.
- Expected result: auth tests green and browser behavior remains credential-manager friendly.

### Wave 3 — business-dialog and management-form tests

- Files: dialog and form tests covering endpoints, model settings, vendor, rule, loadbalance strategy, pricing template, proxy key, models dialog, and connection dialog.
- QA tool: `pnpm exec vitest run`
- QA steps:
  1. Add failing assertions that named internal fields and secrets use the intended explicit policy (`off`, `url`, or preserved existing semantic tokens).
  2. Run only the touched dialog/form test files.
  3. Confirm failures are attributable to missing autocomplete policy on those fields.
- Expected result: red tests for each changed cluster before production edits.

### Wave 4 — business-dialog and management-form implementation

- Files: the touched dialog/form components only.
- QA tools: `pnpm exec vitest run`, `pnpm run build`, browser manual QA.
- QA steps:
  1. Re-run the touched dialog/form tests and confirm they pass.
  2. Run a full frontend build.
  3. Manually open each touched dialog family in the browser and inspect the relevant field attributes or observed popup behavior:
     - endpoint + connection URL fields must expose `url`
     - API keys and internal naming fields must expose `off`
     - previously explicit semantics in models/connection/auth settings must remain intentional
- Expected result: dialog/form tests green, build exits 0, and browser inspection shows the intended field metadata.

### Wave 5 — search and filter surfaces

- Files: `frontend/src/pages/models/ModelsToolbar.tsx`, `frontend/src/pages/EndpointsPage.tsx`, `frontend/src/pages/model-detail/ConnectionsList.tsx`, `frontend/src/pages/request-logs/FiltersBarPrimaryFilters.tsx`, plus any directly coupled tests.
- QA tools: `pnpm exec vitest run`, browser manual QA.
- QA steps:
  1. Add or extend assertions for search/filter inputs so they do not expose auth/contact semantics.
  2. Run the touched search/filter tests.
  3. Open the affected pages in the browser and confirm those inputs no longer present irrelevant saved-info suggestions during basic interaction.
- Expected result: search/filter inputs are explicitly neutral or `off`, tests pass, and browser behavior is improved on the targeted pages.

### Final verification wave

- QA tools: `pnpm exec vitest run`, `pnpm run build`, `lsp_diagnostics`, browser manual QA.
- QA steps:
  1. Run the complete touched frontend test set, then the full frontend test suite if scope is broad enough.
  2. Run `pnpm run build` and require exit code 0.
  3. Run diagnostics on every modified file and require zero new errors.
  4. Repeat browser QA on auth, one representative internal dialog, one secret field, and one search/filter page using a profile with saved data.
- Expected result: tests pass, build passes, diagnostics are clean, and browser QA confirms the issue is resolved without styling hacks.

## Manual QA

- Check login still offers expected credential-manager behavior.
- Check forgot/reset/recovery flows still cooperate with email, OTP, and new-password semantics.
- Check app-internal forms no longer trigger irrelevant saved-info suggestions, or reduce them materially enough that the issue is resolved.
- Check search/filter bars do not trigger profile or credential suggestions.
- Spot-check in Chrome first, then one secondary browser used by the team if available.

## Binary Acceptance Criteria

- PASS if every in-scope user-editable field in the listed clusters has an intentional autocomplete policy.
- PASS if auth fields use auth-correct semantics and none of them are forced `off` in a way that fights password-manager behavior.
- PASS if non-auth internal text, search, and secret fields do not carry misleading auth/contact tokens.
- PASS if new or updated tests assert the intended DOM attributes for every touched form cluster.
- PASS if manual QA confirms the issue is resolved for non-auth app fields without styling hacks and auth autofill still behaves appropriately.
- FAIL if any fix depends on styling the native popup, a blanket global `off` policy, or claims that automated tests proved native popup rendering.

## Plan Review Workflow

1. Prometheus drafts the initial plan.
2. Momus audits for wrong field classification, overuse of `off`, missing clusters, missing tests, or workflow gaps.
3. Revise and re-audit until Momus explicitly approves.
4. Save the approved plan under `.sisyphus/plans/` with the dated filename above.

## Worktree Workflow

1. Create a dedicated worktree from the current local `main` baseline, not from an assumed pristine remote-only state.
2. Use the repo worktree flow so the worktree copies env, initializes submodules, and installs dependencies.
3. Create a root branch such as `frontend-autocomplete-semantics`.
4. Inside that worktree, create a matching `frontend` submodule branch from the submodule’s current local `main` baseline.
5. Perform all implementation and verification inside the worktree.
6. Do not delete the worktree until rebase, verification, and integration are complete.

## Atomic Commit Strategy

1. Root worktree commit: approved plan document only.
2. Frontend commit: auth-page failing tests.
3. Frontend commit: auth-page semantic fixes.
4. Frontend commit: settings/dialog/search failing tests.
5. Frontend commit: settings/dialog/search semantic fixes.
6. Frontend commit: minimal cleanup or test adjustments only if needed.
7. Root worktree commit: updated `frontend` submodule pointer.

## Rebase and Conflict Flow

1. Rebase the `frontend` submodule branch onto the latest local `frontend/main`.
2. If local `main` branches moved, update them in the primary repo first, then rebase the worktree branches.
3. Resolve conflicts by reapplying the approved semantic policy, not by reverting unrelated user changes.
4. After the frontend branch is final, update the root submodule pointer.
5. Rebase the root worktree branch onto the latest local root `main`.
6. Re-run verification after each rebase or conflict-resolution step.
7. Use follow-up commits instead of rewriting unrelated history.

## Cleanup

1. After integration is complete, remove the temporary worktree.
2. Remove the temporary implementation branch if it is no longer needed.
3. Keep the approved plan file in the repository as the active implementation record until the team decides to archive it.
