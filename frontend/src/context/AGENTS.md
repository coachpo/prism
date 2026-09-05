# Providers

- Consume auth through `useAuth.ts`; helper-layer session coordination belongs to [auth/AGENTS.md](auth/AGENTS.md). `AuthContext.tsx` composes bootstrap, browser listeners, and timer lifecycles.
- `ReportingCurrencyContext.tsx` owns readiness and provider fallback for Default profile id `1`. Its `prime()` and `refresh()` paths delegate cache/normalization to `../lib/reportingCurrency.ts`; settings writes use this seam rather than keeping a local currency cache.
- Auth epoch changes clear completed query/reference snapshots in `../App.tsx`. Last-good data must never cross an auth identity boundary.
