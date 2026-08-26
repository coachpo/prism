# FRONTEND ENDPOINTS FEATURE KNOWLEDGE BASE

## OVERVIEW
`features/endpoints/` owns the `/route/endpoints` page: page composition, the compact responsive table with direct-reference disclosure, Endpoint reference lifecycles, the endpoint form (save-only / save-and-verify), and the reference-derived filter/sort contract.

## STRUCTURE
```text
endpoints/
├── EndpointsFeaturePage.tsx    # Page shell, toolbar (search + filter Select), dialog wiring
├── EndpointTable.tsx           # Endpoint table shell, sorting, and responsive composition
├── EndpointRows.tsx             # Desktop/mobile endpoint row rendering
├── EndpointReferenceDisclosure.tsx # Reference summary/detail disclosure rendering
├── EndpointDialog.tsx          # Create/edit form, key identity summary, URL preview, save/verify dual state
├── endpointSchemas.ts          # Form schema parity (128/512), canonical URL preview, payload builders
├── useEndpointsFeatureData.ts  # Page coordinator: list owner + mutation owner handoff
├── useEndpointList.ts          # Endpoint cache load, search, reference filters, sort, and reference reconciliation
├── useEndpointMutations.ts     # Page mutation coordinator for the resource-specific workflow owners
├── useEndpointFormMutations.ts # Endpoint create/update and ordered save-then-verify workflow
├── useEndpointDeletion.ts      # Fresh delete preflight, blocker pagination, CAS/409 race state
├── useEndpointOrphanCleanup.ts # Ownerless connection cleanup and post-mutation invalidation
├── useEndpointDuplication.ts   # Duplicate request state and server DTO insertion
├── useEndpointAttachment.ts    # One-shot attach-to-model navigation state
├── useEndpointReferenceSummaries.ts # Chunked batch summary hydration and fresh/stale/error state
├── useEndpointReferenceDetails.ts   # Per-Endpoint detail snapshot and cursor pagination state
└── useEndpointReferences.ts    # Shared generation fencing and summary/detail composition
```

## WHERE TO LOOK
- Page coordinator: `useEndpointsFeatureData.ts`
- Endpoint list/cache, reference reconciliation, filters, and sort: `useEndpointList.ts`
- Endpoint mutation composition: `useEndpointMutations.ts`
- Form CRUD and save-then-verify: `useEndpointFormMutations.ts`
- Delete preflight, blocker pagination, and lock-time `409` replacement: `useEndpointDeletion.ts`
- Orphan cleanup, duplication, and attach navigation: `useEndpointOrphanCleanup.ts`, `useEndpointDuplication.ts`, `useEndpointAttachment.ts`
- Batch summary state (loading/ready/stale/error, chunking, generations, atomic replacement): `useEndpointReferenceSummaries.ts`
- Per-Endpoint detail snapshot and cursor pagination: `useEndpointReferenceDetails.ts`
- Shared generation fence and summary/detail composition: `useEndpointReferences.ts`
- Table/disclosure rendering and responsive cards: `EndpointTable.tsx`
- Delete/orphan/attach dialogs: `../../pages/endpoints/`
- Typed error guards and API methods: `../../lib/api/endpointErrors.ts`, `../../lib/api/endpoints.ts`

## CONVENTIONS
- Follow `frontend/DESIGN.md` for all UI/UX work: prefer `@/shared/design-system` operator components, preserve the Material 3 operator direction, use semantic tokens and density variables.
- Reference-derived filter/sort is disabled whenever any summary is unknown or stale; the filter normalizes to `all` with a visible explanation while text search keeps working.
- Endpoint table shell, row rendering, and reference disclosure are split across `EndpointTable.tsx`, `EndpointRows.tsx`, and `EndpointReferenceDisclosure.tsx`; `summaryFor` belongs to `useEndpointReferenceSummaries.ts` and the disclosure state belongs to its matching summary/detail owner.
- `useEndpointReferenceSummaries.ts` owns bounded batch chunking, generation-tagged atomic replacement, and fresh/stale/error projection. `useEndpointReferenceDetails.ts` owns lazy per-Endpoint snapshots, cursor append, deduplication, and restart-on-mismatch. `useEndpointReferences.ts` owns only their shared generation fence and composition.
- Detail succeeds atomically replace the batch summary for that Endpoint; generations discard superseded responses; stale/cursor errors restart pagination from page one.
- No reducer or selector may coerce loading/error/stale reference state to an empty summary, and delete confirmation requires a fresh zero-reference preflight (lock-time `409 endpoint_in_use` replaces the dialog page).
- `useEndpointList.ts` owns Endpoint cache load, text search, reference-derived filter/sort, and visible-ID reconciliation. `useEndpointFormMutations.ts`, `useEndpointDeletion.ts`, `useEndpointOrphanCleanup.ts`, `useEndpointDuplication.ts`, and `useEndpointAttachment.ts` each own one mutation or navigation lifecycle; `useEndpointMutations.ts` only composes those page-facing workflows.
- Save-and-verify is two ordered phases: commit first, then verify against the committed `config_revision`/fingerprint; negative outcomes keep the saved state and render inline; late/stale results never claim currency.
- Key form input stays empty (autocomplete off/new-password); server responses always replace the local Endpoint DTO; same-key updates never claim rotation.
