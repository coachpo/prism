# FRONTEND E2E TEST KNOWLEDGE BASE

## OVERVIEW
`frontend/tests/e2e/` owns Playwright browser flows for mounted Prism routes, shell behavior, mocked backend APIs, and UI state.

## STRUCTURE
```text
e2e/
├── *dashboard*.spec.ts, *statistics*.spec.ts  # Overview and analytics route flows
├── request-log*.spec.ts, request-logs*.spec.ts # Request list/detail/audit flows
├── settings-*.spec.ts                         # Settings, config, retention
└── task-*.spec.ts                             # Feature milestone browser coverage
```

## WHERE TO LOOK
- Playwright config and server target: `../../playwright.config.ts`
- Dashboard/statistics shared fixtures: `dashboard-aggregate-fixtures.ts`
- Auth and shell flows: `auth-session-lifecycle.spec.ts`, `protected-shell-sidebar.spec.ts`
- Request-log and audit page flows: `request-log-*.spec.ts`, `request-logs-*.spec.ts`
- Settings/config/retention flows: `settings-*.spec.ts`
- Model, endpoint, pricing, and Ban Policy flows: `model-*.spec.ts`, `models-*.spec.ts`, `task-*.spec.ts`, `loadbalance-*.spec.ts`, `pricing-*.spec.ts`

## CONVENTIONS
- Run through `pnpm run test:e2e -- <playwright args>`.
- Default browser target is `http://127.0.0.1:15174`; override with `PLAYWRIGHT_BASE_URL` or disable the web server with `PLAYWRIGHT_DISABLE_WEBSERVER=1`.
- Mock backend traffic with `page.route("**/*")`, fulfill known `/api`, `/v1`, `/v1beta`, and `/health` paths, and fail unexpected API calls.
- Seed profile/session state explicitly in browser storage when a flow depends on it.
- Use canonical routes from `src/app/router/rewriteRoutes.ts`; legacy-route specs should assert redirects, not treat legacy paths as primary.
- Use `expect.poll` for asynchronous UI state instead of fixed sleeps.
- Keep browser assertions at route-flow level. Pure parser, API-client, and layout contracts belong in `../lib/` or `../src/**/*.test.*`.

## ANTI-PATTERNS
- Do not add real backend dependencies to e2e specs.
- Do not duplicate large fixture payloads when `dashboard-aggregate-fixtures.ts` or local builders already cover them.
- Do not use e2e specs for one-function contract checks.
