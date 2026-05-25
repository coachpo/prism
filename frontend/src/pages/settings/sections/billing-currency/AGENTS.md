# FRONTEND SETTINGS BILLING-CURRENCY CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/billing-currency/` owns the rendering leaf for the Profile-tab billing and currency UI. It presents reporting-currency and FX-mapping widgets while `../costing/` owns bootstrap, validation, CRUD state, save orchestration, and reporting-currency priming.

## STRUCTURE
```text
billing-currency/
├── ReportingCurrencyCard.tsx   # Reporting currency inputs and summary
├── FxMappingForm.tsx           # Add-mapping form and model -> endpoint picker
├── FxMappingsSummary.tsx       # Section heading and default-mapping summary
└── FxMappingsTable.tsx         # Existing mapping table with edit and delete actions
```

## WHERE TO LOOK
- Section shell and parent handoff: `../BillingCurrencySection.tsx`, `../AGENTS.md`
- Stateful costing hooks and save flow: `../../costing/AGENTS.md`
- Shared mapping helpers and display formatting: `../../settingsPageHelpers.ts`
- Reporting-currency inputs and summary: `ReportingCurrencyCard.tsx`
- Add-mapping form and endpoint selection: `FxMappingForm.tsx`
- Mapping table, inline edit state, and delete actions: `FxMappingsTable.tsx`
- Default-mapping summary copy: `FxMappingsSummary.tsx`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this leaf presentation-only. Let `../costing/` own model loading, FX mapping CRUD state, validation, and save orchestration.
- Keep reporting-currency copy and FX mapping copy shaped from the locale layer instead of hard-coding strings here.
- Keep model and endpoint selector data supplied by the parent hook layer.
- Keep the card, form, summary, and table components focused on rendering and local field wiring.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS
- Do not move FX mapping CRUD state or save logic into this leaf.
- Do not add page-level fetches or bootstrap orchestration here.
- Do not duplicate the costing hook boundary or split the leaf into smaller AGENTS files.
