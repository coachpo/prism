# FRONTEND E2E TEST KNOWLEDGE BASE

## OVERVIEW
`frontend/tests/e2e/` owns Prism's capped Playwright journey suite: exactly five mocked browser flows for mounted routes, shell behavior, and UI state.

## STRUCTURE
```text
e2e/
├── auth-session-lifecycle.spec.ts
├── loadbalance-strategies-recovery.spec.ts
├── models-access-target-authoring.spec.ts
├── request-log-dedicated-audit-page.spec.ts
├── shared-chart-statistics.spec.ts
└── dashboard-aggregate-fixtures.ts
```

## WHERE TO LOOK
- Playwright config and server target: `../../playwright.config.ts`
- Shared dashboard/statistics fixture builders: `dashboard-aggregate-fixtures.ts`
- Auth journey: `auth-session-lifecycle.spec.ts`
- Load-balance recovery journey: `loadbalance-strategies-recovery.spec.ts`
- Model access-target authoring journey: `models-access-target-authoring.spec.ts`
- Request-log + audit journey: `request-log-dedicated-audit-page.spec.ts`
- Shared statistics/chart journey: `shared-chart-statistics.spec.ts`

## CONVENTIONS
- Run through `pnpm run test:e2e -- <playwright args>`.
- Default browser target is `http://127.0.0.1:15174`; override with `PLAYWRIGHT_BASE_URL` or disable the web server with `PLAYWRIGHT_DISABLE_WEBSERVER=1`.
- Mock backend traffic with `page.route("**/*")`, fulfill known `/api`, `/v1`, `/v1beta`, and `/health` paths, and fail unexpected API calls.
- Seed profile/session state explicitly in browser storage when a flow depends on it.
- Use canonical routes from `src/app/router/rewriteRoutes.ts`; legacy-route specs should assert redirects, not treat legacy paths as primary.
- Use `expect.poll` for asynchronous UI state instead of fixed sleeps.
- Keep browser assertions at route-flow level. Pure parser, API-client, and layout contracts belong in `../lib/` or `../src/**/*.test.*`.
- This directory is capped at exactly five journey specs. Adding one requires deleting one in the same change and updating this file.

## ANTI-PATTERNS
- Do not add real backend dependencies to e2e specs.
- Do not duplicate large fixture payloads when `dashboard-aggregate-fixtures.ts` or local builders already cover them.
- Do not use e2e specs for one-function contract checks.
- Do not add "local-only" Playwright specs or grow past the five-spec cap.
