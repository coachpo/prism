# FRONTEND DASHBOARD DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/` owns the main landing page data flow under `../DashboardPage.tsx`: initial bootstrap, realtime reconciliation, KPI cards, recent activity, top-spend summaries, dual-strategy summary wording, and the parent-covered routing visualization cluster.

## STRUCTURE
```
dashboard/
├── useDashboardPageData.ts         # High-level page composition
├── useDashboardBootstrapData.ts    # Parallel bootstrap fetches and routing payload load
├── useDashboardRealtime.ts         # Realtime subscription and coalesced reconciliation
├── DashboardMetricsGrid.tsx        # KPI grid and highlighted metrics
├── DashboardHighlightsGrid.tsx     # Summary and api-family highlights
├── RecentActivityCard.tsx          # Recent requests list with insert highlighting
├── TopSpendingModelsCard.tsx       # Top-spend summary card
├── RoutingDiagramCard.tsx          # Routing diagram shell and drill-down entry points
├── routingDiagram.ts               # Barrel over routing-diagram internals
└── routing-diagram/                # Diagram layout, legend, tooltip, nodes, links, and render helpers
```

## WHERE TO LOOK

- Thin route shell: `../DashboardPage.tsx`
- High-level dashboard composition: `useDashboardPageData.ts`
- Initial bootstrap fan-out and routing payload shaping: `useDashboardBootstrapData.ts`
- Realtime payload flow: `useDashboardRealtime.ts`, which reconciles the backend `dashboard.update` payload
- Routing visualization barrel and parent-covered local cluster: `routingDiagram.ts`, `RoutingDiagramCard.tsx`, `routing-diagram/`
- KPI, highlight, recent-activity, and spend presentation: `DashboardMetricsGrid.tsx`, `DashboardHighlightsGrid.tsx`, `RecentActivityCard.tsx`, `TopSpendingModelsCard.tsx`

## CONVENTIONS

- Keep dashboard live state on `useDashboardRealtime.ts` and the shared `useRealtimeData()` hook.
- Reconnect and manual refresh should reconcile through REST bootstrap data. The backend push contract is still `dashboard.update` only.
- Treat `routingDiagram.ts` as the barrel entrypoint for routing visualization. Keep the deeper `routing-diagram/` internals local to this parent doc instead of adding another AGENTS file.
- Keep presentation components focused on rendering. Bootstrap, payload shaping, and merge logic belong in the dashboard hooks.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not bypass `useDashboardRealtime.ts` for dashboard-specific live state.
- Do not hard-code routing-diagram data assembly in card components when `routingDiagram.ts` already fronts that local cluster.
- Do not split `routing-diagram/` into its own AGENTS file. This parent doc owns that cluster.
