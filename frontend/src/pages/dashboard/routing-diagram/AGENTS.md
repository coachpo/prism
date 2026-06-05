# FRONTEND DASHBOARD ROUTING DIAGRAM KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/routing-diagram/` owns the routing visualization internals behind `../routingDiagram.ts` and `../RoutingDiagramCard.tsx`: backend-aligned contracts, layout math, shared rendering helpers, and the desktop/mobile React Flow implementation.

## STRUCTURE
```
routing-diagram/
├── routingDiagramContracts.ts
├── routingDiagramLayout.ts
├── routingDiagramFlowLayout.ts
├── routingDiagramFlowEdgeStyle.ts
├── routingDiagramChartUtils.ts
├── RoutingDiagramFlow.tsx
├── RoutingDiagramFlowEdge.tsx
├── RoutingDiagramFlowNode.tsx
├── RoutingDiagramInspectorContent.tsx
├── RoutingDiagramLegend.tsx
├── RoutingDiagramMobileList.tsx
└── RoutingDiagramVisualizationShell.tsx
```

## WHERE TO LOOK

- Public barrel and parent card entrypoints: `../routingDiagram.ts`, `../RoutingDiagramCard.tsx`
- Backend-aligned diagram payload contracts: `routingDiagramContracts.ts`
- Layout math and flow layout adapters: `routingDiagramLayout.ts`, `routingDiagramFlowLayout.ts`, `routingDiagramFlowEdgeStyle.ts`
- Shared rendering helpers for node labels, state, and route health: `routingDiagramChartUtils.ts`
- React Flow desktop rendering, visualization shell, inspector content, node rendering, legend, and mobile list rendering: `RoutingDiagramFlow.tsx`, `RoutingDiagramVisualizationShell.tsx`, `RoutingDiagramFlowEdge.tsx`, `RoutingDiagramFlowNode.tsx`, `RoutingDiagramInspectorContent.tsx`, `RoutingDiagramLegend.tsx`, `RoutingDiagramMobileList.tsx`
- E2E seam for routing shell chrome, model-node activation, aggregate strategy counts, and exact request-log handoff: `../../../../tests/e2e/dashboard-routing-shell.spec.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep parent consumers on the `../routingDiagram.ts` barrel instead of importing these files ad hoc.
- Keep diagram-specific layout math local to this cluster.
- Keep rendering helpers focused on presentation concerns; backend-owned `RoutingDiagramData` is the source payload.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild routing-diagram payload aggregation in `RoutingDiagramCard.tsx`, the dashboard hooks, or this cluster; consume backend `routing_health_map` data.
- Do not reintroduce route-source REST fan-out or realtime route patches when the dashboard snapshot already carries canonical diagram data.
- Do not couple chart or node rendering directly to page-shell state when the barrel and helper files already own the diagram contract.
