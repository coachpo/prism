# Providers

- Consume auth through `useAuth.ts`.
- `ReportingCurrencyContext.tsx` renders children immediately against the default currency instead of gating the console on one costing read. Its `prime()` and `refresh()` paths delegate cache/normalization to `../lib/reportingCurrency.ts`; settings writes use this seam rather than keeping a local currency cache.
- Auth epoch changes clear completed query/reference snapshots in `../App.tsx`. Last-good data must never cross an auth identity boundary.
