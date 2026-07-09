# FRONTEND TEST BOUNDARY

## OVERVIEW
`frontend/tests/` is Prism's frontend regression surface. It splits browser flows from seam-contract suites and keeps the tree aligned with the current route, provider, typed-client, routing health list, and model/access-target authoring structure.

## TEST SPLIT
- `e2e/` holds the five Playwright browser journey specs only; see `e2e/AGENTS.md`.
- `lib/` holds high-centrality Node seam contracts; see `lib/AGENTS.md`.
- `model-detail/` and `server/` hold the remaining focused seam-contract suites outside Playwright's browser runner.
- `helpers/` holds shared test-only utilities such as TypeScript module loading.
- `../src/test/` holds Vitest/jsdom seams plus shared MSW setup; it is a separate frontend test layer outside this tree.

## CURRENT FACTS
- `../package.json` exposes `pnpm exec vitest run`, `pnpm run test:lib`, `pnpm run test:server`, and the browser regression entrypoint `pnpm run test:e2e`.
- CI runs frontend `pnpm exec vitest run`, `test:lib`, `test:server`, `build`, `lint`, and the five Playwright journey specs in `e2e/`.
- `pnpm test` runs the same Vitest layer in watch mode over `../src/**/*.test.{ts,tsx}` through `../vitest.config.ts`; use `pnpm exec vitest run` for the CI-equivalent gate.
- `test:lib` runs every `*.test.mjs` directly under `lib/` plus `model-detail/`; there are no separate `main/` or `loadbalance/` node-test roots anymore.
- `../playwright.config.ts` points Playwright at `./tests/e2e` and uses `http://127.0.0.1:15174` as the web server target.
- Browser coverage lives in `e2e/auth-session-lifecycle.spec.ts`, `e2e/loadbalance-strategies-recovery.spec.ts`, `e2e/models-access-target-authoring.spec.ts`, `e2e/request-log-dedicated-audit-page.spec.ts`, and `e2e/shared-chart-statistics.spec.ts`.
- Dashboard/statistics browser fixture data lives in `e2e/dashboard-aggregate-fixtures.ts`.
- Model CRUD and access-target authoring seam coverage lives in `lib/model_form_state_contract.test.mjs`, `model-detail/*.test.mjs`, and `lib/profile_scope_header_contract.test.mjs`.
- Dashboard routing list and bootstrap data-shaping seam coverage lives in `lib/dashboard_routing_list_contract.test.mjs` and `lib/dashboard_bootstrap_contract.test.mjs`.

## CHILD DOCS
- `e2e/AGENTS.md`: Playwright route-flow conventions, browser fixtures, mocked backend ownership, and canonical route expectations.
- `lib/AGENTS.md`: Node `--test` seam-contract conventions, `loadTsModule` use, and `test:lib` glob boundaries.

## WHERE TO LOOK
- Statistics and analytics browser coverage: `e2e/shared-chart-statistics.spec.ts`
- Model/access-target seam coverage: `lib/model_form_state_contract.test.mjs`
- Dashboard routing list seam coverage: `lib/dashboard_routing_list_contract.test.mjs`
- Shared contract seams: `lib/*.test.mjs`, `model-detail/*.test.mjs`, `server/*.test.mjs`
- Shared test helpers: `helpers/loadTsModule.mjs`
- Vitest/MSW seams outside this tree: `../src/test/setup.ts`, `../src/test/msw/server.ts`, `../src/test/msw/handlers.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this doc summary-level.
- Keep browser flows in `e2e/` and seam contracts in the named sibling folders.
- Keep Vitest/jsdom tests under `../src/test/` or colocated `../src/**/*.test.{ts,tsx}`; do not mix them into Playwright browser flows.
- Keep shared test-only utilities in `helpers/` instead of scattering loader glue across suites.
- Keep pinned Default-profile header and typed-client contract tests separate from Playwright route flows.
- Do not invent extra test roots or child AGENTS files unless a subtree has a distinct runner or command boundary like `e2e/` and `lib/`.
- Keep test ownership single-layer: backend unit tests own process-local pricing, planning, and stream classification without DB; DB contract suites own one API surface; frontend Vitest/lib owns pure frontend logic. Do not duplicate one behavior across layers or add INSERT-then-SELECT mirror tests.
- Keep Playwright capped near five journey specs; adding a browser journey deletes one. Browser specs must not assert table-cell text or i18n fallback behavior.
- Keep setup before the first act within 10 lines; use defaulted builders when it grows, reject e2e leading mocks over 50 lines, and keep container/build commands out of test functions.
- Use baseline-plus-override helpers or golden files when expectations exceed eight fields. Use golden files for large shapes and inline assertions only for the fields the test cares about.
- Table-drive three or more cases that share the same act/assert shape, and keep at most one narrative story test per resource.
- Test Prism behavior, not platform internals or dependency output: do not grep production Go or TS source text, test Postgres internals, assert Recharts/xyflow rendering, or manually recalculate aggregation internals.
- Let proves-not tests expire with removal PRs. Permanent exceptions are route-contract parity guards, because they own route-level absence, and backend Dockerfile contract tests, because they guard the shipped container contract.
- Tests that remain must run in CI or document why they do not. Wire commands by glob: `pnpm exec vitest run`, `pnpm run test:lib`, `pnpm run test:server`, and the Playwright journey specs are CI gates.
- Prevent rebound: feature PR test line additions must not exceed product line additions, no plan-numbered test names, and a new shared helper must delete the copy/paste it replaces in the same PR.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.
