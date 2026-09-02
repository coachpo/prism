# FRONTEND MODELS COMPATIBILITY CLUSTER

## OVERVIEW

`pages/models/` keeps model dialogs, toolbar, form state, metric hydration, and shared-cache helpers still imported by the feature-owned `/models` route under `src/features/models/`. The feature route owns page orchestration and table rendering. The management surface defaults to 入口模型 (direct entry model) and exposes URL-backed 入口模型 / 仅模型目标 / 全部模型配置 views; 模型目标 (Model Target), 最终目标模型 (final target model), and 上游模型 ID (upstream model ID) keep their own distinct terms and are never rewritten to 入口模型.

## STRUCTURE

```
models/
├── ModelDialog.tsx         # Edit dialog and backend errors
├── CreateModelDialog.tsx   # Create flow
├── InitialTerminalTargetFields.tsx # First Terminal Target fields in composite create
├── useInitialTerminalTargetUpstreamModelId.ts # Entry-ID follow-until-edited field lifecycle
├── DeleteModelDialog.tsx   # Delete confirmation flow
├── AccessTargetsEditor.tsx # Mixed Model Target/Terminal Target list editor with row-ID mutations
├── modelFormState.ts       # Model CRUD form defaults, normalization, validation, and payloads
├── accessTargetFormState.ts # Mixed access-target draft, order, and mutation contract
├── modelListProjection.ts  # Server model DTO to list-row projection
├── modelTableContracts.ts  # Shared metric type contract
├── useModelMetrics24h.ts   # 24h metrics and spend hydration
├── useModelsPageData.ts    # Thin page composition over model, dialog, enablement, delete, strategy, and metrics owners
├── useModelsCollection.ts  # Model/strategy bootstrap, shared-cache reads, and model list patching
├── useModelDialogMutations.ts # Create/edit form state, dialog sessions, CRUD, and server error mapping
├── useModelEnablementMutations.ts # Row and bulk model enablement mutations
├── useModelDeletion.ts      # Model deletion target and mutation lifecycle
├── useLoadbalanceStrategyDefaults.ts # Strategy-default creation, re-read, and dialog reconciliation
└── *.test.ts(x)            # Access-target editor and metrics-hydration coverage
```

## WHERE TO LOOK

- Feature route and table rendering: `../../features/models/`, `../../features/models/ModelsTable.tsx`
- Model collection/bootstrap, revision cache, and server DTO patching: `useModelsCollection.ts`
- Model create/edit dialog sessions, form mutations, and server error mapping: `useModelDialogMutations.ts`
- Row/bulk enablement state and mutations: `useModelEnablementMutations.ts`
- Model deletion target and mutation: `useModelDeletion.ts`
- Load-balance strategy default creation and forced re-read: `useLoadbalanceStrategyDefaults.ts`
- Thin page-facing composition and metrics handoff: `useModelsPageData.ts`
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
- Preserve backend `routing_summary` and authoritative recursive connection counts in list projection. Routing-relevant detail mutations refresh the shared model collection instead of recalculating direct connections in the browser.
- Keep model CRUD form validation, strategy attachment, `api_family`, and independent OpenAI text/image dimensions in `modelFormState.ts`; keep mixed access-target draft/order rules in `accessTargetFormState.ts`.
- Model create payloads are family-discriminated: Anthropic/Gemini omit every OpenAI-only key rather than sending explicit nulls.
- `AccessTargetsEditor.tsx` consumes persisted access targets through mutation-shaped rows and renders one mixed Model Target/Terminal Target list with global "位置 N" numbering. Moves use the shared runtime order; row mutations address the persisted access-target row ID (never a position in `access_targets`, which the drag draft can reorder) and connection actions use the connection ID.
- The models-list exit-mapping cell (`features/models/ModelExitMappingCell.tsx` + `modelExitMapping.ts`) projects the DIRECT `access_targets` rows in shared `(position, id)` order and shows only the first two: Terminal Target rows as endpoint → persisted upstream identity, Model Target rows as the logical target id. The remainder is a detail-pointer, not a summary; the projection never follows Model Target rows recursively and never backfills missing endpoint/upstream evidence from the entry `model_id` (missing values render a reasoned `—`). Identity flags (`modelRoutingFlags.ts`): `upstream_decoupled` compares upstream identity against the entry `model_id` exactly (case-sensitive; missing evidence is unknown, never decoupled), and `has_model_target` detects direct Model Target rows.
- Hydrate 24h metrics separately from the base model list so CRUD flows do not own observability queries.
- Hydrate all three named metric blocks (`ingress`, `final_execution`, `route_attempt`) in one batch read. The table switch is a controlled single-select segmented control (`ModelsMetricsScopeSwitch`, never tabs) that is URL-backed through `scope` and selects a local block; it must not issue one request per tab, and re-selecting the active scope never clears the selection. Route-attempt cost stays absent with a reason.
- Keep the grouped models table keyed by `api_family` while still rendering the per-row `api_family` metadata.
- Keep `direct_request_enabled` as the form's single entry switch. The list view filters by that bit (non-entry rows remain available in the Model Target and all views), displays the server incoming-reference count/warning, and writes the selected view to canonical URL search state.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX

- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild model CRUD or mixed access-target rules in dialog components; import them from their named owner.
- Do not let table components own API calls; the named model lifecycle owners centralize list and mutation calls while `useModelsPageData.ts` only composes them.
- Keep model bootstrap/cache patching, dialog/form CRUD, row/bulk enablement, deletion, and strategy-default creation in their named owners. Dialog session fencing must remain authoritative when strategy defaults are re-read or a modal closes.
- Do not fold metrics queries into the base list bootstrap when `useModelMetrics24h.ts` already isolates that concern.
