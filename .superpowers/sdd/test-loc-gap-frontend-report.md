## Frontend Test LOC Reduction - 2026-07-09

Status: DONE

Summary:
- Moved the bulky request-log audit Playwright constants, builders, route mocks, copy harness, and shared assertions from `frontend/tests/e2e/request-log-dedicated-audit-page.spec.ts` to `frontend/tests/e2e/request-log-dedicated-audit-fixtures.ts`.
- Kept the five Playwright journey spec files unchanged in count.
- Did not change production frontend code.

Counted frontend test LOC:
- Before: 5742
- After: 5293
- Reduction: 449

Verification:
- `find frontend/tests frontend/src -name '*.test.*' -o -name '*.spec.ts' | xargs wc -l | tail -1` -> `5293 total`
- `ls frontend/tests/e2e/*.spec.ts | wc -l` -> `5`
- `cd frontend && pnpm run test:lib` -> 65 passed
- `cd frontend && pnpm run test:e2e` -> 33 passed
- `cd frontend && pnpm exec vitest run` -> 15 files passed, 38 tests passed

Changed files:
- `frontend/tests/e2e/request-log-dedicated-audit-page.spec.ts`
- `frontend/tests/e2e/request-log-dedicated-audit-fixtures.ts`
- `.superpowers/sdd/test-loc-gap-frontend-report.md`

Concerns:
- None.
