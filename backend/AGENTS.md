# Backend Guide

The Go entrypoint is `cmd/prism-backend/main.go`. Read [internal ownership](internal/AGENTS.md) before implementation changes and [test boundaries](tests/AGENTS.md) before adding regressions. Shared contracts and engineering requirements live in [Architecture](../docs/architecture.md) and [Development Rules](../docs/development-rules.md).

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
