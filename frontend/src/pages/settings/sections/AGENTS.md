# FRONTEND SETTINGS SECTIONS KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/` owns the rendered settings sections used by `../../SettingsPage.tsx`. This folder covers the section-level UI for auth setup, shared vendor management, audit and privacy, header-blocklist rules, user-agent or client rules, billing and currency, backup, retention and deletion, and timezone preferences, plus the nested `authentication/` and `billing-currency/` leaf clusters. Keep it focused on section rendering, not the shell or costing orchestration.

## STRUCTURE
```text
sections/
├── AuthenticationSection.tsx
├── VendorManagementSection.tsx
├── VendorCatalogTransportCard.tsx
├── AuditConfigurationSection.tsx
├── AuditConfigurationDefaultsCard.tsx
├── AuditConfigurationHeaderBlocklistCard.tsx
├── AuditConfigurationUserAgentClientRulesCard.tsx
├── AuditConfigurationRuleActions.tsx
├── AuditConfigurationRuleSection.tsx
├── AuditConfigurationRulesPanel.tsx
├── AuditConfigurationRuleTable.tsx
├── AuditConfigurationVendorToggles.tsx
├── BillingCurrencySection.tsx
├── BackupSection.tsx
├── RetentionDeletionSection.tsx
├── TimezoneSection.tsx
├── authentication/                # Auth status, setup grid, and recovery-email auth UI cluster
└── billing-currency/AGENTS.md     # Reporting currency and FX mapping leaf cluster
```

## WHERE TO LOOK

- Auth setup and verified-email prerequisites: `AuthenticationSection.tsx`, `authentication/`
- Shared vendor-catalog table, catalog import/export preview transport, and Global-tab entrypoints for create/edit/delete flows: `VendorManagementSection.tsx`, `VendorCatalogTransportCard.tsx`
- Audit and privacy defaults, header blocklist, user-agent or client rule management, vendor toggles, and rules-panel rendering stay vendor-based today, even though request logs and statistics now filter by `api_family`: `AuditConfigurationSection.tsx`, `AuditConfigurationDefaultsCard.tsx`, `AuditConfigurationHeaderBlocklistCard.tsx`, `AuditConfigurationUserAgentClientRulesCard.tsx`, `AuditConfigurationVendorToggles.tsx`, `AuditConfigurationRulesPanel.tsx`, `AuditConfigurationRuleActions.tsx`, `AuditConfigurationRuleSection.tsx`, `AuditConfigurationRuleTable.tsx`
- Billing and currency section shell that renders reporting currency and FX mapping UI, while staying separate from costing state: `BillingCurrencySection.tsx`, `billing-currency/AGENTS.md`
- Backup and config import or export section: `BackupSection.tsx`
- Retention and deletion section: `RetentionDeletionSection.tsx`
- Timezone preference section: `TimezoneSection.tsx`
- Shared page shell, section IDs, and save-state helpers: `../AGENTS.md`, `../settingsPageHelpers.ts`, `../sectionSaveState.tsx`
- Costing bootstrap, derived state, and save logic that feeds billing and timezone sections: `../costing/AGENTS.md`
- E2E seams for vendor catalog transport, reporting-currency save, retention, and user-agent/client rule copy: `../../../../tests/e2e/settings-vendor-catalog.spec.ts`, `../../../../tests/e2e/settings-reporting-currency-save.spec.ts`, `../../../../tests/e2e/settings-log-retention.spec.ts`, `../../../../tests/e2e/settings-user-agent-client-rules-copy.spec.ts`

## LOCAL CLUSTERS

- `authentication/`: auth status cards, setup field shells, and the nested `authentication/AGENTS.md` leaf
- `billing-currency/`: `ReportingCurrencyCard.tsx`, `FxMappingForm.tsx`, `FxMappingsSummary.tsx`, and `FxMappingsTable.tsx`; its leaf doc lives at `billing-currency/AGENTS.md`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep these files focused on section rendering, local field composition, and section-specific copy.
- Let `VendorManagementSection.tsx` and `VendorCatalogTransportCard.tsx` stay rendering-focused; bootstrap, catalog import/export parsing, preview state, cache patching, and delete-conflict logic belong to the parent settings hooks and dialogs.
- Let vendor section UI show vendor icon metadata from the shared catalog, with fallback monogram or placeholder rendering handled by the shared icon component layer.
- Let `billing-currency/` own the reporting-currency card and FX mapping presentation widgets.
- Pull bootstrap, dirty-state derivation, and save orchestration from the parent settings hooks instead of rebuilding that logic inside section components.
- Keep section IDs and save-state wiring aligned with the parent settings helpers.
- Let `BillingCurrencySection.tsx` stay a rendering boundary. The hooks that own costing changes live in `../costing/`.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move auth setup logic out of `authentication/`.
- Do not move FX mapping CRUD state into `billing-currency/AGENTS.md` presentation components. That boundary belongs to `../costing/`.
- Do not invent extra settings sections or nested AGENTS files beyond the local clusters already covered here. `authentication/AGENTS.md` and `billing-currency/AGENTS.md` are the justified nested leaves.
