# FRONTEND MODELS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/models/` owns the models list search and row-card presentation, dialog state, vendor-grouped table rendering with row-level `api_family` still visible, vendor and `api_family` form logic, ordered access-target editing for same-family model or standalone connection targets, explicit Ban Policy strategy attachment during create or edit, and separate 24-hour metric hydration behind `../ModelsPage.tsx`.

## STRUCTURE
```
models/
├── ModelDialog.tsx         # Create-edit dialog
├── DeleteModelDialog.tsx   # Delete confirmation flow
├── modelFormState.ts       # Form defaults, payload transforms, unified access-target helpers
├── ModelsTable.tsx         # Search-filtered horizontal row list + row actions
├── ModelsToolbar.tsx       # Search-only toolbar
├── modelTableContracts.ts  # Shared metric type contract
├── useModelMetrics24h.ts   # 24h metrics and spend hydration
└── useModelsPageData.ts    # Shared-cache bootstrap, local patching, dialog orchestration
```

## WHERE TO LOOK

- Shared model/vendor bootstrap and mutation patching: `useModelsPageData.ts`
- Unified access-target form behavior, strategy attachment rules, ordered target normalization, vendor selection, and payload transforms that carry `vendor_id` plus fixed `api_family` while preserving vendor metadata such as `icon_key`: `modelFormState.ts`
- 24h metrics and spend overlays: `useModelMetrics24h.ts`
- Search toolbar: `ModelsToolbar.tsx`
- Row rendering, navigation, and row actions: `ModelsTable.tsx`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Bootstrap models and vendors from `@/lib/referenceData`, then patch the local list with `setSharedModels()` after mutations.
- Keep access-target validation, strategy attachment rules, vendor selection, and `api_family` handling in `modelFormState.ts` instead of scattering them across dialog components.
- Keep ordered target add/remove/reorder state in `modelFormState.ts`; `ModelDialog.tsx` should stay a thin rendering layer over that logic.
- Hydrate 24h metrics separately from the base model list so CRUD flows do not own observability queries.
- Keep the grouped models table keyed by vendor, not api family, while still rendering the per-row `api_family` metadata.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not rebuild access-target strategy or form rules outside `modelFormState.ts`.
- Do not let table components own API calls; `useModelsPageData.ts` already centralizes list mutations.
- Do not fold metrics queries into the base list bootstrap when `useModelMetrics24h.ts` already isolates that concern.

