# Typed management clients

- `profileScope.ts` alone decides which management routes receive the pinned `X-Profile-Id` header.
- The profile-scope seam test checks the profile matcher for drift from the backend route contract.
- Keep resource implementations in their existing modules and expose them through `../api.ts`. Use `buildQuery()` and typed parameters; forward caller AbortSignals. Runtime proxy operations remain outside this management client.
- `models.ts` validates required `direct_request_enabled`, incoming-reference count, and configuration warnings; an absent qualification must not become an enabled client entry.
- `modelExport.ts` alone owns Pi source/render/search/binding transport. It uses the literal `/api/models/exports/pi/*` and `/api/models/{id}/pi*` routes, `cache: "no-store"`, and types from `../types/model-export.ts`. Feature callers use `api.modelExport`; refresh commits retain the `PiRefreshCommitRequest` `expected_*` payload.
- `opencodeExport.ts` owns only OpenCode source/render transport through the static `/api/models/exports/opencode/*` routes. Both calls forward abort signals and use `cache: "no-store"`; generated credentials and content stay outside query caches.
- Observe reads preserve explicit attribution scope, and model metrics retain their three named scope blocks.
- Endpoint-specific error guards stay in `endpointErrors.ts`; do not globally specialize `ApiError`. Pricing, costing, retention, and catalog mutations retain their resource-specific preflight/CAS payloads.
