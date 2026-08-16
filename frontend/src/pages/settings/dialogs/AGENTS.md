# FRONTEND SETTINGS DIALOG CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/dialogs/` owns the local dialog cluster mounted by `../../SettingsPage.tsx`: destructive confirmations and audit-rule editors or delete flows. Keep dialog rendering and per-form copy here while the parent settings hooks own mutation state and orchestration.

## STRUCTURE
```
dialogs/
├── DeleteConfirmDialog.tsx
├── DeleteRuleConfirmDialog.tsx
├── DeleteUserAgentClientRuleConfirmDialog.tsx
├── RuleDialog.tsx
├── UserAgentClientRuleDialog.tsx

```

## WHERE TO LOOK

- Mounted dialog surface and parent handoff: `../../SettingsPage.tsx`, `../AGENTS.md`
- Shared delete-confirm flow for destructive settings actions: `DeleteConfirmDialog.tsx`
- Audit-rule create, edit, and delete flows: `RuleDialog.tsx`, `DeleteRuleConfirmDialog.tsx`, `UserAgentClientRuleDialog.tsx`, `DeleteUserAgentClientRuleConfirmDialog.tsx`
- Mutation state and save orchestration feeding the dialogs: `../useSettingsPageData.ts`, `../useAuditConfigurationData.ts`, `../useRetentionDeletionData.ts`
- User-agent/client rule copy belongs to frontend seam tests rather than a dedicated Playwright spec.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep dialog files focused on rendering, field composition, and confirm-copy framing.
- Keep mutation state, delete keywords, and save orchestration in the parent settings hooks.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not add inline modal branches back to `../../SettingsPage.tsx` or the section components when this cluster already owns the dialogs.
- Do not duplicate delete-confirm or save orchestration logic inside the dialog files.
