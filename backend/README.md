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

When launched through `../start.sh`, the backend listens on `http://localhost:18000`.

If the selected bootstrap config keeps docs enabled, it also serves:
- Swagger UI: `http://localhost:18000/docs`
- ReDoc: `http://localhost:18000/redoc`
- OpenAPI JSON: `http://localhost:18000/openapi.json`

Direct Go runs from `backend/` use `PRISM_CONFIG_PATH` and a plaintext bootstrap file such as `../config.json`. The only optional startup env vars are `PRISM_CONFIG_PATH` and `DATABASE_URL`, and the default database URL is `postgres://prism:prism@localhost:5432/prism?sslmode=disable`.

## Verification

Use the Go regression packages directly for targeted validation:

```bash
go test ./tests/contract ./tests/integration ./tests/runtime
go test ./...
go build ./cmd/prism-backend
```

## Configuration
- Supported steady-state backend startup uses `PRISM_CONFIG_PATH` and a plaintext bootstrap file such as `../config.json`.
- When the bootstrap file already exists, Prism loads startup settings from it and the legacy app env surface is not the supported source of truth.
- When the bootstrap file is missing, Prism seeds it from built-in defaults plus the optional `DATABASE_URL` input only.
- The startup bootstrap contract is not DB-backed, and profile backup/restore, vendor catalog export/import, and other settings-page state flows remain PostgreSQL-backed state transport.
- `../start.sh` reads the root `../.env`, provisions local PostgreSQL, defaults `PRISM_CONFIG_PATH` to `../config.json`, and seeds that plaintext bootstrap file when it is missing so local runs keep backend `18000` and the local PostgreSQL DSN on host port `5432`.
- Before booting, `../start.sh` verifies that the selected bootstrap file still resolves to the local launcher contract instead of trying to negotiate alternate backend ports or database targets.
- Direct Go runs should prefer an absolute `PRISM_CONFIG_PATH`.
- Bootstrap writes are durable only for the next start, and Prism must be restarted to apply listener, database, auth, or transport changes.
- The bootstrap API stays file-backed only, so `/api/config/bootstrap` is separate from PostgreSQL-backed settings flows.

## Database and docs artifacts
- Schema migrations are Go-managed and applied from `migrations/` at startup.
- Startup now fails fast when an existing database has application tables but no `prism_schema_migrations` history; reset incompatible local databases instead of relying on migration cutover bridges.
- `docs/openapi.json` is the checked-in management and health contract that the Go server serves at `/openapi.json`.

For local PostgreSQL provisioning without the root launcher, run `docker compose up -d postgres` from `backend/`.
