# FRONTEND SETTINGS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/settings/` is the route-domain shell behind `../SettingsPage.tsx`. It owns the Profile, Global, and Startup tab split, stable section and tab helpers, hash-driven section focus, shared save-state rendering, and the dialog handoff that supports settings mutations. Keep shell behavior here, section rendering in `sections/AGENTS.md`, dialog-local flows in `dialogs/AGENTS.md`, startup bootstrap details under `../../features/settings/startup/`, and costing state in `costing/AGENTS.md`.

## STRUCTURE
```
settings/
├── sections/                      # Rendered settings sections and nested local clusters
├── costing/                       # Costing bootstrap, derived state, FX mapping CRUD, save flows
├── dialogs/
│   ├── AGENTS.md                  # Delete confirmation and audit-rule dialogs
│   └── ...
├── SettingsSectionsNav.tsx         # Sticky section navigation for profile-tab sections
├── SettingsProfileTab.tsx          # Profile-tab body and section layout
├── SettingsGlobalTab.tsx           # Global-tab body for auth plus retention and deletion
├── SettingsStartupTab.tsx         # Startup-tab body for plaintext bootstrap config
├── useSettingsPageData.ts          # Top-level page composition across auth, costing, audit, retention
├── useSettingsPageSectionState.ts  # Active tab, hash, scroll focus, and section jumps
├── useAuthenticationSettingsData.ts
├── useCostingSettingsData.ts
├── useAuditConfigurationData.ts
├── useRetentionDeletionData.ts
├── sectionSaveState.tsx           # Shared dirty, saving, and recently-saved rendering
├── settingsPageHelpers.ts         # Tab ids, section ids, default costing form, shared validation helpers
└── settingsSaveTypes.ts
```

## SHELL CONTRACT

- `SettingsPage.tsx` renders three tabs: `Profile`, `Global`, and `Startup`.
- The Profile tab owns selected-profile section navigation and mounts billing and currency, timezone, and audit and privacy.
- The Global tab mounts instance-wide authentication plus retention and deletion. The Startup tab mounts the instance bootstrap config and telemetry surface through `SettingsStartupTab.tsx`, while the dense field registry and section cluster live under `../../features/settings/startup/`.
- `settingsPageHelpers.ts` is the source of truth for tab ids, profile section ids, instance-only section handling, delete keywords, and shared costing and auth validation helpers.

## WHERE TO LOOK

- Thin route shell, tab split, startup-tab mount, section order, and dialog mounts: `../SettingsPage.tsx`, `SettingsStartupTab.tsx`
- Startup tab field groups for secrets, named PostgreSQL pool lanes, runtime transport, telemetry exporter or auth or TLS settings, auth TTL/cookie settings, mail/SMTP, planned changes, and dangerous confirmations: `../../features/settings/startup/`, `SettingsStartupTab.tsx`
- Cross-section composition, selected-profile labeling, and shared save-state handoff: `useSettingsPageData.ts`
- Active tab state, hash updates, scroll-driven focus, and section jump behavior: `useSettingsPageSectionState.ts`, `SettingsSectionsNav.tsx`
- Stable helper constants and form-normalization utilities: `settingsPageHelpers.ts`
- Shared save-state badges and render helpers: `sectionSaveState.tsx`, `settingsSaveTypes.ts`
- Section implementation boundary: `sections/AGENTS.md`
- Costing bootstrap, derived state, FX mapping CRUD, and save boundary: `costing/AGENTS.md`
- Local dialog ownership for destructive actions, vendor CRUD, and audit-rule editing: `dialogs/AGENTS.md`, `useAuditConfigurationData.ts`, `useRetentionDeletionData.ts`
- E2E seams for startup hash/apply behavior, reporting-currency save, and retention flows: `../../../tests/e2e/settings-startup-tab.spec.ts`, `../../../tests/e2e/settings-reporting-currency-save.spec.ts`, `../../../tests/e2e/settings-log-retention.spec.ts`

## CHILD DOCS

- `sections/AGENTS.md`: authentication-adjacent section UI, audit and privacy, billing and currency, retention, timezone, and the nested `authentication/` and `billing-currency/` clusters.
- `sections/billing-currency/AGENTS.md`: reporting currency and FX-mapping rendering leaf.
- `sections/authentication/AGENTS.md`: operator account and recovery email verification surface.
- `dialogs/AGENTS.md`: delete confirmations and audit-rule editors mounted by `../SettingsPage.tsx`.
- `costing/AGENTS.md`: costing bootstrap, dirty-state derivation, FX mapping CRUD, save flows, and the split between costing hooks and billing-currency section UI.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing settings surface is itself a shipped contract or guardrail.
- Keep new settings work sectionized. Extend helper registries, shared hooks, or local dialogs instead of inflating `SettingsPage.tsx`.
- Keep startup bootstrap field metadata and field-effect rendering under `../../features/settings/startup/`.
- Hash navigation is part of the settings UX contract. New profile-tab sections need stable ids and must participate in jump and active-section logic.
- Save-state feedback belongs in `sectionSaveState.tsx` and related helper types, not in ad hoc spinners or toast-only status.
- Keep the scope split clear in copy and behavior: authentication and retention are global, while billing and currency, timezone, and audit and privacy stay profile-scoped. Audit and privacy loads and saves `/api/settings/audit` as a full three-family replacement.
- `SettingsProfileTab.tsx` and `SettingsGlobalTab.tsx` own the tab bodies, while the shell hook keeps their section state synchronized.
- Billing, reporting currency, timezone preference, and FX mappings cross the `sections/` and `costing/` boundary. Let this parent doc describe the split, then send readers down instead of repeating local details.
- Keep dialogs local to `pages/settings/dialogs/` when they support audit-rule edits, audit-rule edits or destructive confirmation flows, and let `dialogs/AGENTS.md` own the per-file split.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not add inline modal branches to `SettingsPage.tsx` when a local dialog file already fits the flow.
- Do not bypass shared save-state helpers with one-off loading badges or toast-only feedback.
- Do not duplicate auth, FX mapping, or billing-currency implementation detail here when the child docs already own it.
- Do not move bootstrap field registry logic back into `SettingsPage.tsx` or the route shell.
