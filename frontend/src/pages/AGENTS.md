# FRONTEND PAGES KNOWLEDGE BASE

## OVERVIEW
`src/pages/` is the route-domain layer for Prism's mounted frontend pages plus a small set of page-owned drill-down surfaces that are mounted at the app root.

## ROUTE SURFACE
- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- Protected shell routes: `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/settings`, `/proxy-api-keys`, `/sidecars`, `/pricing-templates`, `/request-logs`
- Root redirect: `/` -> `/dashboard`

## DOMAINS
- Auth entry and recovery: `LoginPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`
- Observability: `DashboardPage.tsx`, dashboard analytics content, `RequestLogsPage.tsx`
- Configuration and routing: `ModelsPage.tsx`, `ModelDetailPage.tsx`, `ProxyModelDetailPage.tsx`, `EndpointsPage.tsx`, `LoadbalanceStrategiesPage.tsx`, `PricingTemplatesPage.tsx`; this is also where dual-family strategy selection and assignment surfaces live
- Access control and runtime credentials: `ProxyApiKeysPage.tsx`
- Global sidecar control plane: `SidecarsPage.tsx`, `sidecars/AGENTS.md`
- Settings shell: `SettingsPage.tsx` with Profile, Global, and Startup tabs, plus `settings/sections/`, `settings/startup/`, and `settings/costing/`

## WHERE TO LOOK
- Mounted route list, public auth split, and protected shell boundary: `../App.tsx`
- Dashboard, model detail, request logs, settings, startup bootstrap, sidecars, and statistics leaf maps: `dashboard/AGENTS.md`, `model-detail/AGENTS.md`, `request-logs/AGENTS.md`, `settings/AGENTS.md`, `settings/startup/AGENTS.md`, `sidecars/AGENTS.md`, `statistics/AGENTS.md`
- Settings startup cluster and bootstrap field registry: `settings/startup/AGENTS.md`

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
- `settings/startup/AGENTS.md`
- `sidecars/AGENTS.md`
- `statistics/AGENTS.md`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep backend access on the shared frontend API boundary rather than inventing page-local fetch layers.
- Keep global routes such as `/sidecars`, `/settings`, and `/proxy-api-keys` separate from selected-profile route state.
- Let route files own bookmarkable query or hash state and the first handoff into local hooks.
- Parent-cover local route clusters that do not need their own AGENTS file, including dense local helper folders already documented by the page leaves.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not treat auth pages as protected-shell pages.
- Do not create extra AGENTS files for local helper clusters already covered by their page parent.
- Do not spin up page-specific websocket clients when shared realtime ownership already lives in `src/lib/websocket.ts` and `useRealtimeData()`.
