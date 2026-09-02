# FRONTEND API CLIENT KNOWLEDGE BASE

## OVERVIEW

`lib/api/` is the typed `/api/*` client split behind `../api.ts`. It owns shared request and auth plumbing in `request.ts`, profile-scope route matching in `profileScope.ts`, then groups endpoints by auth/settings, management CRUD, observability, config-rule, audit, loadbalance, and settings surfaces.

## STRUCTURE

```text
api/
├── request.ts        # API base, query serialization, credentials, X-Profile-Id injection, refresh retry, ApiError
├── profileScope.ts   # Management-route matcher for pinned Default-profile headers
├── authSettings.ts   # Auth bootstrap/session/login/logout, settings.auth, and proxy keys
├── models.ts         # Model CRUD, access targets, model connections, and models.dev catalog routes (typed CAS bind/refresh/override/unbind payloads)
├── modelExport.ts    # Pi-only export/render/search/binding client; types imported from lib/types, exposed as api.modelExport
├── loadbalanceStrategies.ts # Loadbalance strategy CRUD, defaults, impact, and preview
├── endpoints.ts      # Endpoint CRUD, verification, references, and orphan cleanup
├── connections.ts    # Shared connection reference reads
├── pricingTemplates.ts # Pricing-template CRUD, history, impact, and catalog pricing
├── observability.ts  # Compatibility barrel for split retained-observability clients
├── stats.ts          # Public stats namespace composed from resource clients
├── requestStats.ts   # Request-log list, chain, detail, export, and filter clients
├── statistics.ts     # Dashboard, aggregate, spending, throughput, and metric clients
├── settingsCosting.ts # Costing, timezone, and currency-migration routes
├── settingsAudit.ts  # API-family audit settings and storage summary routes
├── settingsRetention.ts # Retention policy, preflight, and job routes
├── configRules.ts    # Header blocklist and User-Agent client-rule routes
├── audit.ts          # Audit-log list/detail routes
├── loadbalance.ts    # Loadbalance current-state, event, and incident routes
├── observe.ts        # Observe analytics clients and response contracts
├── model_routing.ts  # Model routing diagnostics and Terminal Target copies
├── endpointErrors.ts # Typed Endpoint error guards; never specializes `ApiError` globally
└── *.test.ts         # Request and resource-client coverage
```

## WHERE TO LOOK

- Public import surface over these modules: `../api.ts`
- Shared request rules, query serialization, cookie credentials, `ApiError`, auth-refresh retry, and pinned `X-Profile-Id: 1` injection for Default-profile management routes: `request.ts`
- Route allowlist for management calls that should receive `X-Profile-Id`: `profileScope.ts`; drift tests assert it against `../../../backend/internal/platform/http/management_route_contract.json`
- Cookie-auth bootstrap/session flows, settings auth endpoints, and proxy-key endpoints: `authSettings.ts`
- Default-profile model, access-target, model-connection, and catalog surfaces: `models.ts`
- Loadbalance strategy CRUD, defaults, impact, and preview: `loadbalanceStrategies.ts`
- Endpoint, shared connection, and pricing-template resource surfaces: `endpoints.ts`, `connections.ts`, `pricingTemplates.ts`
- Retained request-log clients: `requestStats.ts`; aggregate statistics clients: `statistics.ts`; public `stats` namespace: `stats.ts`
- Costing/currency migration, audit, retention, and config-rule routes: `settingsCosting.ts`, `settingsAudit.ts`, `settingsRetention.ts`, `configRules.ts`
- Audit logs and loadbalance current-state/event clients: `audit.ts`, `loadbalance.ts`
- Observe analytics query-context, summary, series, errors, and activity clients: `observe.ts`
- Diagnostics requests forward AbortSignal; Terminal Target copy has one typed client with both capability dimensions; model delete consumes `{deleted:true}`. Endpoint model statistics use their envelope, never a bare-array assertion.
- Observe query contexts carry an explicit attribution scope. Model metrics fetch all three named scope blocks in one POST; Terminal Target drill-down sends `final_execution|route_attempt` on every scoped read.
- Model routing diagnostics and Terminal Target copy clients: `model_routing.ts`
- Compatibility import barrel for existing direct callers: `observability.ts`; the public application facade remains `../api.ts`
- Runtime operation paths `/v1` and `/v1beta` stay outside this client split; launcher/Vite proxying passes them through and backend runtime owns allowlist enforcement.

## CONVENTIONS

- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `request.ts` as the only place that injects `X-Profile-Id`, applies cookie credentials, and performs one refresh retry for eligible `/api/*` requests.
- Keep `profileScope.ts` as the only route matcher deciding which management calls receive `X-Profile-Id`.
- When adding or changing profile-scoped management routes, update `managementRouteSpecs` in `backend/internal/platform/http/admission.go` and regenerate `backend/internal/platform/http/management_route_contract.json` (it is a backend-generated artifact) in the same change so frontend drift tests keep the matcher in backend contract lockstep.
- Keep grouped endpoint surfaces in their existing modules instead of expanding `api.ts` into a second implementation layer.
- Keep model payload normalization in `models.ts` aligned with server-shaped CRUD and access-target fields.
- Keep `models.ts` strict on the required direct-entry projection (`direct_request_enabled`, incoming Model Target count, and warning array); do not fail open when a management response omits the qualification.
- Keep auth/settings nesting in `authSettings.ts` and `api.settings` aligned with the backend route structure.
- `modelExport.ts` is the only Pi export/binding transport module: every function targets the literal routes (`GET /api/models/exports/pi/source`, `POST /api/models/exports/pi/render`, `GET|POST|PUT|DELETE /api/models/{id}/pi...`), all calls are `cache: no-store`, and all wire types come from `../types/model-export`. The public boundary is `api.modelExport` on `../api.ts`; feature code never deep-imports this file, and `lib/` never imports from `features/`. `refreshModelPiCommit` takes the typed `PiRefreshCommitRequest` (the `expected_*` wire shape) directly, mirroring the models.dev `refreshCommit` contract.
- Keep observability-side query building centralized through `buildQuery()` and typed param objects, including config-rule clients consumed by settings surfaces.
- Import statistics through the public `stats` export from `../api.ts` when a caller needs the standalone stats helper; use `api.stats` when staying on the grouped facade.
- Keep runtime-route pass-through out of this client split; `api/request.ts` only governs `/api/*` requests.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX

- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not call `fetch()` directly for Prism backend requests when this client layer already owns credentials and error handling.
- Do not inject `X-Profile-Id` or maintain a second profile-scope route list from pages, hooks, or provider code outside `request.ts` and `profileScope.ts`.
- Do not split one endpoint family across multiple client modules without a real backend-boundary change.
- Do not route `/v1` or `/v1beta` runtime operations through this client layer.
