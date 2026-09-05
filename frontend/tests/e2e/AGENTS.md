# Playwright journeys

- Run `pnpm run test:e2e -- <playwright args>` from `frontend/`. `../../playwright.config.ts` discovers this directory, uses one worker, and starts Vite at `http://127.0.0.1:15174`. Keep the worker setting for deterministic route mocks/auth propagation.
- Existing environment wiring supports `PLAYWRIGHT_BASE_URL`, `PLAYWRIGHT_DISABLE_WEBSERVER`, `PLAYWRIGHT_CHANNEL`, and `PLAYWRIGHT_EXECUTABLE_PATH`; browser availability is an execution prerequisite, not an excuse to change test behavior.
- `pnpm run test:e2e:closed-loop -- <playwright args>` uses `../../scripts/run-playwright-closed-loop.mjs`: pinned locally available Playwright image, read-only source mounts, no image pull, disposable process namespace, and checked Vite shutdown. Keep evidence scripts separate from CI journey specs.
- Mock backend/runtime/health calls explicitly, and fail unexpected API traffic. Reuse `dashboard-aggregate-fixtures.ts`, `request-log-dedicated-audit-fixtures.ts`, `model-detail-catalog-fixtures.ts`, and `spending-report-fixtures.ts` where their evidence fits.
- Use canonical router paths; legacy inputs assert the documented redirect. Seed only the session state the journey needs. Coordinate asynchronous behavior with `expect.poll` rather than fixed sleeps.
- Keep journey scope and replacement policy aligned with the parent-linked canonical test policy. A new spec must own a distinct required journey; pure parser/client/layout checks belong to Node/Vitest.
- Catalog/Pi journeys mock backend catalog routes and must not fetch external catalogs from the browser. Model-export coverage preserves explicit candidate selection, authoritative binding re-read, and deterministic generated content; model-access authoring covers direct-entry and upstream identity behavior.
- Request/audit and streaming-payload journeys own investigation navigation/capture boundaries; they must not widen ordinary browsing into audit-payload fetching.
