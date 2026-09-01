# FRONTEND MODEL DETAIL COMPATIBILITY CLUSTER

## OVERVIEW

`pages/model-detail/` keeps model-detail helpers still imported by `src/features/models/detail/`. The feature route owns page composition, mixed access-target editing, Terminal Target dialogs, and runtime-state rendering.

## UX-UPGRADE SURFACES

- `../models/AccessTargetsEditor.tsx` renders one mixed Model Target/Terminal Target list with global numbering; runtime order is shared across both target kinds.
- `ConnectionDialog.tsx` keeps OpenAI Terminal Target capability equal to the owner model mode and lets "从已有终端目标填充" prefill a draft that saves as an independent private Connection.
- The Terminal Target form owns the explicit `upstream_model_id` (上游模型 ID): create prefills the owner `model_id` for manual decoupling, edit hydrates the persisted identity, and clearing either form is rejected in place; an unchanged edit omits the PATCH field, while server 422 field errors render under the input.
- Dead `?tab=` state is normalized away at the router; `action=create-terminal-target` (+ `endpoint_id`) and `focus_connection_id` are one-shot parameters consumed exactly once.

## STRUCTURE

```text
model-detail/
├── ConnectionDialog.tsx
├── ConnectionCustomRequestParametersEditor.tsx
├── ConnectionRoutingScheduleField.tsx
├── CopyTerminalTargetDialog.tsx
├── ModelCostCards.tsx
├── RouteReadinessCard.tsx
├── CatalogBindDialog.tsx         # Management-only match preview, candidate search (shared picker), CAS bind payload
├── useCatalogCandidates.ts       # models.dev adapter over the shared append pager (debounce, family|all scope, revision mapping)
├── CatalogRefreshDialog.tsx      # Management-only refresh diff preview and full CAS commit
├── CatalogOverrideDialog.tsx     # Per-field/manual override workflow
├── catalogMetadataPresentation.ts # Stable catalog field order, labels, and effective-value projection
├── useModelCatalog.ts            # Settled-record catalog read hook (loading/failed/last-good/stale)

├── classifyOpenAICoverage.ts
├── customRequestParameters.ts
├── modelDetailMetricsAndPaths.ts
├── routingScheduleDraft.ts
├── useConnectionFocus.ts
├── useModelDetailBootstrap.ts
├── useModelDetailConnectionMutations.ts # Page coordinator for connection owners
├── useModelDetailConnectionSubmit.ts    # Create/update submit and server field-error mapping
├── useModelDetailConnectionLifecycle.ts # Quick connection mutations, delete, and refresh handoff
├── useModelDetailConnectionReconciliation.ts # Server connection DTO collection replacement
├── useModelDetailAccessTargetMutations.ts # Row-ID mutations for the mixed target list
├── useModelDetailTargetReconciliation.ts # Server mixed-target response reconciliation
├── connectionSubmitPreparation.ts
├── connectionCollectionState.ts
├── modelAccessTargetProjection.ts
├── connectionDataSupport.ts
├── upstreamModelIdField.ts # Shared 200-code-point/client/server field validation
├── useModelDetailDialogState.ts
├── useModelDetailModelForm.ts
├── useModelLoadbalanceCurrentState.ts
└── *.test.ts                            # Row-mutation, coverage-classification, and current-state coverage
```

## WHERE TO LOOK

- Feature route and page composition: `../../features/models/detail/`
- Bootstrap fetches, focus handoff, and redirect handling: `useModelDetailBootstrap.ts`, `useConnectionFocus.ts`
- Connection mutation coordinator: `useModelDetailConnectionMutations.ts`
- Connection submit parsing, field validation, and payload preparation: `connectionSubmitPreparation.ts`, `connectionDataSupport.ts`
- Connection create/update submit and server field-error mapping: `useModelDetailConnectionSubmit.ts`
- Connection DTO collection replacement and post-mutation refresh: `useModelDetailConnectionReconciliation.ts`, `useModelDetailConnectionLifecycle.ts`
- Mixed target row-ID mutations and server response reconciliation: `useModelDetailAccessTargetMutations.ts`, `useModelDetailTargetReconciliation.ts`
- Custom request parameters editor, client-side parser/validator mirroring the backend limits, and server 422 field mapping: `ConnectionCustomRequestParametersEditor.tsx`, `customRequestParameters.ts`, `useModelDetailConnectionSubmit.ts`
- Mixed access-target editor rendering and shared-order mutations: `../models/AccessTargetsEditor.tsx`
- Connection draft/update payload and limiter/header normalization: `connectionDataSupport.ts`; collection patch/resequence and pricing hydration: `connectionCollectionState.ts`; mounted pricing summaries include the typed `template_kind` and never fabricate a scalar base rate for multi-card templates.
- Default forms, access-target ownership/options/summary projection, and model list patching: `useModelDetailDialogState.ts`, `modelAccessTargetProjection.ts`, `../models/modelListProjection.ts`, `useModelDetailModelForm.ts`
- Spending-summary loading: `useModelDetailBootstrap.ts`, `ModelCostCards.tsx`
- Role metrics reuse the composite model-metrics read and keep `metrics_scope=ingress|final_execution` URL-backed; a failed metric read never blocks model or target configuration.
- Retained Ban Policy current-state fetch/reset hook: `useModelLoadbalanceCurrentState.ts`
- Shared latency and connection-label formatting: `modelDetailMetricsAndPaths.ts`

## CONVENTIONS

- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep model CRUD form state in `../models/modelFormState.ts`, mixed draft/order mutation rules in `../models/accessTargetFormState.ts`, and access-target ownership/options/summary projection in `modelAccessTargetProjection.ts`; access-target editor rendering belongs in `../models/AccessTargetsEditor.tsx`. Pricing selection options and mounted summaries display the localized kind label, while card/window authoring remains on the Pricing route.
- Catalog reads stay honest: `useModelCatalog` distinguishes first-read loading, read failure (with or without last-good data), and settled success while retaining same-model last-good data across a failed re-read. `@/features/models/detail/ModelsDevCatalogPanel` renders `OperatorLoadingState` for a first read, `OperatorErrorState`+retry for a failed first read (never a fabricated 未绑定), last-good metadata under exactly one `OperatorStalenessBadge` for a failed re-read, and the unbound conclusion only for a successful `bound:false`. Timestamps render through `useTimezone`; all mutations stay disabled while the read is unknown/stale. Bind payloads carry `expected_prism_model_id`/`expected_api_family`, refresh commits carry the full coordinate/token/revision CAS chain, sparse overrides carry the displayed coordinate, bulk override clears and unbind carry the displayed coordinate/token snapshot, and candidate paging is delegated to `CatalogCandidatePicker` over `useAppendCandidatePager` (this hook keeps only the models.dev debounce, scope policy, and revision mapping).
- The Terminal Target source-linked price action does **not** own a dialog here. It mounts the shared `@/features/pricing/catalog` `CatalogPricingDialog` with a `bound_model` source, preselecting and locking the current target. `CatalogPricingDialog` remains the only pricing dialog in this leaf. `@/features/models/detail/ModelsDevCatalogPanel.tsx` owns the federated panel shell and unbind confirmation, while this leaf owns `useModelCatalog`, the models.dev candidate adapter, and the bind/refresh/override dialogs.
- Keep connection collection patch/resequence separate from access-target projection. `useModelDetailConnectionMutations.ts` composes the explicit connection submit, lifecycle, reconciliation, and target-mutation owners.
- Parse raw connection drafts and map local/server field errors before connection API mutation; create/update/delete and collection reconciliation remain in the mutation owners.
- Catalog metadata is a management-only projection. `@/features/models/detail/ModelsDevCatalogPanel.tsx` owns the inset shell, effective rendering, action lock, and destructive unbind confirmation; bind, refresh-diff, and override API workflows live in their named dialogs and never affect runtime compatibility.
- Catalog override drafts are metadata-catalog driven across all backend fields and preserve missing, explicit null restore, and explicit values including empty strings.
- Keep the custom request parameters editor draft as raw text and validate with `customRequestParameters.ts`; never bypass client validation with the raw string, and map server 422 field envelopes back to the editor instead of toasts.
- Do not duplicate default form factories or redirect-target logic outside their named form/connection owners.
- Keep access-target mutations scoped to the source access-target row resolved from the model detail, while connection dialogs keep using the connection ID. Moves use the shared position order and must preserve the mixed runtime peer list.
- Model/target/connection/copy mutations refresh diagnostics and the authoritative model list; diagnostics GET owns a real AbortSignal. Copy candidates validate source text and image capability dimensions before submit.
- Do not manage routing priority from `ConnectionDialog.tsx`; ordering belongs to the mixed access-target list.
