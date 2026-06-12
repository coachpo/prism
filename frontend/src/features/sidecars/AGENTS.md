# FRONTEND SIDECARS FEATURE KNOWLEDGE BASE

## OVERVIEW
`frontend/src/features/sidecars/` owns the active global sidecar control-plane UI mounted at `/control/sidecars`: sidecar registration/edit/delete, connection testing, manual sync, provider inventory, live auth-file reads and mutations, row-level safety messaging, and sidecar detail composition.

## STRUCTURE
```text
sidecars/
├── SidecarsFeaturePage.tsx    # Route page wrapper and heading
├── SidecarsScaffold.tsx       # Page composition, selected detail, scroll handoff
├── useSidecarsPageData.ts     # Fetching, mutations, refresh, sidecar/auth/provider state
├── sidecarFormState.ts        # Form state, validation, create/update payload builders
├── SidecarsTable.tsx          # Registration table, health/security/sync states
├── SidecarDialog.tsx          # Create/edit dialog
├── DeleteSidecarDialog.tsx    # Delete confirmation
├── AuthFilesTable.tsx         # Live auth-file sorting, filtering, mutation controls
└── ProviderInventoryTable.tsx # Normalized provider snapshot display
```

## WHERE TO LOOK
- Route composition and detail handoff: `SidecarsFeaturePage.tsx`, `SidecarsScaffold.tsx`
- Data loading, selected sidecar refresh, sync/test/delete, auth mutation notices, and provider inventory state: `useSidecarsPageData.ts`
- Sidecar form state, validation, and payload construction: `sidecarFormState.ts`
- Table rendering, health-state classification, and sync/security labels: `SidecarsTable.tsx`
- Auth-file mutation eligibility, stale/unsafe identity messages, provider model loading, and pagination/search: `AuthFilesTable.tsx`
- Backend sidecar management routes and worker ownership: `../../../../backend/internal/httpapi/management/sidecars/AGENTS.md`
- Typed frontend sidecar API surface: `../../lib/api/sidecars.ts`
- Browser coverage: `../../../tests/e2e/sidecars.spec.ts`

## CONVENTIONS
- Treat sidecars as global instance state, not selected-profile state.
- Keep live auth-file mutation messages conservative: stale names, unsafe identities, missing rows, and upstream management failures should refresh state before retry guidance.
- Keep provider inventory display masked; never reveal fields whose names imply secret, token, key, or password.
- Keep form payload normalization in `sidecarFormState.ts` rather than duplicating number/boolean parsing in dialogs.
- Keep manual sync/test/delete flows in the page data hook so tables stay render and event-delegation focused.

## ANTI-PATTERNS
- Do not add selected-profile headers or assumptions to sidecar UI calls.
- Do not mutate auth files from presentational rows without the page data hook refreshing live state.
- Do not display raw sidecar provider secrets or auth-file secrets.
- Do not silently ignore upstream mutation failures; surface the backend detail and refresh current state.
