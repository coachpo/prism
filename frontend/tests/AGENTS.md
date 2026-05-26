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
- `../playwright.config.ts` uses `http://127.0.0.1:15174` as the web server target.
- Same-origin launcher coverage lives in `e2e/launcher-same-origin-realtime.spec.ts`; sidecar browser coverage lives in `e2e/sidecars.spec.ts`.
- Startup bootstrap coverage lives in `e2e/settings-startup-tab.spec.ts`.
- Request-log/detail coverage lives in `e2e/request-log-*.spec.ts`, `e2e/request-log-detail-copy.spec.ts`, `e2e/request-log-audit-disabled-state.spec.ts`, `e2e/request-logs-token-rate.spec.ts`, `e2e/request-logs-ttft.spec.ts`, and `e2e/request-logs-optional-zero.spec.ts`.
- Model-detail handoff and connection-probe coverage lives in `e2e/model-detail-request-logs-handoff.spec.ts`, `e2e/model-detail-connection-dialog-probe.spec.ts`, `e2e/proxy-model-detail-authoring.spec.ts`, `model-detail/*.test.mjs`, and `../tests/lib/profile_scope_header_contract.test.mjs`.
- Contract tests for build/runtime seams stay outside `e2e/`.

## WHERE TO LOOK
- Browser flow coverage: `e2e/`, `e2e/sidecars.spec.ts`, `e2e/settings-startup-tab.spec.ts`, `e2e/model-detail-request-logs-handoff.spec.ts`, `../playwright.config.ts`
- Shared contract seams: `lib/*.test.mjs`, `loadbalance/*.test.mjs`, `main/*.test.mjs`, `model-detail/*.test.mjs`, `server/*.test.mjs`, `../tests/lib/profile_scope_header_contract.test.mjs`
- Shared test helpers: `helpers/loadTsModule.mjs`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this doc summary-level.
- Keep browser flows in `e2e/` and seam contracts in the named sibling folders.
- Keep shared test-only utilities in `helpers/` instead of scattering loader glue across suites.
- Keep runtime-path and profile-scope contract tests separate from Playwright route flows.
- Do not invent extra test roots or child AGENTS files.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.
