# FRONTEND SETTINGS COSTING KNOWLEDGE BASE

## OVERVIEW
`pages/settings/costing/` owns the settings-side costing hooks that support billing, reporting currency, the atomic currency-migration flow, and timezone behavior. This folder handles bootstrap, normalized derived state, save flows, and reporting-currency priming, while `../sections/BasisAndDisplaySection.tsx` and `../sections/billing-currency/AGENTS.md` stay focused on rendering. FX mapping authoring was hard-deleted; no hook or component here deals with FX.

## STRUCTURE
```text
costing/
├── useCostingSettingsBootstrap.ts   # Load costing settings and shared model options
├── useCostingDerivedState.ts        # Dirty flags, preview text, and labels
└── useCostingSettingsSave.ts        # Billing save and timezone save flows
```

## WHERE TO LOOK

- Bootstrap fetches for costing settings and shared models: `useCostingSettingsBootstrap.ts`
- Normalization, dirty-state derivation, and timezone preview: `useCostingDerivedState.ts`
- Billing save, reporting-currency refresh/prime, and timezone save boundaries: `useCostingSettingsSave.ts`
- Currency-migration committed refresh (re-fetch settings, prime provider, bump revision): `../useCostingSettingsData.ts`
- Shared defaults, normalization helpers, and formatting helpers: `../settingsPageHelpers.ts`
- Reporting-currency and timezone rendering layer: `../sections/BasisAndDisplaySection.tsx`, `../sections/billing-currency/AGENTS.md`
- Reporting-currency save success, failure preservation, and provider priming belong to frontend seam tests rather than dedicated Playwright specs.

## BOUNDARY

- `costing/` owns stateful hooks, normalization, validation, and save orchestration.
- `sections/BasisAndDisplaySection.tsx` owns the section shell that wires those hooks into the settings page.
- `../sections/billing-currency/AGENTS.md` owns presentation widgets such as the reporting currency card and the currency-migration dialog.
- Timezone saving stays in this hook cluster because it shares the costing-form saved-state model, even though it is rendered by `../sections/BasisAndDisplaySection.tsx` alongside reporting currency.
- Reporting-currency cache/trust itself is owned by `../../../context/ReportingCurrencyContext.tsx` and `../../../lib/reportingCurrency.ts`; this folder writes settings and refreshes or primes that shared state.
- Currency migrations (preview + commit) are owned by the backend `settings` package; the frontend only renders the preview impact table and sends `preview_hash` on commit.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep costing data normalized through `normalizeCostingForm()` before dirty checks or saves.
- Preserve the split between billing saves and timezone saves. Timezone save depends on a valid saved billing state.
- Reuse `settingsPageHelpers.ts` for defaults and normalization helpers.
- After reporting-currency writes, use the provider refresh/prime seam instead of creating a local currency cache.
- Do not reintroduce FX mapping state, validation, or endpoint-connection loading: the backend rejects `endpoint_fx_mappings` with 422 and the currency-migration flow is the only way to change the reporting currency code.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move currency-migration dialog state into `../sections/billing-currency/AGENTS.md` presentation components beyond open/commit callbacks.
- Do not collapse billing and timezone saves into one generic action when the hook boundary keeps their validation rules clear.
- Do not duplicate normalization or mapping validation logic outside this hook cluster and `settingsPageHelpers.ts`; there is no mapping validation anymore.
- Do not duplicate reporting-currency trust or fallback behavior here; the provider/lib seam owns it.
- Do not reintroduce FX authoring: no `endpoint_fx_mappings` fields, no per-model/endpoint FX rate forms, no FX CRUD hooks.
