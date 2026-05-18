# FRONTEND API CLIENT KNOWLEDGE BASE

## OVERVIEW
`lib/api/` is the typed `/api/*` client split behind `../api.ts`. It owns shared request plumbing in `core.ts`, profile-scope route matching in `profileScope.ts`, then groups endpoints by auth/settings, management CRUD, observability/bootstrap-config/audit/loadbalance/settings-costing, and global sidecar surfaces.

## STRUCTURE
```
api/
├── core.ts           # API base, credentials, X-Profile-Id injection, refresh retry, query builder
├── profileScope.ts   # Management-route matcher for selected-profile headers
├── authSettings.ts   # Auth bootstrap/session/login/logout, settings.auth, proxy keys, WebAuthn
├── management.ts     # Profiles, vendors, models, loadbalance strategies, endpoints, connections, pricing templates
├── observability.ts  # Stats, usage snapshot, bootstrap config, config import/export, audit, loadbalance events/current-state, settings costing/timezone/retention
└── sidecars.ts       # Global sidecar registration, sync, inventory, mutations
```

## WHERE TO LOOK

- Public import surface over these modules: `../api.ts`
- Shared request rules, cookie credentials, `ApiError`, auth-refresh retry, and `X-Profile-Id` injection for selected management routes: `core.ts`
- Route allowlist for management calls that should receive `X-Profile-Id`: `profileScope.ts`
- Cookie-auth bootstrap/session flows, settings auth endpoints, proxy-key endpoints, and browser WebAuthn endpoints: `authSettings.ts`
- Global profile/vendor management plus profile-scoped model, loadbalance strategy, endpoint, connection, and pricing-template surfaces: `management.ts`
- Observability, usage snapshot, throughput, bootstrap-config get/validate/update, config import/export, audit, loadbalance current state/events, and settings costing/timezone/retention clients: `observability.ts`
- Global sidecar CRUD, test-connection, sync, auth/provider inventory, and direct auth-file mutations: `sidecars.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `core.ts` as the only place that injects `X-Profile-Id`, applies cookie credentials, and performs one refresh retry for eligible `/api/*` requests.
- Keep `profileScope.ts` as the only route matcher deciding which management calls receive `X-Profile-Id`; `/api/sidecars/*` stays global and unscoped.
- Keep grouped endpoint surfaces in their existing modules instead of expanding `api.ts` into a second implementation layer.
- Keep auth/settings nesting in `authSettings.ts` and `api.settings` aligned with the backend route structure.
- Keep observability-side query building centralized through `buildQuery()` and typed param objects, including bootstrap-config validation/update requests consumed by `SettingsStartupTab.tsx`.
- Import statistics through the public `stats` export from `../api.ts` when a caller needs the standalone stats helper; use `api.stats` when staying on the grouped facade.

## ANTI-PATTERNS

- Do not call `fetch()` directly for Prism backend requests when this client layer already owns credentials and error handling.
- Do not inject `X-Profile-Id` or maintain a second profile-scope route list from pages, hooks, or provider code outside `core.ts` and `profileScope.ts`.
- Do not split one endpoint family across multiple client modules without a real backend-boundary change.
