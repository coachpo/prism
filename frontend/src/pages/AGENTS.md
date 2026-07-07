# FRONTEND PAGES KNOWLEDGE BASE

## OVERVIEW
`src/pages/` holds auth pages plus oracle-compatible route-domain clusters still referenced by the feature-owned rewrite routes and tests. New protected route mounts live under `src/features/` and `src/app/router/`.

## ROUTE SURFACE
- Public auth routes: `/auth/login`, `/auth/forgot-password`, `/auth/reset-password`
- Protected rewrite routes: `/observe`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/models`, `/models/:id`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, `/control/proxy-keys`
- Root redirect: `/` -> `/observe`

## DOMAINS
- Auth entry and recovery: `LoginPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`
- Feature oracle clusters: dashboard, model detail, endpoints, models, pricing templates, request logs, settings, and statistics helpers still imported by current rewrite feature modules or contract tests
- Settings shell oracle: `SettingsPage.tsx` with Profile and Global tabs, plus `settings/sections/`, `settings/dialogs/`, and `settings/costing/`.

## WHERE TO LOOK
- Mounted rewrite route list, public auth split, and protected shell boundary: `../app/router/appRouter.tsx`, `../App.tsx`
- Oracle-compatible dashboard, React Flow routing diagram, model detail, request logs, settings, and dashboard-owned statistics leaf maps: `dashboard/AGENTS.md`, `dashboard/routing-diagram/AGENTS.md`, `model-detail/AGENTS.md`, `request-logs/AGENTS.md`, `settings/AGENTS.md`, `statistics/AGENTS.md`
- Active feature-route ownership and leaves: `../features/AGENTS.md`
- Settings nested ownership split: `settings/sections/AGENTS.md`, `settings/sections/authentication/AGENTS.md`, `settings/sections/billing-currency/AGENTS.md`, `settings/dialogs/AGENTS.md`, `settings/costing/AGENTS.md`

## CHILD DOCS
- `dashboard/AGENTS.md`
- `dashboard/routing-diagram/AGENTS.md`
- `endpoints/AGENTS.md`
- `loadbalance-strategies/AGENTS.md`
- `model-detail/AGENTS.md`
- `models/AGENTS.md`
- `pricing-templates/AGENTS.md`
- `proxy-api-keys/AGENTS.md`
- `request-logs/AGENTS.md`
- `settings/AGENTS.md`
- `settings/costing/AGENTS.md`
- `settings/dialogs/AGENTS.md`
- `settings/sections/AGENTS.md`
- `settings/sections/authentication/AGENTS.md`
- `settings/sections/billing-currency/AGENTS.md`
- `statistics/AGENTS.md`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep backend access on the shared frontend API boundary rather than inventing page-local fetch layers.
- Keep global routes such as `/control/proxy-keys` separate from selected-profile route state. Treat `/system/settings` as a mixed shell where Profile-tab sections are selected-profile scoped while Global surfaces are instance scoped.
- Let route files own bookmarkable query or hash state and the first handoff into local hooks.
- Parent-cover local route clusters that do not need their own AGENTS file, including dense local helper folders already documented by the page leaves.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not treat auth pages as protected-shell pages.
- Do not create extra AGENTS files for local helper clusters already covered by their page parent.
- Do not spin up page-specific websocket clients when shared realtime ownership already lives in `../lib/websocket.ts` and `useRealtimeData()`.
