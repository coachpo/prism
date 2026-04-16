# FRONTEND TEST BOUNDARY

## OVERVIEW
`frontend/tests/` is Prism's frontend regression surface. It splits browser flows from contract seams and keeps the tree aligned with the current route and provider structure.

## TEST SPLIT
- `tests/e2e/` holds Playwright route flows.
- `tests/lib/` holds contract tests.
- `tests/loadbalance/`, `tests/main/`, `tests/model-detail/`, and `tests/server/` hold contract seams for those areas.

## CURRENT FACTS
- `playwright.config.ts` points Playwright at `./tests/e2e`.
- `playwright.config.ts` uses `http://127.0.0.1:4173` as the web server target.

## WHERE TO LOOK
- Browser flow coverage: `e2e/`, `../playwright.config.ts`
- Shared contract seams: `lib/*.test.mjs`, `loadbalance/*.test.mjs`
- Bootstrap and route-entry seams: `main/*.test.mjs`, `model-detail/*.test.mjs`, `server/*.test.mjs`

## CONVENTIONS
- Keep this doc summary-level.
- Keep browser flows in `tests/e2e/` and seam contracts in the other test folders.
- Do not invent extra test roots or child AGENTS files.
