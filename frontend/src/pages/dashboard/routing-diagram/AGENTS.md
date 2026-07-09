# FRONTEND DASHBOARD ROUTING DIAGRAM KNOWLEDGE BASE

## OVERVIEW
`pages/dashboard/routing-diagram/` owns the routing health presentation internals behind `../routingDiagram.ts` and `../RoutingDiagramCard.tsx`: backend-aligned contracts, list data shaping, shared rendering helpers, legend, inspector presentation, and the always-on plain list renderer.

## STRUCTURE
```
routing-diagram/
├── routingDiagramContracts.ts
├── routingDiagramData.ts
├── routingDiagramPresentationUtils.ts
├── RoutingDiagramInspectorContent.tsx
├── RoutingDiagramLegend.tsx
├── RoutingDiagramMobileList.tsx
└── RoutingDiagramVisualizationShell.tsx
```

## WHERE TO LOOK

- Public barrel and parent card entrypoints: `../routingDiagram.ts`, `../RoutingDiagramCard.tsx`
- Backend-aligned diagram payload contracts: `routingDiagramContracts.ts`
- Graph normalization, filtering, summary, empty-state, and list relation shaping: `routingDiagramData.ts`
- Shared rendering helpers for node labels, state, and route health: `routingDiagramPresentationUtils.ts`
- Plain-list rendering, visualization shell, inspector content, and legend: `RoutingDiagramMobileList.tsx`, `RoutingDiagramVisualizationShell.tsx`, `RoutingDiagramInspectorContent.tsx`, `RoutingDiagramLegend.tsx`
- List and data-shaping seam contract: `../../../../tests/lib/dashboard_routing_list_contract.test.mjs`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep parent consumers on the `../routingDiagram.ts` barrel instead of importing these files ad hoc.
- Keep diagram-specific data shaping local to this cluster.
- Keep rendering helpers focused on presentation concerns; backend-owned `RoutingDiagramData` is the source payload.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild routing-diagram payload aggregation in `RoutingDiagramCard.tsx`, the dashboard hooks, or this cluster; consume backend `routing_health_map` data.
- Do not reintroduce route-source REST fan-out or realtime route patches when the dashboard snapshot already carries canonical diagram data.
- Do not couple chart or node rendering directly to page-shell state when the barrel and helper files already own the diagram contract.
