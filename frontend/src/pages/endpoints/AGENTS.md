# FRONTEND ENDPOINTS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/endpoints/` keeps endpoint widgets still imported by the feature-owned `/route/endpoints` route under `src/features/endpoints/`. The feature route owns page orchestration and endpoint form state.

## STRUCTURE
```text
endpoints/
├── DeleteEndpointDialog.tsx    # Delete confirmation flow reused by the feature route
├── EndpointCard.tsx            # Sortable endpoint card + overlay presentation
├── endpointCardHelpers.ts      # Card display helpers
├── useEndpointBootstrapData.ts # Shared-cache bootstrap for endpoints and reachable models
└── useEndpointReorder.ts       # Drag sensors, optimistic reorder, rollback, and review-mode reorder guards
```

## WHERE TO LOOK
- Feature route and endpoint form state: `../../features/endpoints/`
- Delete confirmation: `DeleteEndpointDialog.tsx`
- Sortable card rendering and reachable-model display: `EndpointCard.tsx`, `endpointCardHelpers.ts`
- Shared endpoint cache and reorder helpers: `useEndpointBootstrapData.ts`, `useEndpointReorder.ts`

## CONVENTIONS
- Reuse the shared endpoint cache in `@/lib/referenceData` instead of layering another endpoint-specific cache.
- Keep reorder state and DnD bookkeeping in `useEndpointReorder.ts`; cards stay presentational.
- Patch local endpoint state through the feature data hook after create, update, duplicate, delete, and reorder flows.
