# FRONTEND ENDPOINTS FEATURE KNOWLEDGE BASE

## OVERVIEW
`features/endpoints/` owns the `/route/endpoints` page: orchestration, the compact responsive table with direct-reference disclosure, per-Endpoint reference state machines, endpoint form (save-only / save-and-verify), and the reference-derived filter/sort contract.

## STRUCTURE
```text
endpoints/
├── EndpointsFeaturePage.tsx    # Page shell, toolbar (search + filter Select), dialog wiring
├── EndpointTable.tsx           # Desktop compact table + mobile description-list cards + disclosure
├── EndpointDialog.tsx          # Create/edit form, key identity summary, URL preview, save/verify dual state
├── endpointSchemas.ts          # Form schema parity (128/512), canonical URL preview, payload builders
├── useEndpointsFeatureData.ts  # Page coordinator: list/references reconciliation, CRUD, verify, delete machine, orphan cleanup, attach
└── useEndpointReferences.ts    # Per-Endpoint summary/detail state machines + chunked batch coordinator
```

## WHERE TO LOOK
- Page coordinator and mutation flows: `useEndpointsFeatureData.ts`
- Reference state machines (loading/ready/stale/error, generations, batch chunking, atomic summary replacement): `useEndpointReferences.ts`
- Table/disclosure rendering and responsive cards: `EndpointTable.tsx`
- Delete/orphan/attach dialogs: `../../pages/endpoints/`
- Typed error guards and API methods: `../../lib/api/endpointErrors.ts`, `../../lib/api/management.ts`

## CONVENTIONS
- Follow `frontend/DESIGN.md` for all UI/UX work: prefer `@/shared/design-system` operator components, preserve the Material 3 operator direction, use semantic tokens and density variables.
- Reference-derived filter/sort is disabled whenever any summary is unknown or stale; the filter normalizes to `all` with a visible explanation while text search keeps working.
- Detail succeeds atomically replace the batch summary for that Endpoint; generations discard superseded responses; stale/cursor errors restart pagination from page one.
- No reducer or selector may coerce loading/error/stale reference state to an empty summary, and delete confirmation requires a fresh zero-reference preflight (lock-time `409 endpoint_in_use` replaces the dialog page).
- Save-and-verify is two ordered phases: commit first, then verify against the committed `config_revision`/fingerprint; negative outcomes keep the saved state and render inline; late/stale results never claim currency.
- Key form input stays empty (autocomplete off/new-password); server responses always replace the local Endpoint DTO; same-key updates never claim rotation.
