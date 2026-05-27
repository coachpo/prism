# FRONTEND DASHBOARD ROUTING DIAGRAM KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/routing-diagram/` owns the routing visualization internals behind `../routingDiagram.ts` and `../RoutingDiagramCard.tsx`: backend-aligned chart contracts, layout math, and diagram-specific render helpers.

## STRUCTURE
```
routing-diagram/
├── routingDiagramContracts.ts
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
- Backend-aligned diagram payload contracts: `routingDiagramContracts.ts`
- Layout math, empty-state shaping, and chart-data helpers: `routingDiagramLayout.ts`, `routingDiagramChartTypes.ts`, `routingDiagramChartUtils.ts`
- Chart shell, node or link shapes, legend, and tooltip rendering: `RoutingDiagramChart.tsx`, `RoutingDiagramChartShell.tsx`, `RoutingDiagramNodeShape.tsx`, `RoutingDiagramLinkShape.tsx`, `RoutingDiagramLegend.tsx`, `RoutingDiagramTooltip.tsx`
- E2E seam for routing shell chrome, model-node activation, aggregate strategy counts, and exact request-log handoff: `../../../../tests/e2e/dashboard-routing-shell.spec.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep parent consumers on the `../routingDiagram.ts` barrel instead of importing these files ad hoc.
- Keep diagram-specific layout math local to this cluster.
- Keep chart and shape components rendering-focused; backend-owned `RoutingDiagramData` is the source payload.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild routing-diagram payload aggregation in `RoutingDiagramCard.tsx`, the dashboard hooks, or this cluster; consume backend `routing_health_map` data.
- Do not reintroduce route-source REST fan-out or realtime route patches when the dashboard snapshot already carries canonical diagram data.
- Do not couple chart components directly to page-shell state when the barrel and helper files already own the diagram contract.
