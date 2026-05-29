# FRONTEND PAGES KNOWLEDGE BASE

## OVERVIEW
`src/pages/` is the route-domain layer for Prism's mounted frontend pages plus a small set of page-owned drill-down surfaces that are mounted at the app root.

## ROUTE SURFACE
- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- Protected shell routes: `/dashboard`, `/models`, `/models/:id`, `/endpoints`, `/loadbalance-strategies`, `/settings`, `/proxy-api-keys`, `/sidecars`, `/pricing-templates`, `/request-logs`
- Root redirect: `/` -> `/dashboard`

## DOMAINS
- Auth entry and recovery: `LoginPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`
- Observability: `DashboardPage.tsx`, dashboard analytics content from `statistics/`, and `RequestLogsPage.tsx`
- Configuration and routing: `ModelsPage.tsx`, `ModelDetailPage.tsx`, `EndpointsPage.tsx`, `LoadbalanceStrategiesPage.tsx`, `PricingTemplatesPage.tsx`; this is where unified access targets, standalone connections, and explicit Ban Policy strategy assignment surfaces live
- Access control and runtime credentials: `ProxyApiKeysPage.tsx`
- Global sidecar control plane: `SidecarsPage.tsx`, `sidecars/AGENTS.md`
- Settings shell: `SettingsPage.tsx` with Profile, Global, and Startup tabs, plus `settings/sections/`, `settings/dialogs/`, `settings/startup/`, and `settings/costing/`

## WHERE TO LOOK
- Mounted route list, public auth split, and protected shell boundary: `../App.tsx`
- Dashboard, model detail, request logs, settings, startup bootstrap, sidecars, and dashboard-owned statistics leaf maps: `dashboard/AGENTS.md`, `model-detail/AGENTS.md`, `request-logs/AGENTS.md`, `settings/AGENTS.md`, `settings/startup/AGENTS.md`, `sidecars/AGENTS.md`, `statistics/AGENTS.md`
- Settings nested ownership split: `settings/sections/AGENTS.md`, `settings/dialogs/AGENTS.md`, `settings/startup/AGENTS.md`, `settings/costing/AGENTS.md`

## CHILD DOCS
- `dashboard/AGENTS.md`
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
- `settings/startup/AGENTS.md`
- `sidecars/AGENTS.md`
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
