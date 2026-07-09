# FRONTEND DASHBOARD DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/` owns the legacy tabbed dashboard composition rendered through the `/observe` rewrite route adapter: overview-versus-analytics query state, overview bootstrap and polling reconciliation, and analytics handoff into the statistics surface.

## STRUCTURE
```
dashboard/
├── queryParams.ts                  # Overview or analytics tab query-param contract
├── useDashboardPageState.ts        # Query-param parsing and canonicalization
├── DashboardOverviewTab.tsx        # Overview-tab composition over metrics, highlights, and recent activity
├── DashboardAnalyticsContent.tsx   # Analytics-tab handoff into the statistics surface
├── useDashboardPageData.ts         # Overview bootstrap composition and derived dashboard data
├── useDashboardBootstrapData.ts    # Parallel snapshot, recent-activity, and incident bootstrap
├── useDashboardPolling.ts          # REST polling and coalesced reconciliation
├── DashboardMetricsGrid.tsx        # KPI grid and highlighted metrics
├── DashboardHighlightsGrid.tsx     # Summary and api-family highlights
├── RecentActivityCard.tsx          # Recent requests list with insert highlighting
├── TopSpendingModelsCard.tsx       # Top-spend summary card
└── DashboardPageSkeleton.tsx       # Overview loading shell
```

## WHERE TO LOOK

- Rewrite route adapter: `../../features/observe/ObservePage.tsx`
- Legacy route shell, tab split, and route-level navigation actions: `../DashboardPage.tsx`
- Query-param tab helpers: `queryParams.ts`, `useDashboardPageState.ts`
- Overview-tab composition boundary: `DashboardOverviewTab.tsx`
- Analytics-tab handoff into the dashboard-owned statistics domain, which has no standalone `/statistics` route: `DashboardAnalyticsContent.tsx`, `../statistics/AGENTS.md`
- High-level overview data composition: `useDashboardPageData.ts`
- Initial bootstrap fan-out and snapshot reconciliation: `useDashboardBootstrapData.ts`
- Dashboard polling flow: `useDashboardPolling.ts`, which refreshes REST snapshot and recent activity data through `useDashboardBootstrapData.ts`
- KPI, highlight, recent-activity, and spend presentation: `DashboardMetricsGrid.tsx`, `DashboardHighlightsGrid.tsx`, `RecentActivityCard.tsx`, `TopSpendingModelsCard.tsx`, `DashboardPageSkeleton.tsx`
- E2E seam for dashboard aggregate/statistics rendering: `../../../tests/e2e/shared-chart-statistics.spec.ts`; shared aggregate fixtures live in `../../../tests/e2e/dashboard-aggregate-fixtures.ts`.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep dashboard live refresh on `useDashboardPolling.ts`; hooks own the interval, not components.
- Keep the overview-versus-analytics tab split on `queryParams.ts` and `useDashboardPageState.ts` instead of local component state.
- Polling and manual refresh should reconcile through REST bootstrap data. Snapshot reconciliation uses lexicographic `snapshot_revision`; recent activity reconciliation uses request-log IDs only for feed dedupe and drilldown.
- Keep overview presentation components focused on rendering. Bootstrap, payload shaping, and merge logic belong in the dashboard hooks.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not bypass `useDashboardPolling.ts` for dashboard-specific live refresh.
- Do not move overview-or-analytics tab state out of `queryParams.ts` and `useDashboardPageState.ts` into ad hoc local state.
