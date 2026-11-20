# Backend Guide

The Go entrypoint is `cmd/prism-backend/main.go`. Read the guide of the `internal/` boundary you change — [platform](internal/platform/AGENTS.md), [HTTP API](internal/httpapi/AGENTS.md), [gateway](internal/gateway/AGENTS.md), or [domain](internal/domain/AGENTS.md) — and [test boundaries](tests/AGENTS.md) before adding regressions. Shared contracts and engineering requirements live in [Architecture](../ARCHITECTURE.md) and [Development Rules](../CONTRIBUTING.md#project-engineering-rules).

- Do not move HTTP parsing or profile resolution into domain packages, provider behavior into platform assembly, or feature policy into shared transaction helpers. `pgxutil/tx.go` owns transaction mechanics only; keep `endpointdomain/`, `profiledomain/`, and `providerauth/` narrow shared contracts. `openaimodecheck/` is the read-only native text-mode equality check shared by startup and upgrade preflight.
- Keep SQL schema changes under `migrations/` and apply them through `internal/platform/migrate/`. Baseline/history and migration-specific guards protect retained data; an error asking for a rebuild does not authorize a reset.
- Keep bootstrap settings separate from PostgreSQL-backed product settings. Bootstrap ownership is [platform/config](internal/platform/config/AGENTS.md); startup sequencing is [platform/startup](internal/platform/startup/AGENTS.md).
- Use `internal/providerauth/` for API-family and native capability rules; external catalog metadata must not decide runtime compatibility.
- Changes to upstream request/response behavior need both package-local runtime/provider checks and the operation-shaped external runtime suite, including streaming and non-streaming paths.

Run from `backend/`; these commands match the root CI workflow:

```bash
go test ./internal/... ./cmd/...
go test -timeout 30m ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
go build ./cmd/prism-backend
```
