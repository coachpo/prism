# FRONTEND ENDPOINTS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/endpoints/` owns endpoint CRUD, review search/filter state, reorder behavior, and card-level presentation behind `../EndpointsPage.tsx`. This page stays profile-scoped because endpoints are reusable credentials attached to the selected profile.

## STRUCTURE
```
endpoints/
├── EndpointDialog.tsx          # Create-edit form and field normalization
├── DeleteEndpointDialog.tsx    # Delete confirmation flow
├── EndpointCard.tsx            # Sortable endpoint card + overlay presentation
├── endpointCardHelpers.ts      # Card display helpers
├── useEndpointBootstrapData.ts # Shared-cache bootstrap for endpoints and attached models
├── useEndpointReorder.ts       # Drag sensors, optimistic reorder, rollback, and review-mode reorder guards
└── useEndpointsPageData.ts     # Page-level orchestration for review filters, CRUD, duplication, delete flow, and reorder wiring
```

## WHERE TO LOOK

- Page orchestration and mutation handlers: `useEndpointsPageData.ts`
- Bootstrap and shared endpoint cache updates: `useEndpointBootstrapData.ts`
- Drag sensors, reorder guards, and optimistic order updates: `useEndpointReorder.ts`
- Form fields and endpoint payload shaping: `EndpointDialog.tsx`
- Sortable card rendering and model attachment display: `EndpointCard.tsx`, `endpointCardHelpers.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- Reuse the shared endpoint cache in `@/lib/referenceData` instead of layering another endpoint-specific cache.
- Keep reorder state and DnD bookkeeping in `useEndpointReorder.ts`; cards stay presentational.
- Patch local endpoint state through `commitEndpoints()` after create, update, duplicate, delete, and reorder flows.

## ANTI-PATTERNS

- Do not mutate endpoint ordering directly inside card components.
- Do not duplicate endpoint form normalization outside `EndpointDialog.tsx`.
- Do not replace dependency-specific delete messaging with generic toast-only failures when the page already surfaces blocking reasons.
