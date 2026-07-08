# Task 25 Report

## Status

DONE

## Summary

- Inverted the frontend message type source to `frontend/src/i18n/messages/zh-CN.ts`; it now exports `Messages = typeof zhCNMessages` and uses no `as const`.
- Added `frontend/src/i18n/messages/index.ts` and moved message consumers off `messages/en`.
- Deleted `frontend/src/i18n/messages/en.ts`.
- Collapsed locale handling to the single `zh-CN` locale: no normalization, browser-language selection, persisted locale selection, locale setter, or language-switching UI remains.
- Removed `LanguageSwitcher` from the auth shell and app user menu.
- Updated docs and tests for the zh-CN-only i18n contract.
- Updated the TypeScript test loader to resolve directory `index.ts` files before attempting to read directories; this is needed by the new `@/i18n/messages` barrel in Node seam tests.

## Verification

- `rg -n "messages/en|enMessages" frontend/`
  - Passed; no matches.
- `rg -n "setLocale" frontend/`
  - Passed; no matches.
- `cd frontend && pnpm run build`
  - Passed.
- `cd frontend && pnpm run test`
  - Passed: 15 files, 38 tests.
- `cd frontend && pnpm run test:lib`
  - Passed: 75 tests.
- `cd frontend && pnpm run test:server`
  - Passed: 4 tests.
- `cd frontend && pnpm run lint`
  - Passed.

## Concerns

- None.

## Reviewer Follow-up

### Summary

- Removed remaining e2e persisted locale seeding, locale helper functions, and locale options from `frontend/tests/e2e/`.
- Updated focused e2e assertions that had depended on the old English locale seed to assert the live zh-CN UI.
- Updated app-layout and e2e AGENTS docs so `NavUser` and browser-storage guidance no longer mention locale controls/state.

### Verification

- `rg -n "prism\.locale|locale" frontend/tests/e2e frontend/src/components/layout/app-layout/AGENTS.md frontend/tests/e2e/AGENTS.md`
  - Passed; no matches.
- `rg -n "messages/en|enMessages" frontend/`
  - Passed; no matches.
- `rg -n "setLocale" frontend/`
  - Passed; no matches.
- `cd frontend && pnpm run test:e2e -- auth-session-lifecycle.spec.ts request-log-dedicated-audit-page.spec.ts protected-shell-sidebar.spec.ts`
  - Passed: 26 tests.
- `cd frontend && pnpm run test:lib`
  - Passed: 75 tests.

### Concerns

- None.
