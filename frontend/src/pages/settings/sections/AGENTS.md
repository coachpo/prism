# FRONTEND SETTINGS SECTIONS KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/` owns the rendered settings sections used by `../SettingsPage.tsx`. This folder covers the section-level UI for auth setup, shared vendor management, audit and privacy, billing and currency, backup, retention and deletion, and timezone preferences, plus the nested `authentication/` and `billing-currency/` UI clusters. Keep it focused on section rendering, not the shell or costing orchestration.

## STRUCTURE
```
sections/
├── AuthenticationSection.tsx
├── VendorManagementSection.tsx
├── AuditConfigurationSection.tsx
├── AuditConfigurationDefaultsCard.tsx
├── AuditConfigurationHeaderBlocklistCard.tsx
├── AuditConfigurationRuleActions.tsx
├── AuditConfigurationRuleSection.tsx
├── AuditConfigurationRulesPanel.tsx
├── AuditConfigurationRuleTable.tsx
├── AuditConfigurationVendorToggles.tsx
├── BillingCurrencySection.tsx
├── BackupSection.tsx
├── MonitoringSection.tsx
├── RetentionDeletionSection.tsx
├── TimezoneSection.tsx
├── authentication/                # Auth status, setup grid, passkey cards, browser ceremony hook, leaf AGENTS doc
└── billing-currency/              # Reporting currency card and FX mapping UI pieces
```

## WHERE TO LOOK

- Auth setup, verified-email prerequisites, and passkey UX: `AuthenticationSection.tsx`, `authentication/`
- Shared vendor-catalog table and Global-tab entrypoints for create/edit/delete flows: `VendorManagementSection.tsx`
- Audit and privacy defaults, header blocklist, vendor toggles, and rules-panel rendering stay vendor-based today, even though request logs and statistics now filter by `api_family`: `AuditConfigurationSection.tsx`, `AuditConfigurationDefaultsCard.tsx`, `AuditConfigurationHeaderBlocklistCard.tsx`, `AuditConfigurationVendorToggles.tsx`, `AuditConfigurationRulesPanel.tsx`, `AuditConfigurationRuleActions.tsx`, `AuditConfigurationRuleSection.tsx`, `AuditConfigurationRuleTable.tsx`
- Billing and currency section shell that renders reporting currency and FX mapping UI, while staying separate from costing state: `BillingCurrencySection.tsx`, `billing-currency/`
- Backup and config import or export section: `BackupSection.tsx`
- Monitoring cadence section rendering, with bootstrap/save orchestration still owned by parent settings hooks: `MonitoringSection.tsx`
- Retention and deletion section: `RetentionDeletionSection.tsx`
- Timezone preference section: `TimezoneSection.tsx`
- Shared page shell, section IDs, and save-state helpers: `../AGENTS.md`, `../settingsPageHelpers.ts`, `../sectionSaveState.tsx`
- Costing bootstrap, derived state, and save logic that feeds billing and timezone sections: `../costing/AGENTS.md`

## LOCAL CLUSTERS

- `authentication/`: auth status cards, setup field shells, passkey metadata, `usePasskeyManagement.ts`, and the nested `authentication/AGENTS.md` leaf
- `billing-currency/`: `ReportingCurrencyCard.tsx`, `FxMappingForm.tsx`, `FxMappingsSummary.tsx`, and `FxMappingsTable.tsx`

## CONVENTIONS

- Keep these files focused on section rendering, local field composition, and section-specific copy.
- Let `authentication/` own browser passkey ceremony and credential presentation details.
- Let `VendorManagementSection.tsx` stay rendering-focused; bootstrap, cache patching, and delete-conflict logic belong to the parent settings hooks and dialogs.
- Let `VendorManagementSection.tsx` stay rendering-focused and show vendor icon metadata from the shared catalog, with fallback monogram or placeholder rendering handled by the shared icon component layer.
- Let `billing-currency/` own the reporting-currency card and FX mapping presentation widgets.
- Let `MonitoringSection.tsx` stay rendering-focused; monitoring cadence bootstrap and save flows belong to the parent settings hooks.
- Pull bootstrap, dirty-state derivation, and save orchestration from the parent settings hooks instead of rebuilding that logic inside section components.
- Keep section IDs and save-state wiring aligned with the parent settings helpers.
- Let `BillingCurrencySection.tsx` stay a rendering boundary. The hooks that own costing changes live in `../costing/`.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not move auth setup or passkey browser logic out of `authentication/`.
- Do not move FX mapping CRUD state into `billing-currency/` presentation components. That boundary belongs to `../costing/`.
- Do not invent extra settings sections or nested AGENTS files beyond the local clusters already covered here. `authentication/AGENTS.md` is the one justified nested leaf; `billing-currency/` remains parent-covered.
