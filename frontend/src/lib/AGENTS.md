# API and browser infrastructure

- Keep `api.ts` and `types.ts` as public barrels. Resource request implementations and backend-aligned contracts belong to [api](api/AGENTS.md) and [types](types/AGENTS.md), respectively; this layer does not import feature implementations.
- `referenceData.ts` and `referenceDataRegistry.ts` own shared lookup reuse, in-flight deduplication, and revision invalidation. Resource mutations must reconcile these owners instead of creating parallel page caches.
- `reportingCurrency.ts` owns normalization, active currency, cache, fallback, and provider prime/refresh support. `costing.ts` formats costs on that state; `timezone.ts` owns timezone preference cache and preview helpers.
- `appVersion.ts` formats Vite-injected package version/build metadata for the shell. Do not hard-code a second visible version.
- `clipboard.ts` owns copy behavior: try `navigator.clipboard.writeText`, then the textarea/`execCommand` fallback. Keep its optional container argument so modal callers can provide their own focus scope; request-log sheets use that seam. A copy failure is not authorization to download.
- `loadbalanceRoutingPolicy.ts` owns shared Ban Policy defaults and normalization; `observeReturn.ts` validates routing-health return state from Requests.
