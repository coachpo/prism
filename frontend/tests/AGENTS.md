# Frontend test ownership

Follow the canonical [test ownership policy](../../docs/development-rules.md#test-ownership) for layer selection, fixture/assertion size, scenario and line budgets, and prohibited duplicate/platform checks. This directory only adds runner-specific boundaries.

- `../vitest.config.ts` discovers `../src/**/*.test.{ts,tsx}` with jsdom and `../src/test/setup.ts`; run `pnpm exec vitest run` from `frontend/` for a non-watch gate. `pnpm test` starts watch mode.
- [lib](lib/AGENTS.md) contains Node seam contracts, discovered by `pnpm run test:lib` through `tests/lib/*.test.mjs`. Keep shared TS-loading utilities in `helpers/`.
- [e2e](e2e/AGENTS.md) contains mocked Playwright journeys, discovered by `pnpm run test:e2e`. Browser flows must not duplicate pure API/parser/form tests or contact a real backend.
- CI runs Vitest, Node seams, build, lint, and the Playwright journey glob. Place retained tests in those existing discovery boundaries instead of creating local-only runner roots.
- Keep profile-header/typed-client contracts in Node seams, React lifecycle/last-good/candidate-pager behavior in Vitest, and complete navigation/mutation flows in Playwright. Test the boundary that owns the behavior once.
