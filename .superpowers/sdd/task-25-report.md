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
