# FRONTEND E2E TEST KNOWLEDGE BASE

## OVERVIEW

`frontend/tests/e2e/` owns Prism's mocked Playwright journey suite for mounted routes, shell behavior, UI state, responsive evidence, and accessibility evidence. The suite is intentionally bounded by scenario ownership; it is not limited to an arbitrary fixed number of spec files.

## STRUCTURE

```text
e2e/
├── auth-session-lifecycle.spec.ts
├── loadbalance-strategies-recovery.spec.ts
├── model-catalog-pricing.spec.ts
├── model-export-journey.spec.ts
├── models-access-target-authoring.spec.ts
├── request-log-dedicated-audit-page.spec.ts
├── routing-health-events.spec.ts
├── shared-chart-statistics.spec.ts
├── narrow-accessibility-evidence.spec.ts
├── request-log-streaming-payloads.spec.ts
├── terminal-target-custom-request-parameters.spec.ts
├── spending-report-fixtures.ts   # Shared ingress SpendingReport/query contract fixture
├── capture-a11y-evidence.mjs      # Accessibility evidence capture (not a journey spec)
├── capture-endpoint-evidence.mjs  # Endpoint evidence capture (not a journey spec)
├── capture-settings-evidence.mjs  # Responsive Settings evidence capture (not a journey spec)
├── dashboard-aggregate-fixtures.ts
└── request-log-dedicated-audit-fixtures.ts
```

## WHERE TO LOOK

- Playwright config and server target: `../../playwright.config.ts`
- Shared dashboard/statistics fixture builders: `dashboard-aggregate-fixtures.ts`
- Shared model-detail Spending response and exact ingress-query assertion: `spending-report-fixtures.ts`
- Auth journey: `auth-session-lifecycle.spec.ts`
- Load-balance recovery journey: `loadbalance-strategies-recovery.spec.ts`
- models.dev catalog binding and source-linked price-import journeys: `model-catalog-pricing.spec.ts` (mocked `/api/models/{id}/catalog*`, `/api/models/{id}/pi`, and `/api/pricing-templates/catalog/*`; binding candidate pagination covers `20 → 40 → total` and selects candidates through the shared listbox options, delayed replace/append isolation and same-offset retry, model detail binds then unbinds with the displayed coordinate/`updated_at` snapshot and authoritatively re-reads, and catalog price commit keeps the current target locked; `/route/pricing` covers unique match with no target, paged explicit candidate selection, and target-set re-preview)
- Pi client-export journey: `model-export-journey.spec.ts` (uncatalogued Prism id → backend-only bounded directory search carrying the `limit`/`offset` paging fields → explicit cross-directory bind through the shared controller while preserving separate live-candidate evidence, with full seven-field evidence asserted after selection → deterministic render/download; asserts no browser request reaches pi.dev and no unexpected API/page errors are tolerated)
- Model access-target authoring journey: `models-access-target-authoring.spec.ts`
- Request-log + audit journey: `request-log-dedicated-audit-page.spec.ts`; shared request-log/audit fixture builders: `request-log-dedicated-audit-fixtures.ts`
- Routing-health events journey: `routing-health-events.spec.ts`
- Shared statistics/chart journey: `shared-chart-statistics.spec.ts`
- Settings responsive/a11y evidence: `capture-settings-evidence.mjs`; it uses bounded mocked owner snapshots at 1680, 1200, and narrow keyboard-focus viewports and is kept separate from the owned journey-spec set.

## CONVENTIONS

- Run through `pnpm run test:e2e -- <playwright args>`.
- Closed-loop campaigns use `pnpm run test:e2e:closed-loop -- <playwright args>` or invoke its Node runner directly. It pins the local Playwright image, keeps Vite and Chromium inside a disposable container PID namespace, mounts project source read-only, and verifies Vite shutdown without allowing a network pull.
- The checked-in Playwright config uses one worker so per-test catch-all API route tables, auth/session propagation, and the local Vite module graph remain deterministic; do not override it in the verification campaign.
- Default browser target is `http://127.0.0.1:15174`; override with `PLAYWRIGHT_BASE_URL` or disable the web server with `PLAYWRIGHT_DISABLE_WEBSERVER=1`.
- When the bundled Playwright revision is unavailable, use `PLAYWRIGHT_CHANNEL=chrome` or the evidence runner/config's `PLAYWRIGHT_EXECUTABLE_PATH`; browser process permissions remain an environment prerequisite.
- Mock backend traffic with `page.route("**/*")`, fulfill known `/api`, `/v1`, `/v1beta`, and `/health` paths, and fail unexpected API calls.
- Seed profile/session state explicitly in browser storage when a flow depends on it.
- Use canonical routes from `src/app/router/rewriteRoutes.ts`; legacy-route specs should assert redirects, not treat legacy paths as primary.
- Use `expect.poll` for asynchronous UI state instead of fixed sleeps.
- Keep browser assertions at route-flow level. Pure parser, API-client, and layout contracts belong in `../lib/` or `../src/**/*.test.*`.
- Keep one spec per owned journey or evidence scenario. Additions must remain bounded, use existing fixtures where possible, and update this file when the scenario inventory changes; do not use an arbitrary file-count cap as a reason to omit required Settings, routing, request/audit, or accessibility coverage.

## ANTI-PATTERNS

- Do not add real backend dependencies to e2e specs.
- Do not duplicate large fixture payloads when `dashboard-aggregate-fixtures.ts` or local builders already cover them.
- Do not use e2e specs for one-function contract checks.
- Do not add "local-only" Playwright specs or duplicate an existing journey/evidence scenario.
