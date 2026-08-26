# FRONTEND ENDPOINTS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/endpoints/` keeps endpoint dialogs still imported by the feature-owned `/route/endpoints` route under `src/features/endpoints/`. The feature route owns page orchestration, reference state machines, and endpoint form state.

## STRUCTURE
```text
endpoints/
├── DeleteEndpointDialog.tsx     # Delete preflight state machine (checking/eligible/blocked/check_error/integrity_error/deleting), typed 409 race handling
├── OrphanCleanupDialog.tsx      # Separate destructive confirmation for ownerless connections
└── AttachToModelDialog.tsx      # Model picker for the one-shot attach-to-model flow
```

## WHERE TO LOOK
- Feature route, compact table, list/reconciliation state, reference owners, and mutation workflows: `../../features/endpoints/`
- Delete preflight machine and race `409` replacement: `DeleteEndpointDialog.tsx`
- Orphan cleanup and attach dialogs: `OrphanCleanupDialog.tsx`, `AttachToModelDialog.tsx`
- Batch reference summary state: `../../features/endpoints/useEndpointReferenceSummaries.ts`
- Per-Endpoint detail snapshot/cursor state: `../../features/endpoints/useEndpointReferenceDetails.ts`
- Shared generation fence and composition: `../../features/endpoints/useEndpointReferences.ts`
- Endpoint delete state: `../../features/endpoints/useEndpointDeletion.ts`
- Typed Endpoint error guards: `../../lib/api/endpointErrors.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Reuse the shared endpoint cache in `@/lib/referenceData`; reference summaries are auxiliary per-Endpoint snapshots keyed by Endpoint ID, and paged detail is a separate lazy snapshot fetched only for disclosure/delete.
- Unknown/stale reference state never equals zero: filter/sort disable, delete preflight fails closed, and no reducer may coerce non-ready state to an empty summary.
- Desktop renders the compact six-column table; narrow viewports use semantic description-list row cards with no horizontal table scroll.
- Reference disclosure and delete pagination follow the same opaque snapshot cursor; stale/cursor errors discard accumulated pages and restart from page one.
- The one-shot attach flow carries only `action=create-terminal-target` + `endpoint_id` in router state; key material and fingerprints never enter router state.
