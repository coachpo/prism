# FRONTEND API CLIENT KNOWLEDGE BASE

## OVERVIEW
`lib/api/` is the typed `/api/*` client split behind `../api.ts`. It owns shared request plumbing in `core.ts`, profile-scope route matching in `profileScope.ts`, then groups endpoints by auth/settings, management CRUD, observability, config-rule, audit, loadbalance, and settings surfaces.

## STRUCTURE
```text
api/
├── core.ts           # API base, credentials, X-Profile-Id injection, refresh retry, query builder
├── profileScope.ts   # Management-route matcher for pinned Default-profile headers
├── authSettings.ts   # Auth bootstrap/session/login/logout, settings.auth, and proxy keys
├── management.ts     # Models, access targets, loadbalance strategies, endpoints, connections, pricing templates
├── observability.ts  # Stats, usage snapshot, config rules, audit, loadbalance events/current-state, settings costing/timezone/retention
├── endpointErrors.ts # Typed Endpoint error guards; never specializes `ApiError` globally
└── *.test.ts         # Client core coverage
```

## WHERE TO LOOK
- Public import surface over these modules: `../api.ts`
- Shared request rules, cookie credentials, `ApiError`, auth-refresh retry, and pinned `X-Profile-Id: 1` injection for Default-profile management routes: `core.ts`
- Route allowlist for management calls that should receive `X-Profile-Id`: `profileScope.ts`; drift tests assert it against `../../../backend/internal/platform/http/management_route_contract.json`
- Cookie-auth bootstrap/session flows, settings auth endpoints, and proxy-key endpoints: `authSettings.ts`
- Default-profile model, access-target, loadbalance strategy, endpoint, connection, and pricing-template surfaces: `management.ts`
- Observability, usage snapshot, throughput, header-blocklist and user-agent/client rules, audit logs, loadbalance current state/events, and settings costing/timezone/API-family audit/retention clients: `observability.ts`
- Runtime operation paths `/v1` and `/v1beta` stay outside this client split; launcher/Vite proxying passes them through and backend runtime owns allowlist enforcement.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `core.ts` as the only place that injects `X-Profile-Id`, applies cookie credentials, and performs one refresh retry for eligible `/api/*` requests.
- Keep `profileScope.ts` as the only route matcher deciding which management calls receive `X-Profile-Id`.
- When adding or changing profile-scoped management routes, update `backend/internal/platform/http/management_route_contract.json` in the same change so frontend drift tests keep the matcher in backend contract lockstep.
- Keep grouped endpoint surfaces in their existing modules instead of expanding `api.ts` into a second implementation layer.
- Keep model payload normalization in `management.ts` aligned with server-shaped CRUD and access-target fields.
- Keep auth/settings nesting in `authSettings.ts` and `api.settings` aligned with the backend route structure.
- Keep observability-side query building centralized through `buildQuery()` and typed param objects, including config-rule clients consumed by settings surfaces.
- Import statistics through the public `stats` export from `../api.ts` when a caller needs the standalone stats helper; use `api.stats` when staying on the grouped facade.
- Keep runtime-route pass-through out of this client split; `api/core.ts` only governs `/api/*` requests.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not call `fetch()` directly for Prism backend requests when this client layer already owns credentials and error handling.
- Do not inject `X-Profile-Id` or maintain a second profile-scope route list from pages, hooks, or provider code outside `core.ts` and `profileScope.ts`.
- Do not split one endpoint family across multiple client modules without a real backend-boundary change.
- Do not route `/v1` or `/v1beta` runtime operations through this client layer.
