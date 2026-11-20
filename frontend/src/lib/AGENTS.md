# API and browser infrastructure

- Keep `api.ts` and `types.ts` as public barrels; this layer does not import feature implementations.
- `referenceData.ts` and `referenceDataRegistry.ts` own shared lookup reuse, in-flight deduplication, and revision invalidation. Resource mutations must reconcile these owners instead of creating parallel page caches.
- `appVersion.ts` formats Vite-injected package version/build metadata for the shell. Do not hard-code a second visible version.
- `clipboard.ts` owns copy behavior: try `navigator.clipboard.writeText`, then the textarea/`execCommand` fallback. Keep its optional container argument so modal callers can provide their own focus scope; request-log sheets use that seam. A copy failure is not authorization to download.
