# FRONTEND MODELS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/models/` keeps model dialogs, toolbar, form state, metric hydration, and shared-cache helpers still imported by the feature-owned `/models` route under `src/features/models/`. The feature route owns page orchestration and table rendering.

## STRUCTURE
```
models/
├── ModelDialog.tsx         # Edit dialog and backend errors
├── CreateModelDialog.tsx   # Create flow
├── DeleteModelDialog.tsx   # Delete confirmation flow
├── AccessTargetsEditor.tsx # Mixed Model Target/Terminal Target list editor with row-ID mutations
├── modelFormState.ts       # Model CRUD form defaults, normalization, validation, and payloads
├── accessTargetFormState.ts # Mixed access-target draft, order, and mutation contract
├── modelListProjection.ts  # Server model DTO to list-row projection
├── modelTableContracts.ts  # Shared metric type contract
├── useModelMetrics24h.ts   # 24h metrics and spend hydration
├── useModelsPageData.ts    # Shared-cache bootstrap, local patching, dialog orchestration
└── *.test.ts(x)            # Access-target editor and metrics-hydration coverage
```

## WHERE TO LOOK

- Feature route and table rendering: `../../features/models/`, `../../features/models/ModelsTable.tsx`
- Shared model bootstrap and mutation patching: `useModelsPageData.ts`
- Model CRUD form defaults, OpenAI text/image normalization, validation, and payload transforms: `modelFormState.ts`
- Mixed access-target draft/order/mutation contract, including `(position, id)` ordering helpers: `accessTargetFormState.ts`
- Model-list DTO projection after CRUD/detail responses: `modelListProjection.ts`
- Mixed access-target list editor with row-ID mutations: `AccessTargetsEditor.tsx`
- Backend validation messages and create/edit payload handoff: `ModelDialog.tsx`
- 24h metrics and spend overlays: `useModelMetrics24h.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Bootstrap models from `@/lib/referenceData`, then patch the local list with `setSharedModels()` after mutations.
- Keep model CRUD form validation, strategy attachment, `api_family`, and independent OpenAI text/image dimensions in `modelFormState.ts`; keep mixed access-target draft/order rules in `accessTargetFormState.ts`.
- `AccessTargetsEditor.tsx` consumes persisted access targets through mutation-shaped rows and renders one mixed Model Target/Terminal Target list with global "位置 N" numbering. Moves use the shared runtime order; row mutations address the persisted access-target row ID (never a position in `access_targets`, which the drag draft can reorder) and connection actions use the connection ID.
- Hydrate 24h metrics separately from the base model list so CRUD flows do not own observability queries.
- Keep the grouped models table keyed by `api_family` while still rendering the per-row `api_family` metadata.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild model CRUD or mixed access-target rules in dialog components; import them from their named owner.
- Do not let table components own API calls; `useModelsPageData.ts` already centralizes list mutations.
- Do not fold metrics queries into the base list bootstrap when `useModelMetrics24h.ts` already isolates that concern.
