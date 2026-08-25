# FRONTEND SETTINGS SECTIONS KNOWLEDGE BASE

## OVERVIEW

`pages/settings/sections/` owns the rendered settings sections used by `../../SettingsPage.tsx`. This folder covers the section-level UI for auth setup, audit and privacy, header-blocklist rules, user-agent or client rules, billing and currency, retention and deletion, and timezone preferences, plus the nested `authentication/` and `billing-currency/` leaf clusters. Keep it focused on section rendering, not the shell or costing orchestration.

## STRUCTURE

```text
sections/
├── AuthenticationSection.tsx
├── AuditConfigurationSection.tsx
├── AuditConfigurationAPIFamilyCard.tsx
├── AuditConfigurationHeaderBlocklistCard.tsx
├── AuditConfigurationUserAgentClientRulesCard.tsx
├── AuditConfigurationRuleActions.tsx
├── AuditConfigurationRuleSection.tsx
├── AuditConfigurationRulesPanel.tsx
├── AuditConfigurationRuleTable.tsx
├── BasisAndDisplaySection.tsx     # Reporting currency and timezone in one card, because they are one write
├── RetentionDeletionSection.tsx   # Retention policy draft and owner coverage
├── ManualCleanupSection.tsx       # Danger-outlined immediate deletion: preflight then typed confirmation
├── RetentionJobsSection.tsx       # Server-persisted retention job center
├── authentication/                # Operator-account field leaf cluster
├── billing-currency/AGENTS.md     # Reporting currency and currency-migration leaf cluster
└── *.test.tsx                     # Section-level rendering coverage
```

## WHERE TO LOOK

- Auth setup: `AuthenticationSection.tsx`, `authentication/`
- Audit and privacy API-family audit rows, header blocklist, user-agent or client rule management, and rules-panel rendering: `AuditConfigurationSection.tsx`, `AuditConfigurationAPIFamilyCard.tsx`, `AuditConfigurationHeaderBlocklistCard.tsx`, `AuditConfigurationUserAgentClientRulesCard.tsx`, `AuditConfigurationRulesPanel.tsx`, `AuditConfigurationRuleActions.tsx`, `AuditConfigurationRuleSection.tsx`, `AuditConfigurationRuleTable.tsx`
- Reporting currency and timezone section shell that renders the currency card and the migration/archive dialogs, while staying separate from costing state: `BasisAndDisplaySection.tsx`, `billing-currency/AGENTS.md`
- Retention and deletion section (policy draft, owner actual-coverage cards, fresh preflight dialog handoff): `RetentionDeletionSection.tsx`, `../useRetentionDeletionData.ts`, `../dialogs/RetentionPolicyPreflightDialog.tsx`
- Immediate manual cleanup, kept in its own danger-outlined card rather than folded into the policy card: `ManualCleanupSection.tsx`
- Retention job center (server-persisted jobs as a static browser snapshot, filters, manual refresh plus post-mutation calibration with serial fresh-cursor page walks, two independent detail evidence lanes, cancel): `RetentionJobsSection.tsx`
- Shared page shell, section IDs, and save-state helpers: `../AGENTS.md`, `../settingsPageHelpers.ts`, `../sectionSaveState.tsx`
- Costing bootstrap, derived state, and save logic that feeds billing and timezone sections: `../costing/AGENTS.md`
- Settings seam coverage lives in frontend lib/Vitest tests; the capped Playwright journey set does not include dedicated settings specs.

## LOCAL CLUSTERS

- `authentication/`: operator-account fields and the shared field shell, plus the nested `authentication/AGENTS.md` leaf
- `billing-currency/`: `ReportingCurrencyCard.tsx`, `CurrencyMigrationDialog.tsx`, and `ArchiveUnusedFxDialog.tsx`; its leaf doc lives at `billing-currency/AGENTS.md`

## CONVENTIONS

- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Destructive retention changes (enable/shorten/manual cleanup) are never browser-local truth: they always render the fresh server preflight impact (counts/bytes/partitions/coverage after) and require keyword confirmation, and the created job is server-persisted and tracked in the job center.
- Keep these files focused on section rendering, local field composition, and section-specific copy.
- Let `billing-currency/` own the reporting-currency card and currency-migration dialog presentation.
- Pull bootstrap, dirty-state derivation, and save orchestration from the parent settings hooks instead of rebuilding that logic inside section components.
- Keep section IDs and save-state wiring aligned with the parent settings helpers.
- Let `BasisAndDisplaySection.tsx` stay a rendering boundary. The hooks that own costing changes live in `../costing/`.
- Keep the `?section=timezone` anchor resolving inside `BasisAndDisplaySection.tsx`; the previously shipped deep link must keep landing on the same content.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX

- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move auth setup logic out of `authentication/`.
- Do not move currency-migration or save orchestration into `billing-currency/` presentation components. That boundary belongs to `../costing/`.
- Do not invent extra settings sections or nested AGENTS files beyond the local clusters already covered here. `authentication/AGENTS.md` and `billing-currency/AGENTS.md` are the justified nested leaves.
