# FRONTEND MODEL DETAIL COMPATIBILITY CLUSTER

## OVERVIEW
`pages/model-detail/` keeps model-detail helpers still imported by `src/features/models/detail/`. The feature route owns page composition, ordered access-target editing, Terminal Target dialogs, and summary rendering.

## STRUCTURE
```text
model-detail/
├── ConnectionDialog.tsx
├── ConnectionCustomRequestParametersEditor.tsx
├── customRequestParameters.ts
├── ModelDetailHeader.tsx
├── modelDetailMetricsAndPaths.ts
├── OverviewCards.tsx
├── useConnectionFocus.ts
├── useModelDetailBootstrap.ts
├── useModelDetailConnectionMutations.ts
├── useModelDetailDataSupport.ts
├── useModelDetailDialogState.ts
├── useModelDetailModelForm.ts
└── useModelLoadbalanceCurrentState.ts
```

## WHERE TO LOOK
- Feature route and page composition: `../../features/models/detail/`
- Bootstrap fetches, focus handoff, and redirect handling: `useModelDetailBootstrap.ts`, `useConnectionFocus.ts`
- Connection create, edit, and delete flows with target-row-ID mutations: `useModelDetailConnectionMutations.ts`, `useModelDetailDialogState.ts`
- Custom request parameters editor, client-side parser/validator mirroring the backend limits, and server 422 field mapping: `ConnectionCustomRequestParametersEditor.tsx`, `customRequestParameters.ts`, `useModelDetailConnectionMutations.ts`
- Mixed access-target editor rendering and row-ID mutations: `../models/AccessTargetsEditor.tsx`
- Default forms, access-target summary (single enabled authored-order first row), and model list patching helpers: `useModelDetailDataSupport.ts`, `useModelDetailModelForm.ts`
- Spending-summary loading: `useModelDetailBootstrap.ts`, `OverviewCards.tsx`
- Retained Ban Policy current-state fetch/reset hook: `useModelLoadbalanceCurrentState.ts`
- Shared latency and connection-label formatting: `modelDetailMetricsAndPaths.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep access-target option building and update payload shaping in `useModelDetailDataSupport.ts` / `useModelDetailModelForm.ts`; access-target editor rendering belongs in `../models/AccessTargetsEditor.tsx`.
- Keep the custom request parameters editor draft as raw text and validate with `customRequestParameters.ts`; never bypass client validation with the raw string, and map server 422 field envelopes back to the editor instead of toasts.
- Do not duplicate default form factories or redirect-target logic outside `useModelDetailDataSupport.ts`.
- Keep access-target mutations row-ID based: move/toggle/delete use the persistent target row ID, while connection dialogs keep using the connection ID. Terminal-only optimistic reorder flows are removed; reorder goes through the mixed list position route.
- Do not manage routing priority from `ConnectionDialog.tsx`; ordering belongs to the mixed access-target list.
