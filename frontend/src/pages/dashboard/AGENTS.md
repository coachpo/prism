# FRONTEND DASHBOARD DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/` owns the `/dashboard` route shell under `../DashboardPage.tsx`: the overview-versus-analytics tab split, query-param page state, overview bootstrap and realtime reconciliation, analytics handoff into the statistics surface, and the handoff into the nested routing visualization leaf.

## STRUCTURE
```
dashboard/
├── queryParams.ts                  # Overview or analytics tab query-param contract
├── useDashboardPageState.ts        # Query-param parsing and canonicalization
├── DashboardOverviewTab.tsx        # Overview-tab composition over metrics, highlights, routing, and recent activity
├── DashboardAnalyticsContent.tsx   # Analytics-tab handoff into the statistics surface
├── useDashboardPageData.ts         # Overview bootstrap composition and derived dashboard data
├── useDashboardBootstrapData.ts    # Parallel bootstrap fetches and routing payload load
├── useDashboardRealtime.ts         # Realtime subscription and coalesced reconciliation
├── DashboardMetricsGrid.tsx        # KPI grid and highlighted metrics
├── DashboardHighlightsGrid.tsx     # Summary and api-family highlights
├── RecentActivityCard.tsx          # Recent requests list with insert highlighting
├── TopSpendingModelsCard.tsx       # Top-spend summary card
├── RoutingDiagramCard.tsx          # Routing diagram card and drill-down entry points
├── RoutingDiagramShell.tsx         # Diagram card empty, loading, and error shell
├── dashboardDataUtils.ts           # Dashboard-local formatting and shaping helpers
├── DashboardPageSkeleton.tsx       # Overview loading shell
├── routingDiagram.ts               # Barrel over routing-diagram internals
└── routing-diagram/
    ├── AGENTS.md                   # Diagram-local layout, realtime patching, aggregation, and chart helpers
    └── ...
```

## WHERE TO LOOK

- Thin route shell, overview-versus-analytics tab split, and route-level navigation actions: `../DashboardPage.tsx`
- Query-param state and canonical tab contract: `queryParams.ts`, `useDashboardPageState.ts`
- Overview-tab composition boundary: `DashboardOverviewTab.tsx`
- Analytics-tab handoff into the statistics domain: `DashboardAnalyticsContent.tsx`, `../statistics/AGENTS.md`
- High-level overview data composition: `useDashboardPageData.ts`
- Initial bootstrap fan-out and routing payload shaping: `useDashboardBootstrapData.ts`
- Realtime payload flow: `useDashboardRealtime.ts`, which reconciles the backend `dashboard.update` payload
- Routing visualization barrel and leaf cluster: `routingDiagram.ts`, `RoutingDiagramCard.tsx`, `routing-diagram/AGENTS.md`
- KPI, highlight, recent-activity, and spend presentation: `DashboardMetricsGrid.tsx`, `DashboardHighlightsGrid.tsx`, `RecentActivityCard.tsx`, `TopSpendingModelsCard.tsx`, `DashboardPageSkeleton.tsx`

## CHILD DOCS

- `routing-diagram/AGENTS.md`: routing-diagram aggregation, realtime patching, layout math, chart helpers, and render shapes.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep dashboard live state on `useDashboardRealtime.ts` and the shared `useRealtimeData()` hook.
- Keep the overview-versus-analytics tab split on `queryParams.ts` and `useDashboardPageState.ts` instead of local component state.
- Reconnect and manual refresh should reconcile through REST bootstrap data. The backend push contract is still `dashboard.update` only.
- Treat `routingDiagram.ts` as the barrel entrypoint for routing visualization and let `routing-diagram/AGENTS.md` own the layout, aggregation, realtime, and chart-helper split beneath it.
- Keep overview presentation components focused on rendering. Bootstrap, payload shaping, and merge logic belong in the dashboard hooks.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS

- Do not bypass `useDashboardRealtime.ts` for dashboard-specific live state.
- Do not move overview-or-analytics tab state out of `queryParams.ts` and `useDashboardPageState.ts` into ad hoc local state.
- Do not hard-code routing-diagram data assembly in card components when `routingDiagram.ts` already fronts that local cluster.
- Do not duplicate routing-diagram layout, aggregation, or realtime merge logic in page-level hooks or cards when the local leaf cluster already owns it.
