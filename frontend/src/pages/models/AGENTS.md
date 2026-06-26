# FRONTEND MODELS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/models/` keeps model dialogs, toolbar, form state, metric hydration, and shared-cache helpers still imported by the feature-owned `/models` route under `src/features/models/`. The feature route owns page orchestration and table rendering.

## STRUCTURE
```
models/
├── ModelDialog.tsx         # Create-edit dialog and backend errors
├── DeleteModelDialog.tsx   # Delete confirmation flow
├── modelFormState.ts       # Form defaults, payload transforms, and access-target helpers
├── ModelsToolbar.tsx       # Search-only toolbar
├── modelTableContracts.ts  # Shared metric type contract
├── useModelMetrics24h.ts   # 24h metrics and spend hydration
└── useModelsPageData.ts    # Shared-cache bootstrap, local patching, dialog orchestration
```

## WHERE TO LOOK

- Feature route and table rendering: `../../features/models/`, `../../features/models/ModelsTable.tsx`
- Shared model bootstrap and mutation patching: `useModelsPageData.ts`
- Unified access-target form behavior, strategy attachment rules, ordered target normalization, and payload transforms that carry fixed `api_family`: `modelFormState.ts`
- Backend validation messages and create/edit payload handoff: `ModelDialog.tsx`
- 24h metrics and spend overlays: `useModelMetrics24h.ts`
- Search toolbar: `ModelsToolbar.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Bootstrap models from `@/lib/referenceData`, then patch the local list with `setSharedModels()` after mutations.
- Keep access-target validation, strategy attachment rules, and `api_family` handling in `modelFormState.ts` instead of scattering them across dialog components.
- Keep ordered target add/remove/reorder state in `modelFormState.ts`; `ModelDialog.tsx` should stay a thin rendering layer over that logic plus backend validation display.
- Hydrate 24h metrics separately from the base model list so CRUD flows do not own observability queries.
- Keep the grouped models table keyed by `api_family` while still rendering the per-row `api_family` metadata.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild access-target strategy or form rules outside `modelFormState.ts`.
- Do not let table components own API calls; `useModelsPageData.ts` already centralizes list mutations.
- Do not fold metrics queries into the base list bootstrap when `useModelMetrics24h.ts` already isolates that concern.
