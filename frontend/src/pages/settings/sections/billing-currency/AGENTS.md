# FRONTEND SETTINGS BILLING-CURRENCY CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/billing-currency/` owns the rendering leaf for the visible billing and currency UI. It presents the reporting-currency card and the atomic currency-migration dialog while `../../costing/` owns bootstrap, validation, save orchestration, and reporting-currency priming. FX mapping authoring was hard-deleted; nothing here deals with FX.

## STRUCTURE
```text
billing-currency/
├── ReportingCurrencyCard.tsx   # Reporting currency code/symbol inputs and summary
├── CurrencyMigrationDialog.tsx # Two-phase migration preview -> commit dialog
└── ArchiveUnusedFxDialog.tsx   # Archive-only unused-FX preview -> commit dialog
```

## WHERE TO LOOK
- Section shell and parent handoff: `../BasisAndDisplaySection.tsx`, `../AGENTS.md`
- Stateful costing hooks and save flow: `../../costing/AGENTS.md`
- Shared normalization and display formatting helpers: `../../settingsPageHelpers.ts`
- Reporting-currency inputs and summary: `ReportingCurrencyCard.tsx`
- Currency-migration preview (per-template vN+1 impact table) and commit (sends `preview_hash`): `CurrencyMigrationDialog.tsx`
- Archive-only unused-FX preview and commit (keeps the active epoch and template prices unchanged): `ArchiveUnusedFxDialog.tsx`
- Currency-migration API calls and CAS `expected_updated_at` threading: `../../../../lib/api/observability.ts` (`settingsCosting.currencyMigrationPreview` / `currencyMigrationCommit`)
- Reporting-currency save and failed-save preservation belong to frontend seam tests rather than dedicated Playwright specs.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this leaf presentation-only. Let `../costing/` and `../useCostingSettingsData.ts` own currency-migration state and the post-commit refresh.
- Keep reporting-currency and migration copy shaped from the locale layer instead of hard-coding strings here.
- Keep `expected_updated_at` optional: omit it when the settings snapshot is unavailable instead of sending an empty string (the backend rejects non-RFC3339 values with 400).
- Do not load an unbounded active-template array for migration. The dialog must page the Pricing owner list or the immutable inventory scaffold, preserve null/pending prices, and show an explicit repair input step before previewing a same-currency repair.
- `archive_unused_fx` is intentionally separate from currency cutover: it is available only when the server inventory proves all FX evidence is unused and templates are ready, and it never authorizes FX authoring or changes the active reporting epoch.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not move currency-migration commit orchestration or post-commit refresh into this leaf.
- Do not add page-level fetches or bootstrap orchestration here.
- Do not duplicate the costing hook boundary or split the leaf into smaller AGENTS files.
- Do not reintroduce FX mapping forms, per-model/endpoint FX rate pickers, or mapping tables; the backend rejects `endpoint_fx_mappings` with 422.
