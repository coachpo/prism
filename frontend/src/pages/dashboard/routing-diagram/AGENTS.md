# FRONTEND DASHBOARD ROUTING DIAGRAM KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/routing-diagram/` owns the routing visualization internals behind `../routingDiagram.ts` and `../RoutingDiagramCard.tsx`: chart contracts, payload aggregation, realtime patching, layout math, and diagram-specific render helpers.

## STRUCTURE
```
routing-diagram/
├── routingDiagramContracts.ts
├── routingDiagramAggregation.ts
├── routingDiagramRealtime.ts
├── routingDiagramLayout.ts
├── routingDiagramChartTypes.ts
├── routingDiagramChartUtils.ts
├── RoutingDiagramChart.tsx
├── RoutingDiagramChartShell.tsx
├── RoutingDiagramLegend.tsx
├── RoutingDiagramTooltip.tsx
├── RoutingDiagramNodeShape.tsx
└── RoutingDiagramLinkShape.tsx
```

## WHERE TO LOOK

- Public barrel and parent card entrypoints: `../routingDiagram.ts`, `../RoutingDiagramCard.tsx`
- Diagram payload contracts and aggregation from dashboard data: `routingDiagramContracts.ts`, `routingDiagramAggregation.ts`
- Realtime patch application for live diagram updates: `routingDiagramRealtime.ts`
- Layout math, empty-state shaping, and chart-data helpers: `routingDiagramLayout.ts`, `routingDiagramChartTypes.ts`, `routingDiagramChartUtils.ts`
- Chart shell, node or link shapes, legend, and tooltip rendering: `RoutingDiagramChart.tsx`, `RoutingDiagramChartShell.tsx`, `RoutingDiagramNodeShape.tsx`, `RoutingDiagramLinkShape.tsx`, `RoutingDiagramLegend.tsx`, `RoutingDiagramTooltip.tsx`

## CONVENTIONS

- Keep parent consumers on the `../routingDiagram.ts` barrel instead of importing these files ad hoc.
- Keep diagram-specific aggregation, realtime patching, and layout math local to this cluster.
- Keep chart and shape components rendering-focused; data shaping belongs in the diagram helpers.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not rebuild routing-diagram payload aggregation in `RoutingDiagramCard.tsx` or the dashboard hooks.
- Do not spread realtime diagram patch handling outside `routingDiagramRealtime.ts`.
- Do not couple chart components directly to page-shell state when the barrel and helper files already own the diagram contract.
