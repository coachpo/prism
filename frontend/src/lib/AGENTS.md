# FRONTEND LIB KNOWLEDGE BASE

## OVERVIEW
`src/lib/` is the frontend boundary to backend contracts and browser integrations. It owns the typed API seam, singleton websocket client, shared reference-data caches, frontend-side import validation, explicit Ban Policy loadbalance mirror, selected-profile keyed reporting-currency cache, timezone/cost helpers, app version, and clipboard helpers.

## STRUCTURE
```
lib/
├── api.ts                        # Public API facade re-exporting split modules
├── api/AGENTS.md                 # Typed `/api/*` client module split and grouped ownership
├── api/
│   ├── core.ts                   # API base, X-Profile-Id injection, auth refresh, query builder
│   ├── profileScope.ts           # Profile-scoped management route matcher
│   ├── authSettings.ts           # Auth bootstrap, proxy keys, WebAuthn methods
│   ├── management.ts             # Profiles, vendors, models, endpoints, connections, pricing templates
│   ├── observability.ts          # Usage snapshot, stats, bootstrap config, config import/export, audit, loadbalance, settings costing/timezone
│   └── sidecars.ts               # Global sidecar registration, sync, inventory, mutations
├── websocket.ts                  # Singleton WebSocket client with channel ref-counts and reconnects
├── websocket/AGENTS.md           # Helper split beneath the singleton client
├── websocket/                    # Protocol, subscription, transport/reconnect helpers
├── referenceData.ts              # Shared reference-data cache keyed by profile revision
├── referenceDataRegistry.ts      # Registry of shared reference-data datasets
├── configImportValidation.ts     # Frontend-side mirrored config import contract checks
├── configImportValidationReferences.ts
├── loadbalanceRoutingPolicy.ts   # Dual-family defaults and policy normalization
├── appVersion.ts                 # Browser-facing app version helper built from Vite-injected package metadata
├── reportingCurrency.ts          # Selected-profile keyed reporting-currency cache
├── types.ts + types/             # Backend-aligned payload and domain types
├── costing.ts                    # Shared cost formatting and usage-label helpers
├── timezone.ts                   # Timezone preference cache and formatting helpers used by hooks/pages
├── clipboard.ts                  # Browser clipboard helpers and UX-safe copy flow
└── utils.ts                      # Small generic browser/UI helpers
```

## WHERE TO LOOK

- Public import boundary: `api.ts`
- Typed `/api/*` client split, grouped surfaces, `api/core.ts` request rules, and profile-scope matcher: `api/AGENTS.md`
- Shared lookup cache, request dedupe, and dataset registry: `referenceData.ts`, `referenceDataRegistry.ts`
- Frontend-side config import reference validation: `configImportValidation.ts`, `configImportValidationReferences.ts`
- Shared dual-family load-balance defaults and policy normalization: `loadbalanceRoutingPolicy.ts`
- Browser app version label formatting and Vite-injected package metadata: `appVersion.ts`
- Shared reporting-currency cache, normalization, active-currency sync, `prime()` and `refresh()` support, and fail-open default used by `ReportingCurrencyContext.tsx`: `reportingCurrency.ts`
- WebSocket connection state, reconnects, channel ref-counts, protocol parsing, and profile switching behind the preferred `useRealtimeData()` consumer: `websocket.ts`, `websocket/AGENTS.md`
- Shared timezone preference lookup and formatting helpers consumed by `useTimezone()`: `timezone.ts`
- Shared cost formatting and usage-label helpers layered over the active reporting currency: `costing.ts`
- Browser clipboard helpers reused across route shells and detail views: `clipboard.ts`
- Backend-aligned payload types: `types.ts`, `types/`

## CHILD DOCS

- `api/AGENTS.md`: `core.ts`, `profileScope.ts`, `authSettings.ts`, `management.ts`, `observability.ts`, and `sidecars.ts` ownership beneath the public `api.ts` barrel.
- `websocket/AGENTS.md`: message helpers, subscription bookkeeping, and transport/reconnect rules beneath `websocket.ts`.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Pages and hooks should import from `api.ts` or its exported `stats` helper, not call `fetch()` directly.
- `setApiProfileId()` is fed by `ProfileContext`, and `api/core.ts` is the only place that injects `X-Profile-Id` into selected `/api/*` requests.
- `api/profileScope.ts` owns the route matcher for management calls that should receive `X-Profile-Id`; global sidecar calls intentionally stay out of that allowlist.
- `request()` handles cookie credentials, `ApiError`, and one refresh retry for eligible `/api/*` paths.
- Let `api/AGENTS.md` own the typed client split instead of expanding this parent with module-by-module endpoint detail.
- `referenceData.ts` and `referenceDataRegistry.ts` own shared cache reuse, request dedupe, and revision-keyed lookup invalidation.
- `configImportValidation.ts` owns frontend-side mirrored validation of config import contracts, including config-bundle v3 top-level connections, ordered model access targets, explicit Ban Policy strategy data, and vendor `icon_key` presence, instead of leaving that logic in page components.
- `loadbalanceRoutingPolicy.ts` owns explicit Ban Policy defaults, retry-window labels, and normalized failure-status or ban-policy handling.
- `appVersion.ts` owns the browser-facing frontend version contract so shell chrome reads the synced `frontend/package.json` version through Vite instead of hard-coded literals.
- `reportingCurrency.ts` owns selected-profile keyed cache reuse, active-currency sync, `prime()` or `refresh()` support, fail-open defaults, and normalization of `report_currency_code` or `report_currency_symbol` used by `ReportingCurrencyContext.tsx`, settings, and costing.
- `websocket.ts` owns the singleton client; `websocket/AGENTS.md` owns protocol parsing, subscription bookkeeping, and reconnect transport helpers, while shared React consumers should prefer `useRealtimeData()`.
- `timezone.ts` owns shared timezone preference caching and helper access beneath `useTimezone()`.
- `costing.ts` owns shared cost formatting and usage labels on top of the active reporting currency instead of duplicating cache or normalization logic.
- Keep backend payload naming aligned with server schemas, including `vendor_id`, `vendor_key`, fixed `api_family` fields, vendor `icon_key` on vendor payloads only, `expected_active_profile_id`, and stats or request-log identifiers like `ingress_request_id`.
- Treat `types.ts` as a barrel. Backend-aligned contracts live in `types/` leaf files and should retain server field names.
- Request-log clipboard fallback behavior is shared infrastructure through `clipboard.ts`; route sheets can supply scoped fallback roots, but copy helpers stay here.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not bypass `api/core.ts` or `api/profileScope.ts` for Prism backend requests or selected-profile header rules.
- Do not create ad hoc websocket clients or duplicate subscribe/unsubscribe bookkeeping outside `websocket.ts` and `websocket/`.
- Do not add a parallel reference-data cache when `referenceData.ts` already owns the shared lookup datasets.
- Do not duplicate config import validation in page or dialog code when `configImportValidation.ts` already mirrors that contract.
- Do not duplicate reporting-currency cache or normalization in settings, costing, or page code when `reportingCurrency.ts` already owns that seam.
- Do not duplicate timezone or cost helper logic in page folders when `timezone.ts` and `costing.ts` already own those seams.
- Do not camelCase backend response fields in the shared type layer.
- Do not split one backend endpoint family across multiple client modules unless the backend boundary changes first.
