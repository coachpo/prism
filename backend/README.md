# Prism Backend

**Go backend runtime for proxy routing, load balancing, telemetry, and management APIs.**

This directory owns Prism's live Go backend service.

## Project structure

```text
backend/
├── cmd/prism-backend/              # Go process entrypoint
├── internal/httpapi/               # management, runtime, realtime, and docs handlers
├── internal/platform/              # config, server assembly, migrations, startup, version
├── internal/domain/                # audit, loadbalance, and stats domain logic
├── internal/{endpoint,profile,vendor}domain/ # shared management-domain helpers
├── migrations/                     # SQL migration chain applied by the Go runtime
├── testdata/                       # checked-in OpenAPI, bundle, and realtime fixtures
├── tests/                          # Go contract, integration, and runtime regressions
├── docker-compose.yml              # local PostgreSQL provisioning
├── Dockerfile                      # Go backend image build
└── VERSION                         # backend version surface
```

## Running

Recommended local entrypoints:
```bash
../start.sh headless
../start.sh full
```

When launched through `../start.sh`, the backend listens on `http://localhost:18000` and serves:
- Swagger UI: `http://localhost:18000/docs`
- ReDoc: `http://localhost:18000/redoc`
- OpenAPI JSON: `http://localhost:18000/openapi.json`

Direct Go runs from `backend/` use `PRISM_CONFIG_PATH` plus `PRISM_CONFIG_MASTER_KEY`; legacy `HOST`, `PORT`, and related app env vars are seed inputs only when that bootstrap file does not exist yet.

## Verification

Use the Go regression packages directly for targeted validation:

```bash
go test ./tests/contract ./tests/integration ./tests/runtime
go test ./...
go build ./cmd/prism-backend
```

## Configuration
- Supported steady-state backend startup uses `PRISM_CONFIG_PATH` plus `PRISM_CONFIG_MASTER_KEY`.
- When the bootstrap file already exists, Prism loads startup settings from it and the legacy app env surface is no longer the supported source of truth.
- When the bootstrap file is missing, Prism can seed it once from legacy startup env inputs such as `DATABASE_URL`, `HOST`, `PORT`, `CORS_ALLOWED_ORIGINS`, auth settings, and the optional `CONFIG_BUNDLE_ENCRYPTION_KEY`; when that optional bundle key is omitted, Prism falls back to `PRISM_CONFIG_MASTER_KEY`.
- Profile backup/restore, vendor catalog export/import, and other settings-page state flows remain PostgreSQL-backed state transport; `bootstrap-config.json` owns startup inputs only.
- `../start.sh` loads `.env`, requires `PRISM_CONFIG_MASTER_KEY` explicitly, provisions local PostgreSQL, and when `PRISM_CONFIG_PATH` is unset creates a launcher-local bootstrap file so local runs keep backend `18000` and the expected frontend CORS origins.
- Before booting, `../start.sh` resolves the effective backend bind settings through the bootstrap-managed startup path; if an existing bootstrap file resolves to a different backend port or a non-local bind host, the launcher fails early instead of starting against mismatched assumptions.
- Direct Go runs should prefer an absolute `PRISM_CONFIG_PATH`.

## Database and docs artifacts
- Schema migrations are Go-managed and applied from `migrations/` at startup.
- Startup now fails fast when an existing database has application tables but no `prism_schema_migrations` history; reset incompatible local databases instead of relying on migration cutover bridges.
- `docs/openapi.json` is the checked-in management and health contract that the Go server serves at `/openapi.json`.

For local PostgreSQL provisioning without the root launcher, run `docker compose up -d postgres` from `backend/`.
