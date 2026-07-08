# FRONTEND ENDPOINTS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/endpoints/` keeps endpoint widgets still imported by the feature-owned `/route/endpoints` route under `src/features/endpoints/`. The feature route owns page orchestration and endpoint form state.

## STRUCTURE
```text
endpoints/
├── DeleteEndpointDialog.tsx    # Delete confirmation flow reused by the feature route
├── EndpointCard.tsx            # Endpoint card presentation and move controls
├── endpointCardHelpers.ts      # Card display helpers
├── useEndpointBootstrapData.ts # Shared-cache bootstrap for endpoints and reachable models
└── useEndpointReorder.ts       # Move up/down helpers, optimistic reorder, rollback, and review-mode reorder guards
```

## WHERE TO LOOK
- Feature route and endpoint form state: `../../features/endpoints/`
- Delete confirmation: `DeleteEndpointDialog.tsx`
- Card rendering, move controls, and reachable-model display: `EndpointCard.tsx`, `endpointCardHelpers.ts`
- Shared endpoint cache and reorder helpers: `useEndpointBootstrapData.ts`, `useEndpointReorder.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Reuse the shared endpoint cache in `@/lib/referenceData` instead of layering another endpoint-specific cache.
- Keep reorder state and move bookkeeping in `useEndpointReorder.ts`; cards stay presentational.
- Patch local endpoint state through the feature data hook after create, update, duplicate, delete, and reorder flows.
