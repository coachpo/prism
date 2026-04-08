# FRONTEND PAGES KNOWLEDGE BASE

## OVERVIEW
`src/pages/` is the route-domain layer for Prism's mounted frontend pages plus a small set of page-owned drill-down surfaces that are mounted at the app root.

## ROUTE SURFACE
- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- Protected shell routes: `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/statistics`, `/settings`, `/proxy-api-keys`, `/pricing-templates`, `/request-logs`
- Root redirect: `/` -> `/dashboard`

## DOMAINS
- Auth entry and recovery: `LoginPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`
- Observability: `DashboardPage.tsx`, `StatisticsPage.tsx`, `RequestLogsPage.tsx`
- Configuration and routing: `ModelsPage.tsx`, `ModelDetailPage.tsx`, `ProxyModelDetailPage.tsx`, `EndpointsPage.tsx`, `LoadbalanceStrategiesPage.tsx`, `PricingTemplatesPage.tsx`; this is also where dual-family strategy selection and assignment surfaces live
- Access control and runtime credentials: `ProxyApiKeysPage.tsx`
- Settings shell: `SettingsPage.tsx` with `settings/sections/` and `settings/costing/`

## WHERE TO LOOK
- Mounted route list, public auth split, and protected shell boundary: `../App.tsx`
- Dashboard, model detail, request logs, settings, and statistics leaf maps: `dashboard/AGENTS.md`, `model-detail/AGENTS.md`, `request-logs/AGENTS.md`, `settings/AGENTS.md`, `statistics/AGENTS.md`

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
- `statistics/AGENTS.md`

## CONVENTIONS
- Keep backend access on the shared frontend API boundary rather than inventing page-local fetch layers.
- Let route files own bookmarkable query or hash state and the first handoff into local hooks.
- Parent-cover local route clusters that do not need their own AGENTS file, including dense local helper folders already documented by the page leaves.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not treat auth pages as protected-shell pages.
- Do not create extra AGENTS files for local helper clusters already covered by their page parent.
- Do not spin up page-specific websocket clients when shared realtime ownership already lives in `src/lib/websocket.ts` and `useRealtimeData()`.
