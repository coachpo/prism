# FRONTEND SETTINGS STARTUP CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/startup/` owns the dense bootstrap-config editing surface mounted by `../SettingsStartupTab.tsx`: field metadata, capability/effect badges, server/database/runtime/mail+secret section rendering, dangerous confirmations, validation rows, and save/apply effect copy. It renders backend-provided startup values and owns no canonical backend startup default table.

## STRUCTURE
```text
startup/
├── StartupServerSection.tsx      # server/CORS, capability badges, and shared input helpers
├── StartupDatabaseSection.tsx    # database URL, named pool lanes, and management-admission fields
├── StartupRuntimeSection.tsx     # runtime transport, buffering, and side-effects timeout fields
├── StartupMailSecretsSection.tsx # auth/cookies, mail/SMTP, secrets, dangerous confirmations, validate/save
└── startupFieldMetadata.ts       # field labels, secret keys, effect helpers, validation mapping
```

## WHERE TO LOOK
- Tab shell and fetch/save orchestration: `../SettingsStartupTab.tsx`
- Capability badges, effect labels, date formatting, dangerous confirmation helpers, and field-path groups: `startupFieldMetadata.ts`, `StartupServerSection.tsx`
- Server/CORS section: `StartupServerSection.tsx`
- Database URL, pool lanes, and management-admission section: `StartupDatabaseSection.tsx`
- Runtime transport, raw `runtime.transport.requestTimeout`, and API `runtime.side_effects.attempt_timeout` section: `StartupRuntimeSection.tsx`
- Auth, cookies, mail, secrets, validation, and save dialog flow: `StartupMailSecretsSection.tsx`
- Backend bootstrap API and response contract: `../../../../../backend/internal/httpapi/management/bootstrapconfig/AGENTS.md`
- Backend bootstrap schema contract: `../../../../../backend/internal/platform/config/`
- E2E coverage: `../../../../tests/e2e/settings-startup-tab.spec.ts`

## CONVENTIONS
- Keep field registries and effect helpers in `startupFieldMetadata.ts`; keep section components presentation-focused.
- Keep runtime transport and side-effects timeout handling explicit. Raw file field `runtime.transport.requestTimeout` appears as API field `runtime.transport.request_timeout`, is seeded as `300s`, and is hot-applicable. Raw file field `runtime.sideEffects.attemptTimeout` appears as API field `runtime.side_effects.attempt_timeout`, is seeded as `10s`, and remains restart-required.
- Keep secret replacement fields and dangerous confirmations local to this cluster instead of pushing them back into `SettingsPage.tsx`.
- Mirror backend apply-capability and failed-hot-apply semantics in copy and effect badges, not in bespoke page-level state.
- Keep bootstrap config editing separate from profile-scoped settings and from config import/export.
- Keep missing pool or startup values empty or validation-driven instead of filling them from a frontend canonical default table. Current backend fresh seeds provide `8000`, `5173`, `15432`, pool split `4/8/4/2/2/2/2`, runtime transport `100/16/16/300s/90s/0s/10s/1s`, and admission `3/2`.
- Existing valid bootstrap files may show older values because startup preserves them. Reset guidance belongs in copy as manual operator action: stop Prism, remove or relocate the bootstrap file, then restart.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS
- Do not inflate `SettingsPage.tsx` with field-level bootstrap logic.
- Do not duplicate capability/effect rendering outside `startupFieldMetadata.ts` and the startup section components.
- Do not mix startup bootstrap editing with profile-scoped settings or config-bundle flows.
- Do not hide restart-required fields behind generic form state.
