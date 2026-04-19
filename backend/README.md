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

Direct Go runs from `backend/` use the configured `HOST` and `PORT` values from the environment.

## Verification

Use the Go regression packages directly for targeted validation:

```bash
go test ./tests/contract ./tests/integration ./tests/runtime
go test ./...
go build ./cmd/prism-backend
```

## Configuration
- `DATABASE_URL` should be a PostgreSQL DSN accepted by the Go runtime, for example `postgres://prism:prism@localhost:15432/prism?sslmode=disable`.
- `../start.sh` loads `.env`, provisions local PostgreSQL, and launches the Go backend without rewriting legacy DSN formats or resetting existing databases.
- Other common settings come from `../.env.example`, including `HOST`, `PORT`, `CORS_ALLOWED_ORIGINS`, auth settings, WebAuthn settings, and SMTP settings.

## Database and docs artifacts
- Schema migrations are Go-managed and applied from `migrations/` at startup.
- Startup now fails fast when an existing database has application tables but no `prism_schema_migrations` history; reset incompatible local databases instead of relying on migration cutover bridges.
- `docs/openapi.json` is the checked-in management and health contract that the Go server serves at `/openapi.json`.

For local PostgreSQL provisioning without the root launcher, run `docker compose up -d postgres` from `backend/`.
