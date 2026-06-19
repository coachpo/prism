# FRONTEND STARTUP FEATURE KNOWLEDGE BASE

## OVERVIEW
`frontend/src/features/settings/startup/` owns the Startup tab implementation for Prism's file-backed bootstrap config: frontend validation, field metadata, apply-capability rendering, dangerous confirmation checklists, secret update staging, and section composition.

## STRUCTURE
```text
startup/
├── SettingsStartupTab.tsx       # Startup tab orchestration, validation/save flow, review panel
├── startupFieldMetadata.ts      # Field paths, normalization, validation rows, apply-result shaping
├── StartupServerSection.tsx     # Shared section UI helpers plus server/CORS/file status cards
├── StartupDatabaseSection.tsx   # Database, pool, JWT, and bundle-key fields
├── StartupRuntimeSection.tsx    # Runtime transport and side-effect fields
├── StartupTelemetrySection.tsx  # OTLP exporter, metrics, traces, and auth header fields
└── StartupMailSecretsSection.tsx # Mail/SMTP, secret inputs, danger dialog, mobile review panel
```

## WHERE TO LOOK
- Full tab state machine, backend validation-before-save, `expected_revision`/`expected_etag`, confirmation tokens, and secret update payloads: `SettingsStartupTab.tsx`
- Frontend-only validation, safe defaults for incomplete mail/telemetry UI state, capability field grouping, and apply-result messages: `startupFieldMetadata.ts`
- Section components and shared field renderers: `StartupServerSection.tsx`, `StartupDatabaseSection.tsx`, `StartupRuntimeSection.tsx`, `StartupTelemetrySection.tsx`, `StartupMailSecretsSection.tsx`
- Backend file-backed bootstrap contract: `../../../../../backend/internal/httpapi/management/bootstrapconfig/AGENTS.md`, `../../../../../backend/internal/platform/config/`
- Settings shell handoff: `../../../pages/settings/AGENTS.md`, `../../../pages/SettingsPage.tsx`
- E2E startup coverage: `../../../../tests/e2e/settings-startup-tab.spec.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid deprecated compatibility wrappers listed there.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep backend-owned startup defaults out of frontend tables; render backend-provided values, capabilities, planned changes, and safe secret metadata.
- Preserve `runtime.secretEncryptionKey` as preserve-only in v1. Do not stage replacement input for it.
- Keep frontend validation as a preflight; successful save still validates through `api.config.bootstrap.validate()` before `update()`.
- Keep dangerous restart-required changes behind backend-provided confirmation tokens and the review checklist.
- Keep mail and telemetry incomplete-config helpers local to `startupFieldMetadata.ts` so section components stay render-focused.

## ANTI-PATTERNS
- Do not persist redacted placeholders or masked values as replacement secrets.
- Do not treat external file edits as watched state; hot publication is through the Startup tab or bootstrap API.
- Do not invent frontend env vars for settings already owned by the plaintext startup config.
- Do not bypass backend validation or save directly after client-only checks.
