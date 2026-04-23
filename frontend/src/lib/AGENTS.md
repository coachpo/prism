# FRONTEND LIB KNOWLEDGE BASE

## OVERVIEW
`src/lib/` is the frontend boundary to backend contracts and browser integrations. Keep the shared hotspots here: `api/core.ts`, `websocket.ts`, `referenceData.ts`, `configImportValidation.ts`, `loadbalanceRoutingPolicy.ts`, `appVersion.ts`, `reportingCurrency.ts`, `timezone.ts`, `costing.ts`, `clipboard.ts`, and `webauthn.ts`. This layer now owns the dual-family loadbalance contract mirror, including full adaptive `routing_policy` round-trips, family-specific import validation, shared objective/ban-policy defaults, and the selected-profile keyed reporting-currency cache used by settings and costing. `websocket/AGENTS.md` owns the helper split beneath the singleton client, and stats callers should go through the typed observability clients for the unified usage-snapshot route and the retained shared stats routes.

## STRUCTURE
```
lib/
├── api.ts                        # Public API facade re-exporting split modules
├── api/AGENTS.md                 # Typed `/api/*` client module split and grouped ownership
├── api/
│   ├── core.ts                   # API base, X-Profile-Id injection, auth refresh, query builder
│   ├── authSettings.ts           # Auth bootstrap, proxy keys, WebAuthn methods
│   ├── management.ts             # Profiles, vendors, models, endpoints, connections, pricing templates
│   └── observability.ts          # Usage snapshot, summary, spending, throughput, metrics, timezone, current config format, audit, loadbalance
├── websocket.ts                  # Singleton WebSocket client with channel ref-counts and reconnects
├── websocket/AGENTS.md           # Helper split beneath the singleton client
├── websocket/                    # Protocol parsing, subscription bookkeeping, transport/reconnect helpers
├── referenceData.ts              # Shared reference-data cache keyed by profile revision
├── referenceDataRegistry.ts      # Registry of shared reference-data datasets
├── configImportValidation.ts     # Frontend-side config import validation mirrored from backend contracts
├── configImportValidationReferences.ts
├── loadbalanceRoutingPolicy.ts   # Dual-family defaults, adaptive objective labels, and ban/failure-policy normalization
├── appVersion.ts                 # Browser-facing app version helper built from Vite-injected package metadata
├── reportingCurrency.ts          # Selected-profile keyed reporting-currency cache, normalization, and active-currency sync
├── webauthn.ts                   # Browser passkey ceremony helpers
├── types.ts + types/             # Backend-aligned payload and domain types
├── costing.ts                    # Shared cost formatting and usage-label helpers
├── timezone.ts                   # Timezone preference cache and formatting helpers used by hooks/pages
├── clipboard.ts                  # Browser clipboard helpers and UX-safe copy flow
└── utils.ts                      # Small generic browser/UI helpers
```

## WHERE TO LOOK

- Public import boundary: `api.ts`
- Typed `/api/*` client split, grouped surfaces, and `api/core.ts` request rules: `api/AGENTS.md`
- Shared vendor cache, request dedupe, and dataset registry: `referenceData.ts`, `referenceDataRegistry.ts`
- Frontend-side config import reference validation: `configImportValidation.ts`, `configImportValidationReferences.ts`
- Shared dual-family load-balance defaults, adaptive objective labels, and failure-status or ban-policy normalization: `loadbalanceRoutingPolicy.ts`
- Browser app version label formatting and Vite-injected package metadata: `appVersion.ts`
- Shared reporting-currency cache, normalization of `report_currency_code` or `report_currency_symbol`, active-currency sync, and fail-open default: `reportingCurrency.ts`
- WebSocket connection state, reconnects, channel ref-counts, protocol parsing, and profile switching: `websocket.ts`, `websocket/AGENTS.md`
- Shared timezone preference lookup and formatting helpers consumed by `useTimezone()`: `timezone.ts`
- Shared cost formatting and usage-label helpers layered over the active reporting currency: `costing.ts`
- Browser clipboard helpers reused across route shells and detail views: `clipboard.ts`
- Browser passkey helpers and support checks: `webauthn.ts`
- Backend-aligned payload types: `types.ts`, `types/`

## CHILD DOCS

- `api/AGENTS.md`: `core.ts`, `authSettings.ts`, `management.ts`, and `observability.ts` ownership beneath the public `api.ts` barrel.
- `websocket/AGENTS.md`: message helpers, subscription bookkeeping, and transport/reconnect rules beneath `websocket.ts`.

## CONVENTIONS

- Pages and hooks should import from `api.ts` or the exported helpers, not call `fetch()` directly.
- `setApiProfileId()` is fed by `ProfileContext`, and `api/core.ts` is the only place that injects `X-Profile-Id` into `/api/*` requests.
- `request()` handles cookie credentials, `ApiError`, and one refresh retry for eligible `/api/*` paths.
- Let `api/AGENTS.md` own the typed client split instead of expanding this parent with module-by-module endpoint detail.
- `referenceData.ts` and `referenceDataRegistry.ts` own shared cache reuse, request dedupe, and revision-keyed invalidation for lookup datasets.
- `configImportValidation.ts` owns frontend-side validation of the current import payload shape, including family-specific legacy/adaptive strategy data, inactive-side `null` fields from backend export, and vendor `icon_key` presence, instead of leaving that logic in page components.
- `loadbalanceRoutingPolicy.ts` owns dual-family defaults, adaptive objective labels, full-policy preservation helpers, and normalized failure-status or ban-policy handling used by settings and model flows.
- `appVersion.ts` owns the browser-facing frontend version contract so shell chrome reads the synced `frontend/package.json` version through Vite instead of hard-coded literals.
- `reportingCurrency.ts` owns selected-profile keyed cache reuse, active-currency sync, fail-open defaults, and normalization of `report_currency_code` or `report_currency_symbol` used by settings and costing.
- `websocket.ts` owns the singleton client, while `websocket/AGENTS.md` owns protocol parsing, subscription bookkeeping, and reconnect transport helpers. Consumers should use `useRealtimeData()` instead of creating clients directly.
- `timezone.ts` owns shared timezone preference caching and helper access beneath `useTimezone()`.
- `costing.ts` owns shared cost formatting and usage labels on top of the active reporting currency instead of duplicating cache or normalization logic.
- Keep browser WebAuthn ceremony code in `webauthn.ts`.
- Keep backend payload naming aligned with server schemas, including `vendor_id`, `vendor_key`, fixed `api_family` fields, vendor `icon_key` on vendor payloads only, and stats snapshot identifiers like `ingress_request_id`.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not bypass `api/core.ts` for Prism backend requests or inject `X-Profile-Id` from pages.
- Do not create ad hoc websocket clients or duplicate subscribe/unsubscribe bookkeeping outside `websocket.ts` and `websocket/`.
- Do not add a parallel reference-data cache when `referenceData.ts` already owns the shared lookup datasets.
- Do not duplicate config import validation in page or dialog code when `configImportValidation.ts` already mirrors that contract.
- Do not duplicate reporting-currency cache or normalization in settings, costing, or page code when `reportingCurrency.ts` already owns that seam.
- Do not move passkey browser ceremony into page components when `webauthn.ts` already owns it.
- Do not duplicate timezone or cost helper logic in page folders when `timezone.ts` and `costing.ts` already own those seams.
- Do not camelCase backend response fields in the shared type layer.
