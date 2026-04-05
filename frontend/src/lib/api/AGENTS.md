# FRONTEND API CLIENT KNOWLEDGE BASE

## OVERVIEW
`lib/api/` is the typed `/api/*` client split behind `../api.ts`. It owns shared request plumbing in `core.ts`, then groups endpoints by auth/settings, management CRUD, and observability/config/audit/loadbalance surfaces.

## STRUCTURE
```
api/
├── core.ts           # API base, credentials, X-Profile-Id injection, refresh retry, query builder
├── authSettings.ts   # Auth bootstrap/session/login/logout, settings.auth, proxy keys, WebAuthn
├── management.ts     # Profiles, vendors, models, loadbalance strategies, endpoints, connections, pricing templates
└── observability.ts  # Stats, usage snapshot, config import/export, audit, loadbalance events/current-state, settings costing/timezone
```

## WHERE TO LOOK

- Public import surface over these modules: `../api.ts`
- Shared request rules, cookie credentials, `ApiError`, auth-refresh retry, and `X-Profile-Id` injection for `/api/*`: `core.ts`
- Cookie-auth bootstrap/session flows, settings auth endpoints, proxy-key endpoints, and browser WebAuthn endpoints: `authSettings.ts`
- Profile-scoped management CRUD surfaces for profiles, vendors, models, loadbalance strategies, endpoints, connections, and pricing templates: `management.ts`
- Observability, usage snapshot, throughput, config import/export, audit, loadbalance current state/events, and settings costing/timezone clients: `observability.ts`

## CONVENTIONS

- Keep `core.ts` as the only place that injects `X-Profile-Id`, applies cookie credentials, and performs one refresh retry for eligible `/api/*` requests.
- Keep grouped endpoint surfaces in their existing modules instead of expanding `api.ts` into a second implementation layer.
- Keep auth/settings nesting in `authSettings.ts` and `api.settings` aligned with the backend route structure.
- Keep observability-side query building centralized through `buildQuery()` and typed param objects.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not call `fetch()` directly for Prism backend requests when this client layer already owns credentials and error handling.
- Do not inject `X-Profile-Id` from pages, hooks, or provider code outside `core.ts`.
- Do not split one endpoint family across multiple client modules without a real backend-boundary change.
