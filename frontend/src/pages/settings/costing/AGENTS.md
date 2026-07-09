# FRONTEND SETTINGS COSTING KNOWLEDGE BASE

## OVERVIEW
`pages/settings/costing/` owns the settings-side costing hooks that support billing, reporting currency, FX mapping, and timezone behavior. This folder handles bootstrap, normalized derived state, FX mapping CRUD state, save flows, and reporting-currency priming, while `../sections/BillingCurrencySection.tsx` and `../sections/billing-currency/AGENTS.md` stay focused on rendering.

## STRUCTURE
```text
costing/
├── useCostingSettingsBootstrap.ts   # Load costing settings and shared model options
├── useCostingDerivedState.ts        # Dirty flags, preview text, labels, and mapping options
├── useCostingMappingCrud.ts         # FX mapping create, edit, delete, and connection loading
└── useCostingSettingsSave.ts        # Billing save and timezone save flows
```

## WHERE TO LOOK

- Bootstrap fetches for costing settings and shared models: `useCostingSettingsBootstrap.ts`
- Normalization, dirty-state derivation, timezone preview, model labels, and endpoint options: `useCostingDerivedState.ts`
- FX mapping CRUD, selected-model connection loading, and inline validation: `useCostingMappingCrud.ts`
- Billing save, reporting-currency refresh/prime, and timezone save boundaries: `useCostingSettingsSave.ts`
- Shared defaults, normalization helpers, mapping validation, and formatting helpers: `../settingsPageHelpers.ts`
- Billing and currency rendering layer: `../sections/BillingCurrencySection.tsx`, `../sections/billing-currency/AGENTS.md`
- Reporting-currency save success, failure preservation, and provider priming belong to frontend seam tests rather than dedicated Playwright specs.

## BOUNDARY

- `costing/` owns stateful hooks, normalization, validation, and save orchestration.
- `sections/BillingCurrencySection.tsx` owns the section shell that wires those hooks into the settings page.
- `sections/billing-currency/AGENTS.md` owns presentation widgets such as the reporting currency card, FX mapping form, summary, and table.
- Timezone saving stays in this hook cluster because it shares the costing-form saved-state model, even though the rendered section lives in `../sections/TimezoneSection.tsx`.
- Reporting-currency cache/trust itself is owned by `../../../context/ReportingCurrencyContext.tsx` and `../../../lib/reportingCurrency.ts`; this folder writes settings and refreshes or primes that shared state.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep costing data normalized through `normalizeCostingForm()` before dirty checks or saves.
- Preserve the split between billing saves and timezone saves. Timezone save depends on a valid saved billing state.
- Load FX mapping endpoint choices from the selected model's connections inside the CRUD hook, not inside presentation components.
- Reuse `settingsPageHelpers.ts` for mapping keys, validation, formatting, and default state.
- After reporting-currency writes, use the provider refresh/prime seam instead of creating a local currency cache.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move FX mapping CRUD state into `sections/billing-currency/AGENTS.md` presentation components.
- Do not collapse billing and timezone saves into one generic action when the hook boundary keeps their validation rules clear.
- Do not duplicate normalization or mapping validation logic outside this hook cluster and `settingsPageHelpers.ts`.
- Do not duplicate reporting-currency trust or fallback behavior here; the provider/lib seam owns it.
