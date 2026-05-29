# FRONTEND ENDPOINTS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/endpoints/` owns endpoint CRUD, review search/filter state, reorder behavior, reachable-model display, and card-level presentation behind `../EndpointsPage.tsx`. This page stays profile-scoped because endpoints are reusable credentials attached to the selected profile.

## STRUCTURE
```
endpoints/
├── EndpointDialog.tsx          # Create-edit form and field normalization
├── DeleteEndpointDialog.tsx    # Delete confirmation flow
├── EndpointCard.tsx            # Sortable endpoint card + overlay presentation
├── endpointCardHelpers.ts      # Card display helpers
├── useEndpointBootstrapData.ts # Shared-cache bootstrap for endpoints and reachable models
├── useEndpointReorder.ts       # Drag sensors, optimistic reorder, rollback, and review-mode reorder guards
└── useEndpointsPageData.ts     # Page-level orchestration for review filters, CRUD, duplication, delete flow, and reorder wiring
```

## WHERE TO LOOK

- Page orchestration and mutation handlers: `useEndpointsPageData.ts`
- Bootstrap and shared endpoint cache updates: `useEndpointBootstrapData.ts`
- Drag sensors, reorder guards, and optimistic order updates: `useEndpointReorder.ts`
- Form fields and endpoint payload shaping: `EndpointDialog.tsx`
- Sortable card rendering and reachable-model display: `EndpointCard.tsx`, `endpointCardHelpers.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Reuse the shared endpoint cache in `@/lib/referenceData` instead of layering another endpoint-specific cache.
- Keep reorder state and DnD bookkeeping in `useEndpointReorder.ts`; cards stay presentational.
- Patch local endpoint state through `commitEndpoints()` after create, update, duplicate, delete, and reorder flows.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not mutate endpoint ordering directly inside card components.
- Do not duplicate endpoint form normalization outside `EndpointDialog.tsx`.
- Do not replace dependency-specific delete messaging with generic toast-only failures when the page already surfaces blocking reasons.
