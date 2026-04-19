# Prism Backend

**Go backend runtime for proxy routing, load balancing, telemetry, and cutover verification.**

This directory owns Prism's live Go backend service. The legacy Python runtime tree has been retired from the live repo surface; the remaining backend-local metadata and regression artifacts are non-runtime only.

## Project structure

```text
backend/
├── cmd/prism-backend/              # Go process entrypoint
├── internal/httpapi/               # management, runtime, realtime, and docs handlers
├── internal/platform/              # config, server assembly, migrations, startup, version
├── internal/domain/                # audit, loadbalance, and stats domain logic
├── internal/{endpoint,profile,vendor}domain/ # shared management-domain helpers
├── migrations/                     # SQL migration chain applied by the Go runtime
├── testdata/                       # checked-in OpenAPI, bundles, and cutover fixtures
├── tests/                          # regression roots, including Go cutover suites
├── docker-compose.yml              # local PostgreSQL provisioning
├── Dockerfile                      # Go backend image build
├── pyproject.toml                  # non-runtime cutover metadata stub
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

Targeted cutover checks live in the Go test packages under `tests/`:

```bash
go test ./tests/integration -run 'TestCutover(Rehearsal|SchemaDrift|BaselineStamp)$'
go test ./tests/contract -run 'TestCutoverSmoke$'
```

For broader backend validation, use `go test ./...`.

## Configuration
- `DATABASE_URL` should be a PostgreSQL DSN accepted by the Go runtime, for example `postgres://prism:prism@localhost:15432/prism?sslmode=disable`.
- `../start.sh` normalizes older `postgresql+asyncpg://...` values before launching the Go backend so existing local `.env` files still work during cutover.
- Other common settings come from `../.env.example`, including `HOST`, `PORT`, `CORS_ALLOWED_ORIGINS`, auth settings, WebAuthn settings, and SMTP settings.

## Database and docs artifacts
- Schema migrations are Go-managed and applied from `migrations/` at startup.
- `testdata/schema/cutover-live.sql` and the cutover tests preserve compatibility checks around the legacy `alembic_version` table concept without requiring the retired Python tree.
- `docs/openapi.json` is the checked-in management and health contract that the Go server serves at `/openapi.json`.

For local PostgreSQL provisioning without the root launcher, run `docker compose up -d postgres` from `backend/`.
