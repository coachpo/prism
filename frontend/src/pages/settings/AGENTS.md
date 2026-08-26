# FRONTEND SETTINGS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/settings/` is the route-domain shell behind `../SettingsPage.tsx`. It owns the canonical `scope=global|instance` URL contract with a section allowlist, stable section helpers, hash-driven section focus, shared save-state rendering, and the dialog handoff that supports settings mutations. Keep shell behavior here, section rendering in `sections/AGENTS.md`, dialog-local flows in `dialogs/AGENTS.md`, and costing state in `costing/AGENTS.md`.

## STRUCTURE
```
settings/
├── sections/                      # Rendered settings sections and nested local clusters
├── costing/                       # Costing bootstrap, derived state, save flows
├── dialogs/
│   ├── AGENTS.md                  # Delete confirmation and audit-rule dialogs
│   └── ...
├── SettingsSaveAction.tsx          # Header save action naming the cards with unsaved edits
├── SettingsSectionsNav.tsx         # Sticky navigation for visible Global-tab sections
├── SettingsProfileTab.tsx          # Visible Global-tab body and section layout
├── SettingsGlobalTab.tsx           # Visible Instance-tab body for auth plus retention and deletion
├── useSettingsPageData.ts          # Top-level page composition across auth, costing, audit, retention
├── useSettingsPageSectionState.ts  # Active tab, hash, scroll focus, and section jumps
├── useAuthenticationSettingsData.ts
├── useCostingSettingsData.ts
├── useAuditConfigurationData.ts       # Thin audit-resource coordinator
├── useAPIFamilyAuditSettings.ts        # API-family audit policy/storage lifecycle
├── useHeaderBlocklistRules.ts          # Header Blocklist read/mutation lifecycle
├── useUserAgentClientRules.ts          # User-Agent Client Rules read/mutation lifecycle
├── useRetentionDeletionData.ts
├── useRetentionPolicy.ts              # Retention policy/CAS/preflight lifecycle
├── useManualCleanup.ts                # Manual-cleanup preflight/job lifecycle
├── useRetentionJobList.ts               # Job snapshot/filter/load-more/cancel lifecycle
├── useRetentionJobDetails.ts            # Job detail and independent evidence lanes
├── retentionProtocol.ts                 # Shared retention operation/preflight protocol
├── manualCleanup.ts                     # Manual-cleanup types and localized labels
├── sectionSaveState.tsx                 # Shared dirty, saving, and recently-saved rendering
├── settingsNavigation.ts                # Scope ids, section allowlists, navigation sections, and URL defaults
├── settingsSaveTypes.ts
└── *.test.tsx                     # Save-action and retention keyword-confirmation coverage
```

## SHELL CONTRACT

- `SettingsPage.tsx` renders two scopes: `全局` (`scope=global`) and `实例` (`scope=instance`). The legacy `tab` query value is dropped during canonicalization.
- The Global scope mounts billing and reporting currency, timezone, and audit and privacy.
- The Instance scope mounts instance-wide authentication plus automatic retention policy with owner actual coverage, manual cleanup, and the retention job center.
- `settingsNavigation.ts` is the source of truth for scope ids, section ids, section allowlists, and default section per scope. Destructive retention confirmation has no client-side keyword: the server issues `confirmation_keyword` with every preflight and compares it exactly.

## WHERE TO LOOK

- Thin route shell, tab split, section order, and dialog mounts: `../SettingsPage.tsx`
- Cross-section composition and shared save-state handoff: `useSettingsPageData.ts`
- Audit resource composition: `useAuditConfigurationData.ts`; resource lifecycles: `useAPIFamilyAuditSettings.ts`, `useHeaderBlocklistRules.ts`, `useUserAgentClientRules.ts`
- Retention resource composition: `useRetentionDeletionData.ts`; lifecycle owners: `useRetentionPolicy.ts`, `useManualCleanup.ts`, `useRetentionJobList.ts`, `useRetentionJobDetails.ts`, `retentionProtocol.ts`
- Active tab state, hash updates, scroll-driven focus, and section jump behavior: `useSettingsPageSectionState.ts`, `SettingsSectionsNav.tsx`
- Settings scope, section allowlists, navigation sections, and URL defaults: `settingsNavigation.ts`
- Shared save-state badges and render helpers: `sectionSaveState.tsx`, `settingsSaveTypes.ts`
- Section implementation boundary: `sections/AGENTS.md`
- Costing bootstrap, derived state, currency-migration refresh, and save boundary: `costing/AGENTS.md`
- Costing form defaults and normalization: `costing/costingForm.ts`
- Currency migration inventory/draft/preview/commit protocol: `costing/currencyMigrationProtocol.ts`
- Manual cleanup types and labels: `manualCleanup.ts`
- Authentication password bounds and validation: `sections/authentication/authenticationPassword.ts`
- Timezone offset and preview presentation: `../../lib/timezone.ts`
- Local dialog ownership for destructive actions, vendor CRUD, and audit-rule editing: `dialogs/AGENTS.md`, `useAuditConfigurationData.ts`, `useRetentionDeletionData.ts`
- Settings seam coverage lives in frontend lib/Vitest tests; the capped Playwright journey set does not include dedicated settings specs.

## CHILD DOCS

- `sections/AGENTS.md`: authentication-adjacent section UI, audit and privacy, billing and currency, retention, timezone, and the nested `authentication/` and `billing-currency/` clusters.
- `sections/billing-currency/AGENTS.md`: reporting currency and currency-migration rendering leaf.
- `sections/authentication/AGENTS.md`: operator account surface.
- `dialogs/AGENTS.md`: delete confirmations and audit-rule editors mounted by `../SettingsPage.tsx`.
- `costing/AGENTS.md`: costing bootstrap, dirty-state derivation, save flows, and the split between costing hooks and billing-currency section UI.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing settings surface is itself a shipped contract or guardrail.
- Keep new settings work sectionized. Extend helper registries, shared hooks, or local dialogs instead of inflating `SettingsPage.tsx`.
- Keep startup bootstrap editing out of the settings route after R2.
- Hash navigation is part of the settings UX contract. New Global-tab sections need stable ids and must participate in jump and active-section logic.
- Save-state feedback belongs in `sectionSaveState.tsx` and related helper types, not in ad hoc spinners or toast-only status.
- Keep the scope split clear in copy and behavior: authentication and retention are instance-wide, while billing and currency, timezone, and audit and privacy use the pinned management scope. Audit and privacy loads and saves `/api/settings/audit` as a full three-family three-state replacement; retention loads `/api/settings/log-retention` and consumes the per-dataset `actual_coverage` owner projection for the coverage cards.
- Destructive retention changes (enable/shorten/cleanup) always go through the fresh server preflight dialog with keyword confirmation and a server-persisted job; job truth is never browser-local.
- `SettingsProfileTab.tsx` and `SettingsGlobalTab.tsx` own the tab bodies, while the shell hook keeps their section state synchronized.
- Billing, reporting currency, timezone preference, and currency migration cross the `sections/` and `costing/` boundary. Let this parent doc describe the split, then send readers down instead of repeating local details.
- Keep dialogs local to `pages/settings/dialogs/` when they support audit-rule edits, audit-rule edits or destructive confirmation flows, and let `dialogs/AGENTS.md` own the per-file split.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not add inline modal branches to `SettingsPage.tsx` when a local dialog file already fits the flow.
- Do not bypass shared save-state helpers with one-off loading badges or toast-only feedback.
- Do not duplicate auth, currency-migration, or billing-currency implementation detail here when the child docs already own it.
- Do not move bootstrap file editing back into `SettingsPage.tsx` or the route shell.
