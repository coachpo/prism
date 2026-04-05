# FRONTEND SETTINGS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/settings/` is the route-domain shell behind `../SettingsPage.tsx`. It owns the Profile and Global tab split, stable section and tab helpers, hash-driven section focus, shared save-state rendering, and the dialog handoff that supports settings mutations. Keep shell behavior here, section rendering in `sections/AGENTS.md`, and costing state in `costing/AGENTS.md`.

## STRUCTURE
```
settings/
├── sections/                      # Rendered settings sections and nested local clusters
├── costing/                       # Costing bootstrap, derived state, FX mapping CRUD, save flows
├── dialogs/                       # Delete confirmation and audit-rule dialogs
├── SettingsSectionsNav.tsx         # Sticky section navigation for profile-tab sections
├── SettingsProfileTab.tsx          # Profile-tab body and section layout
├── SettingsGlobalTab.tsx           # Global-tab body for auth + shared vendor management
├── useSettingsPageData.ts          # Top-level page composition across backup, auth, costing, audit, retention
├── useSettingsPageSectionState.ts  # Active tab, hash, scroll focus, and section jumps
├── useSettingsSectionNavigation.ts   # Scroll target registration and section jump helpers
├── useAuthenticationSettingsData.ts
├── useCostingSettingsData.ts
├── useAuditConfigurationData.ts
├── useConfigBackupData.ts
├── useMonitoringSettingsData.ts   # Profile-scoped monitoring cadence bootstrap and save flow
├── useRetentionDeletionData.ts
├── useVendorManagementData.ts      # Shared-vendor CRUD bootstrap and delete-safety flow
├── vendorManagementFormState.ts    # Vendor form normalization and delete-conflict parsing
├── sectionSaveState.tsx           # Shared dirty, saving, and recently-saved rendering
├── settingsPageHelpers.ts         # Tab ids, section ids, default costing form, shared validation helpers
└── settingsSaveTypes.ts
```

## SHELL CONTRACT

- `SettingsPage.tsx` renders two tabs: `Profile` and `Global`.
- The Profile tab owns section navigation and mounts backup, billing and currency, timezone, monitoring cadence, audit and privacy, and retention and deletion.
- The Global tab mounts instance-wide authentication plus the shared vendor-management section and its dialogs. Vendor rows carry the persisted optional `icon_key`, while model rows do not.
- `settingsPageHelpers.ts` is the source of truth for tab ids, profile section ids, instance-only section handling, delete keywords, and shared costing and auth validation helpers.

## WHERE TO LOOK

- Thin route shell, tab split, section order, and dialog mounts: `../SettingsPage.tsx`
- Cross-section composition, selected-profile labeling, and shared save-state handoff: `useSettingsPageData.ts`
- Active tab state, hash updates, scroll-driven focus, and section jump behavior: `useSettingsPageSectionState.ts`, `useSettingsSectionNavigation.ts`, `SettingsSectionsNav.tsx`
- Stable helper constants and form-normalization utilities: `settingsPageHelpers.ts`
- Shared save-state badges and render helpers: `sectionSaveState.tsx`, `settingsSaveTypes.ts`
- Section implementation boundary: `sections/AGENTS.md`
- Monitoring cadence bootstrap and save flow: `useMonitoringSettingsData.ts`, `sections/MonitoringSection.tsx`
- Costing bootstrap, derived state, FX mapping CRUD, and save boundary: `costing/AGENTS.md`
- Global vendor CRUD, usage prefetch, shared-cache patching, and delete-conflict parsing: `useVendorManagementData.ts`, `vendorManagementFormState.ts`
- Local dialogs for destructive actions and audit rule editing: `dialogs/`, `useAuditConfigurationData.ts`, `useRetentionDeletionData.ts`, `useConfigBackupData.ts`, `dialogs/VendorDialog.tsx`, `dialogs/DeleteVendorDialog.tsx`

## CHILD DOCS

- `sections/AGENTS.md`: authentication-adjacent section UI, audit and privacy, billing and currency, backup, retention, timezone, and the nested `authentication/` and `billing-currency/` clusters.
- `sections/authentication/AGENTS.md`: operator account, recovery email verification, passkey ceremony, and passkey presentation metadata.
- `costing/AGENTS.md`: costing bootstrap, dirty-state derivation, FX mapping CRUD, save flows, and the split between costing hooks and billing-currency section UI.

## CONVENTIONS

- Keep new settings work sectionized. Extend helper registries, shared hooks, or local dialogs instead of inflating `SettingsPage.tsx`.
- Hash navigation is part of the settings UX contract. New profile-tab sections need stable ids and must participate in jump and active-section logic.
- Save-state feedback belongs in `sectionSaveState.tsx` and related helper types, not in ad hoc spinners or toast-only status.
- Keep the scope split clear in copy and behavior: authentication is global, while backup, billing and currency, timezone, monitoring cadence, audit and privacy, and retention stay profile-scoped.
- Keep monitoring cadence backend-owned. The profile-tab section edits `/api/settings/monitoring`; browser polling never runs upstream probes directly.
- Keep shared vendor catalog CRUD on the Global tab and continue to let the Profile-tab audit defaults consume that same shared vendor catalog.
- Keep vendor icon metadata on the shared vendor catalog and preserve it through global CRUD flows.
- `SettingsProfileTab.tsx` and `SettingsGlobalTab.tsx` own the tab bodies, while the shell hook keeps their section state synchronized.
- Billing, reporting currency, timezone preference, and FX mappings cross the `sections/` and `costing/` boundary. Let this parent doc describe the split, then send readers down instead of repeating local details.
- Keep dialogs local to `pages/settings/dialogs/` when they support audit-rule edits or destructive confirmation flows.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not add inline modal branches to `SettingsPage.tsx` when a local dialog file already fits the flow.
- Do not bypass shared save-state helpers with one-off loading badges or toast-only feedback.
- Do not duplicate auth, FX mapping, or billing-currency implementation detail here when the child docs already own it.
