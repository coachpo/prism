# FRONTEND TEST BOUNDARY

## OVERVIEW
`frontend/tests/` is Prism's frontend regression surface. It splits browser flows from contract seams and keeps the tree aligned with the current route and provider structure.

## TEST SPLIT
- `e2e/` holds Playwright route flows.
- `lib/`, `loadbalance/`, `main/`, `model-detail/`, and `server/` hold focused contract seams.
- `helpers/` holds shared test-only utilities such as TypeScript module loading.

## CURRENT FACTS
- `../playwright.config.ts` points Playwright at `./tests/e2e`.
- `../playwright.config.ts` uses `http://127.0.0.1:4173` as the web server target.

## WHERE TO LOOK
- Browser flow coverage: `e2e/`, `../playwright.config.ts`
- Shared contract seams: `lib/*.test.mjs`, `loadbalance/*.test.mjs`, `main/*.test.mjs`, `model-detail/*.test.mjs`, `server/*.test.mjs`
- Shared test helpers: `helpers/loadTsModule.mjs`

## CONVENTIONS
- Keep this doc summary-level.
- Keep browser flows in `e2e/` and seam contracts in the named sibling folders.
- Keep shared test-only utilities in `helpers/` instead of scattering loader glue across suites.
- Do not invent extra test roots or child AGENTS files.
