# FRONTEND PAGES KNOWLEDGE BASE

## OVERVIEW
`src/pages/` holds auth pages plus oracle-compatible route-domain clusters still referenced by the feature-owned rewrite routes and tests. New protected route mounts live under `src/features/` and `src/app/router/`.

## ROUTE SURFACE
- Public auth routes: `/auth/login`, `/auth/forgot-password`, `/auth/reset-password` with legacy auth redirects from `/login`, `/forgot-password`, and `/reset-password`
- Protected rewrite routes: `/observe`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/models`, `/models/:id`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, `/control/proxy-keys`, `/control/sidecars`
- Root redirect: `/` -> `/observe`

## DOMAINS
- Auth entry and recovery: `LoginPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`
- Feature oracle clusters: dashboard, model detail, endpoints, models, pricing templates, request logs, settings, and statistics helpers still imported by current rewrite feature modules or contract tests
- Settings shell oracle: `SettingsPage.tsx` with Profile, Global, and Startup tabs, plus `settings/sections/`, `settings/dialogs/`, and `settings/costing/`; startup implementation lives under `../features/settings/startup/`

## WHERE TO LOOK
- Mounted rewrite route list, public auth split, and protected shell boundary: `../app/router/appRouter.tsx`, `../App.tsx`
- Oracle-compatible dashboard, React Flow routing diagram, model detail, request logs, settings, and dashboard-owned statistics leaf maps: `dashboard/AGENTS.md`, `dashboard/routing-diagram/AGENTS.md`, `model-detail/AGENTS.md`, `request-logs/AGENTS.md`, `settings/AGENTS.md`, `statistics/AGENTS.md`
- Feature-owned startup and sidecars surfaces: `../features/settings/startup/`, `../features/sidecars/`
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

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep backend access on the shared frontend API boundary rather than inventing page-local fetch layers.
- Keep global routes such as `/sidecars` and `/proxy-api-keys` separate from selected-profile route state. Treat `/settings` as a mixed shell where Profile-tab sections are selected-profile scoped while Global and Startup surfaces are instance scoped.
- Let route files own bookmarkable query or hash state and the first handoff into local hooks.
- Parent-cover local route clusters that do not need their own AGENTS file, including dense local helper folders already documented by the page leaves.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not treat auth pages as protected-shell pages.
- Do not create extra AGENTS files for local helper clusters already covered by their page parent.
- Do not spin up page-specific websocket clients when shared realtime ownership already lives in `src/lib/websocket.ts` and `useRealtimeData()`.
