# Typed management clients

- `request.ts` owns base URL, query serialization, cookie credentials, `ApiError`, auth recovery/replay, and the pinned `X-Profile-Id` header. `profileScope.ts` alone decides which management routes receive that header.
- Keep the profile matcher aligned with backend `managementRouteSpecs` and its generated `backend/internal/platform/http/management_route_contract.json`; the profile-scope seam test checks drift. Changes to that artifact belong to the backend generator.
- Keep resource implementations in their existing modules and expose them through `../api.ts`. Use `buildQuery()` and typed parameters; forward caller AbortSignals. Runtime proxy operations remain outside this management client.
- `models.ts` validates required `direct_request_enabled`, incoming-reference count, and configuration warnings; an absent qualification must not become an enabled client entry.
- `modelExport.ts` alone owns Pi source/render/search/binding transport. It uses the literal `/api/models/exports/pi/*` and `/api/models/{id}/pi*` routes, `cache: "no-store"`, and types from `../types/model-export.ts`. Feature callers use `api.modelExport`; refresh commits retain the `PiRefreshCommitRequest` `expected_*` payload.
- `requestStats.ts` owns attempts/chains/detail/CSV; `statistics.ts` owns aggregates, composed through `stats.ts`. Observe reads preserve explicit attribution scope, and model metrics retain their three named scope blocks.
- Endpoint-specific error guards stay in `endpointErrors.ts`; do not globally specialize `ApiError`. Pricing, costing, retention, and catalog mutations retain their resource-specific preflight/CAS payloads.

Relevant seams include `request.test.ts`, `../../../tests/lib/profile_scope_header_contract.test.mjs`, `../../../tests/lib/model_export_api_contract.test.mjs`, and `../../../tests/lib/observability_api_contract.test.mjs`.
