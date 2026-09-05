# Currency display and migration dialogs

- `ReportingCurrencyCard.tsx` renders the current currency/symbol draft. `CurrencyMigrationDialog.tsx` owns cutover/repair input steps and localized feedback; `ArchiveUnusedFxDialog.tsx` owns the separate archive flow.
- Delegate inventory/draft/preview/commit requests to `../../costing/currencyMigrationProtocol.ts` and post-commit refresh to `../../useCostingSettingsData.ts`. Keep server snapshot/CAS evidence intact; do not invent timestamps or send empty-string CAS values.
- Migration displays complete role-keyed cards through `currencyMigrationCards.ts`, preserving explicit zero versus missing/pending prices. The protocol pages bounded inventory/templates; do not load an unbounded template array for the dialog.
- Same-currency repair has an explicit repair-input step before preview. Archive is offered only when server inventory declares unused-FX cleanup available, and must not imply a currency cutover or FX authoring.
