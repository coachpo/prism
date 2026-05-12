# FRONTEND TEST BOUNDARY

## OVERVIEW
`frontend/tests/` is Prism's frontend regression surface. It splits browser flows from contract seams and keeps the tree aligned with the current route and provider structure.

## TEST SPLIT
- `e2e/` holds Playwright route flows.
- `lib/`, `loadbalance/`, `main/`, `model-detail/`, and `server/` hold focused contract seams.
- `helpers/` holds shared test-only utilities such as TypeScript module loading.

## CURRENT FACTS
- `../package.json` exposes the frontend regression entrypoint as `pnpm run test:e2e`.
- `../playwright.config.ts` points Playwright at `./tests/e2e`.
- `../playwright.config.ts` uses `http://127.0.0.1:4173` as the web server target.
- Same-origin launcher coverage lives in `e2e/launcher-same-origin-realtime.spec.ts`; sidecar browser coverage lives in `e2e/sidecars.spec.ts`; contract tests for build/runtime seams stay outside `e2e/`.

## WHERE TO LOOK
- Browser flow coverage: `e2e/`, `e2e/sidecars.spec.ts`, `../playwright.config.ts`
- Shared contract seams: `lib/*.test.mjs`, `loadbalance/*.test.mjs`, `main/*.test.mjs`, `model-detail/*.test.mjs`, `server/*.test.mjs`
- Shared test helpers: `helpers/loadTsModule.mjs`

## CONVENTIONS

- When doing upgrade work, first account for this project stage: This application is under development, it doesn't have users at the moment. Backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested; prefer the best current implementation shape over preserving the old one, and do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.
- Keep this doc summary-level.
- Keep browser flows in `e2e/` and seam contracts in the named sibling folders.
- Keep shared test-only utilities in `helpers/` instead of scattering loader glue across suites.
- Do not invent extra test roots or child AGENTS files.
