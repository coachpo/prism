# Backend Guide

- Do not move HTTP parsing or profile resolution into domain packages, provider behavior into platform assembly, or feature policy into shared transaction helpers. `pgxutil/tx.go` owns transaction mechanics only; keep `endpointdomain/`, `profiledomain/`, and `providerauth/` narrow shared contracts. `openaimodecheck/` is the read-only native text-mode equality check shared by startup and upgrade preflight.
- Keep SQL schema changes under `migrations/` and apply them through `internal/platform/migrate/`. Baseline/history and migration-specific guards protect retained data; an error asking for a rebuild does not authorize a reset.
- Keep bootstrap settings separate from PostgreSQL-backed product settings.
- `internal/domain/stats/retention_source.go` is the single cutoff/floor/epoch/purge and actual-coverage owner for Observe, Requests, Audit, Events, Settings, and management jobs. Resolve bounds and coverage through it; policy days, row minima, `MIN(created_at)`, page size, or browser time never prove coverage or reconstruct a floor.
- Keep pi.dev export bindings independent from models.dev metadata and pricing bindings in both domain and management code.
- Use `internal/providerauth/` for API-family and native capability rules; external catalog metadata must not decide runtime compatibility.
- Changes to upstream request/response behavior need both package-local runtime/provider checks and the operation-shaped external runtime suite, including streaming and non-streaming paths.
