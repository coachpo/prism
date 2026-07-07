# FRONTEND TEST BOUNDARY

## OVERVIEW
`frontend/tests/` is Prism's frontend regression surface. It splits browser flows from seam-contract suites and keeps the tree aligned with the current route, provider, websocket, typed-client, React Flow routing diagram, and model/access-target authoring structure.

## TEST SPLIT
- `e2e/` holds Playwright browser route flows only; see `e2e/AGENTS.md`.
- `lib/` holds high-centrality Node seam contracts; see `lib/AGENTS.md`.
- `loadbalance/`, `main/`, `model-detail/`, and `server/` hold smaller focused seam-contract suites outside Playwright's browser runner.
- `helpers/` holds shared test-only utilities such as TypeScript module loading.
- `../src/test/` holds Vitest/jsdom seams plus shared MSW setup; it is a separate frontend test layer outside this tree.

## CURRENT FACTS
- `../package.json` exposes `pnpm run test:lib`, `pnpm run test:server`, focused config Playwright as `pnpm run test:config`, and the full browser regression entrypoint as `pnpm run test:e2e`.
- CI runs frontend `test:lib`, `test:server`, `build`, and `lint`; it does not run `pnpm test`, `test:config`, or full `test:e2e`.
- `pnpm test` runs Vitest over `../src/**/*.test.{ts,tsx}` through `../vitest.config.ts`; treat it as a separate local gate for `../src` changes.
- Some `lib/*.test.mjs`, plus `main/` and `loadbalance/`, are not included in `test:lib`; run the specific node-test file when changing those seams.
- Config import/export hardening follows a focused-to-broad order: backend configbundle/model tests, frontend seam tests, focused Playwright config/model specs, broadened backend Go suites, frontend `test:e2e`, frontend `build`, frontend `lint`, backend build.
- `../playwright.config.ts` points Playwright at `./tests/e2e` and uses `http://127.0.0.1:15174` as the web server target.
- Statistics and analytics realtime coverage lives in `e2e/shared-chart-statistics.spec.ts`, `e2e/statistics-ttft.spec.ts`, `e2e/statistics-token-rate.spec.ts`, `e2e/statistics-filtered-totals.spec.ts`, `e2e/statistics-proxy-api-key-label.spec.ts`, and `e2e/analytics-websocket-native.spec.ts`.
- Request-log/detail coverage lives in `e2e/request-log-*.spec.ts`, `e2e/request-log-detail-copy.spec.ts`, `e2e/request-log-audit-disabled-state.spec.ts`, `e2e/request-logs-token-rate.spec.ts`, `e2e/request-logs-ttft.spec.ts`, and `e2e/request-logs-optional-zero.spec.ts`.
- Model-detail handoff, unified access-target authoring, and connection-probe coverage lives in the model-detail e2e flows, `e2e/model-detail-request-logs-handoff.spec.ts`, `e2e/model-detail-connection-dialog-probe.spec.ts`, `model-detail/*.test.mjs`, and `lib/profile_scope_header_contract.test.mjs`.
- Model CRUD, access-target authoring, and profile config import coverage lives in `e2e/settings-config-import.spec.ts`, `lib/model_form_state_contract.test.mjs`, and `lib/config_import_validation_contract.test.mjs`.
- Dashboard React Flow layout and renderer seam coverage lives in `lib/dashboard_routing_flow_layout_contract.test.mjs`, while browser shell behavior stays in `e2e/dashboard-routing-shell.spec.ts`.
- Shared dashboard/statistics browser fixture data lives in `e2e/dashboard-aggregate-fixtures.ts`.
- Browser coverage also includes auth session lifecycle, launcher same-origin realtime, proxy-key lifecycle, startup/settings export, reporting currency, startup tab, and user-agent client-rule flows under `e2e/`.

## CHILD DOCS
- `e2e/AGENTS.md`: Playwright route-flow conventions, browser fixtures, mocked backend ownership, and canonical route expectations.
- `lib/AGENTS.md`: Node `--test` seam-contract conventions, `loadTsModule` use, and `test:lib` allowlist boundaries.

## WHERE TO LOOK
- Statistics and analytics browser coverage: `e2e/shared-chart-statistics.spec.ts`, `e2e/statistics-ttft.spec.ts`, `e2e/statistics-token-rate.spec.ts`, `e2e/statistics-filtered-totals.spec.ts`, `e2e/statistics-proxy-api-key-label.spec.ts`, `e2e/analytics-websocket-native.spec.ts`
- Model/access-target browser and seam coverage: `e2e/settings-config-import.spec.ts`, `lib/model_form_state_contract.test.mjs`, `lib/config_import_validation_contract.test.mjs`
- Dashboard routing flow seam coverage: `lib/dashboard_routing_flow_layout_contract.test.mjs`, `e2e/dashboard-routing-shell.spec.ts`
- Shared contract seams: `lib/*.test.mjs`, `loadbalance/*.test.mjs`, `main/*.test.mjs`, `model-detail/*.test.mjs`, `server/*.test.mjs`
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
- Keep websocket, selected-profile, and typed-client contract tests separate from Playwright route flows.
- Do not invent extra test roots or child AGENTS files unless a subtree has a distinct runner or command boundary like `e2e/` and `lib/`.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.
