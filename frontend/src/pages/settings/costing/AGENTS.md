# FRONTEND SETTINGS COSTING KNOWLEDGE BASE

## OVERVIEW
`pages/settings/costing/` owns the settings-side costing hooks that support billing, currency, FX mapping, and timezone behavior. This folder handles bootstrap, normalized derived state, FX mapping CRUD state, and save flows, while `../sections/BillingCurrencySection.tsx` and `../sections/billing-currency/` stay focused on rendering.

## STRUCTURE
```
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
- Billing save and timezone save boundaries: `useCostingSettingsSave.ts`
- Shared defaults, normalization helpers, mapping validation, and formatting helpers: `../settingsPageHelpers.ts`
- Billing and currency rendering layer: `../sections/BillingCurrencySection.tsx`, `../sections/billing-currency/`

## BOUNDARY

- `costing/` owns stateful hooks, normalization, validation, and save orchestration.
- `sections/BillingCurrencySection.tsx` owns the section shell that wires those hooks into the settings page.
- `sections/billing-currency/` owns presentation widgets such as the reporting currency card, FX mapping form, summary, and table.
- Timezone saving stays in this hook cluster because it shares the costing-form saved-state model, even though the rendered section lives in `../sections/TimezoneSection.tsx`.

## CONVENTIONS

- Keep costing data normalized through `normalizeCostingForm()` before dirty checks or saves.
- Preserve the split between billing saves and timezone saves. Timezone save depends on a valid saved billing state.
- Load FX mapping endpoint choices from the selected model's connections inside the CRUD hook, not inside presentation components.
- Reuse `settingsPageHelpers.ts` for mapping keys, validation, formatting, and default state.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not move FX mapping CRUD state into `sections/billing-currency/` UI components.
- Do not collapse billing and timezone saves into one generic action when the hook boundary keeps their validation rules clear.
- Do not duplicate normalization or mapping validation logic outside this hook cluster and `settingsPageHelpers.ts`.
