# Prism

**A lightweight, self-hosted LLM proxy gateway with routing, load balancing, and observability.**

Prism fronts multiple LLM API families through explicit runtime operations, letting you configure, route, and load-balance requests through a single gateway with a web-based management dashboard.

---

## Features

### Core capabilities

- **Operation-registered runtime support**: allowed routes are `GET /v1/models`, `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/responses/input_tokens`, `POST /v1/responses/compact`, `POST /v1/messages`, `POST /v1/messages/count_tokens`, `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent`, and `POST /v1beta/models/{model}:countTokens`
- **Not a full vendor API clone**: unsupported vendor routes are rejected before provider transport, telemetry, audit, or feedback side effects
- **Terminal Targets**: public model IDs resolve through ordered model targets, while each terminal target is a model-private endpoint binding managed from model detail. Endpoints remain reusable across Terminal Targets
- **Explicit Ban Policy routing**: reusable load-balance strategies use `single`, `fill-first`, or `round-robin` routing plus retry-window settings, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, and `off`, `temporary`, or `until_reset` ban modes
- **Automatic buffering**: operation hooks handle streaming and internal buffered fallbacks for supported routes

### Observability & management

- **Product observability**: retained request history, usage events, spending, request-log detail, and dashboard aggregates are stored in PostgreSQL-backed APIs
- **Retained request history**: product-facing request logs, spending, usage snapshots, and dashboard aggregates remain in PostgreSQL for `/request-logs` and `/api/stats/*`
- **Audit logging**: optional request/response body capture with header redaction
- **Success-rate badges**: Terminal Target health based on recent request data
- **Startup bootstrap config**: strict plaintext `config.json` startup loading; external edits require a Prism restart after R2
- **Plain backup path**: disaster recovery uses `pg_dump` for PostgreSQL state plus a copy of the plaintext `config.json`
- **Caller client filtering**: request logs filter caller clients through User-Agent Client Rules using `client_rule_id`, while final target filtering uses `resolved_target_model_id`

### Architecture

- **Backend**: Go runtime service for the management API and runtime proxy surface
- **Frontend**: React 19 with TypeScript, Vite, TailwindCSS, and shadcn/ui
- **Database**: PostgreSQL with schema migrations managed by the backend runtime, including partitioned log tables, global retention jobs, and endpoint label snapshots
- **Deployment**: GHCR images or local runs via `./start.sh`

---

## Quick Start

### Prerequisites

- Go 1.26.4 toolchain
- Node.js 24+
- pnpm
- Docker with Docker Compose
- Git

### Local development

```bash
git clone https://github.com/coachpo/prism.git
cd prism
./start.sh full
./start.sh headless
```

Prism is a monorepo: the backend and frontend live in the same checkout under `backend/` and `frontend/`.

The launcher keeps frontend `5173` and PostgreSQL `15432` fixed, and it follows the selected bootstrap file's backend listener port. In this repo's checked-in `config.json`, that backend port is `18000`; freshly seeded startup configs default it to `8000`.

Log retention is configured from the Settings Global tab. Normal retention is global across all profiles and runs as durable `log_retention` jobs. It drops expired daily log partitions first, then cleans only the cutoff-overlapping boundary partition and vacuums that child with `VACUUM (ANALYZE, PROCESS_TOAST TRUE)`. Manual shrink tools such as `VACUUM FULL`, `CLUSTER`, and `pg_repack` are emergency operator actions only; `pg_repack` is not available in the default local `postgres:16-alpine` image.

For subproject-specific setup and commands, use:

- [`backend/README.md`](backend/README.md) for backend runtime, migrations, and verification
- [`frontend/README.md`](frontend/README.md) for frontend-only dev, build, and lint flows

### Docker Compose

The root `docker-compose.yml` is the default local/self-hosted deployment bundle. It builds one Prism application image from the root `Dockerfile`, runs PostgreSQL as a separate internal service, publishes only the public Prism HTTP port, and persists both PostgreSQL data and the Prism bootstrap config in named volumes.

```bash
docker compose up --build
docker compose up -d --build
BUILD_FRONTEND=false docker compose up --build

docker compose down
docker compose down -v
```

`docker compose down` stops the stack while preserving the `prism_postgres_data` and `prism_config` volumes. `docker compose down -v` intentionally deletes both local volumes, including the PostgreSQL database and `/app/config/config.json` bootstrap file.

Compose defaults to `http://localhost:8080`. The `prism` service mounts `prism_config` at `/app/config`, sets `PRISM_CONFIG_PATH=/app/config/config.json`, and sets `DATABASE_URL` to `postgres://prism:prism@postgres:5432/prism?sslmode=disable`. The `postgres` service uses `postgres:16-alpine`, stores data in `prism_postgres_data`, has a `pg_isready` healthcheck, and does not publish port `5432` to the host.

Common `.env` overrides are `PRISM_PUBLIC_PORT`, `PRISM_NGINX_PORT`, `PRISM_BACKEND_UPSTREAM_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `BUILD_FRONTEND`, `VITE_API_BASE`, `VITE_GIT_RUN_NUMBER`, and `VITE_GIT_REVISION`. Leave `VITE_API_BASE` unset for same-origin production builds. If the persisted bootstrap file changes the backend listener port away from the fresh-seed default `8000`, set `PRISM_BACKEND_UPSTREAM_PORT` to the same value before restarting the container.

### Docker (single image)

The root single-image build is separate from the existing backend-only and frontend-only images. It contains the Go `prism-backend` binary, backend migrations and version file, optional built React static assets, Nginx for static serving and reverse proxying, and a signal-aware launcher for both backend and Nginx. It does not run `frontend/server.mjs`, does not run the Vite dev server, and does not include PostgreSQL.

```bash
docker build -t prism-single .
docker build --build-arg BUILD_FRONTEND=false -t prism-single-backend-only .
```

The image exposes only public port `8080` by default. Nginx serves `/` from the built frontend, falls back to `/index.html` for frontend route refreshes, and proxies `/health`, `/api`, `/v1`, and `/v1beta` to the private backend upstream. `BUILD_FRONTEND=false` skips the React build and serves a minimal fallback page at `/` while keeping backend proxy paths available.

For direct Docker runs with an external PostgreSQL container:

```bash
docker network create prism-net

docker run -d \
  --name prism-postgres \
  --network prism-net \
  -e POSTGRES_DB=prism \
  -e POSTGRES_USER=prism \
  -e POSTGRES_PASSWORD=prism \
  -v prism_postgres_data:/var/lib/postgresql/data \
  postgres:16-alpine

docker run --rm \
  --network prism-net \
  -p 8080:8080 \
  -v prism_config:/app/config \
  -e PRISM_CONFIG_PATH=/app/config/config.json \
  -e DATABASE_URL="postgres://prism:prism@prism-postgres:5432/prism?sslmode=disable" \
  prism-single
```

Startup uses a plaintext bootstrap file owned by `PRISM_CONFIG_PATH`. Freshly seeded files use backend-owned canonical defaults, including `0.0.0.0:8000`, CORS for local development, `runtime.transport.requestTimeout` as `"300s"`, and `runtime.sideEffects.attemptTimeout` as `"10s"`. Existing valid bootstrap files are preserved, even when they contain older values. If a persisted file already contains a different database URL or backend port, edit `config.json` directly and restart Prism, or reset the config volume intentionally.

The application image runs as `prism:prism`, UID/GID `1000:1000`. Named Compose volumes work out of the box. If you use a host bind mount for `/app/config`, make the host directory writable by UID/GID `1000:1000`, for example with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.

Assumptions and limitations: PostgreSQL is not bundled inside the application image, the default Compose password is only a local/self-hosted default meant to be overridden for real deployments, and a persisted bootstrap file remains Prism's source of truth for backend listener and database settings after first startup.

### Docker (separate images)

The existing `backend/Dockerfile`, `frontend/Dockerfile`, and GHCR workflow remain available for deployments that want separate backend and frontend containers with their own reverse proxy. In that mode, the backend image serves the Go API on its bootstrap-configured port, and the frontend image serves the built `dist/` output with `frontend/server.mjs` on port `3000`.

```bash
docker pull ghcr.io/coachpo/prism-backend:latest
docker pull ghcr.io/coachpo/prism-frontend:latest
```

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [API Specification](docs/API_SPEC.md)
- [Data Model](docs/DATA_MODEL.md)
- [Workflows](docs/WORKFLOWS.md)
- [Requests Page Notes](docs/REQUESTS_PAGE.md)
- [PRD](docs/PRD.md)

The checked-in `docs/` tree is reserved for durable reference material and archive notes. Local scratch plans and run evidence are kept outside `docs/` under ignored `artifacts/`. Endpoint snapshot and request-log filter details live in the active docs rather than only in archived smoke evidence.

---

## Development

### Version management

`backend/` and `frontend/` keep their version metadata inside this monorepo checkout. `backend/VERSION` is the backend runtime version surface, and the frontend keeps its runtime-visible version in `frontend/package.json` alongside `frontend/VERSION`. Root `VERSION` stays aligned with those surfaces during releases.

### Root release flow

The root-local `./release.sh` helper is the monorepo release gate for Prism. It accepts either `patch|minor|major` or an exact `X.Y.Z` version like `0.2.4`, updates `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json`, runs a backend version-metadata check plus the frontend build, then commits, tags, and pushes one root-repo release. Live releases require a clean, current `main` checkout; `--dry-run` previews the flow without modifying files and skips those branch-state guards so the release plan can be reviewed from a feature branch.

```bash
./release.sh patch --dry-run
./release.sh 0.2.4 --yes
```

The helper creates one root `vX.Y.Z` tag. That tag triggers `.github/workflows/docker-images.yml` to publish the backend and frontend images from the monorepo checkout.

### Backend

```bash
./start.sh headless

cd backend
go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
go build ./cmd/prism-backend
```

`./start.sh` launches the live Go backend through the checked-in service, while direct backend work uses the Go entrypoint and Go test packages under `backend/`.

### Frontend

```bash
cd frontend
pnpm install
pnpm run dev
pnpm run build
pnpm run lint
```

The frontend build injects `VITE_APP_VERSION` from `frontend/package.json` plus `VITE_GIT_RUN_NUMBER` and `VITE_GIT_REVISION` for the visible app-version label.

### Security scanning policy

The CI workflow treats backend and frontend dependency scanners as blocking release-safety gates:

- Backend vulnerability scanning runs plain `govulncheck ./...` from `backend/`; the blocking step intentionally avoids machine-readable output modes that can exit successfully despite findings.
- Frontend production dependency scanning runs `pnpm audit --prod --audit-level=high` from `frontend/` after `pnpm install --frozen-lockfile`; registry failures are not ignored or failed open.

Container vulnerability scanning is evidence-only in this wave. CI locally builds `prism-backend:ci` and `prism-frontend:ci`, scans those exact local tags with pinned Trivy using `--severity HIGH,CRITICAL --ignore-unfixed --exit-code 0`, uploads the reports as artifacts, and writes an explicit non-blocking status summary. Container scans are not gating yet because Trivy advisory database and network drift can make image-vulnerability gating unstable even when the scanned source revision is unchanged. Release image publishing remains owned by `.github/workflows/docker-images.yml`; scanner evidence does not scan mutable remote `latest` tags.

---

## Configuration

### Environment variables

Use [`frontend/.env.example`](frontend/.env.example) as the frontend sample.

Plaintext bootstrap startup uses the startup JSON as the steady-state source, with only bootstrap-critical environment exceptions:

- `PRISM_CONFIG_PATH` points at a plaintext bootstrap file such as `config.json`
- `DATABASE_URL` is optional seed/startup input. Backend-native seeds default to `postgres://prism:prism@localhost:5432/prism?sslmode=disable`; `./start.sh` sets it to the local launcher PostgreSQL DSN on host port `15432`.

The plaintext startup file is edited outside the dashboard. R2 removed the Startup settings tab and bootstrap management API, so external `config.json` edits always require a Prism restart before they affect the running process. `runtime.secretEncryptionKey`, JWT signing keys, database URLs, and telemetry authorization headers remain raw file secrets.

That bootstrap file owns startup values directly. Runtime buffering is automatic and not user-configurable. If an encrypted bootstrap file is still present, replace it before booting.

The top-level startup `telemetry` section is still parsed so existing live `config.json` files keep loading, but Prism no longer starts metrics or tracing exporters from it. Prism does not expose a backend-local `/metrics` scrape endpoint; retained request-history, spending, usage, and dashboard aggregate APIs remain product-facing PostgreSQL-backed APIs under `/api/stats/*`.

Plaintext bootstrap files must include `runtime.transport.requestTimeout`, seeded as `"300s"`, and `runtime.sideEffects.attemptTimeout`, seeded as `"10s"`. Missing either required field fails startup validation by design. `runtime.transport.requestTimeout` remains the whole-request upstream provider HTTP timeout. `runtime.sideEffects.attemptTimeout` is the per-attempt background side-effect enqueue budget. Both values now change only after editing `config.json` and restarting Prism.

Mail delivery was removed. Existing bootstrap files with a `mail` block still parse for startup compatibility, and seeded local configs keep the explicit disabled shape:

```json
{
  "mail": {
    "enabled": false
  }
}
```

Other configuration notes:

- `./start.sh` reads the root `.env`, provisions PostgreSQL from `backend/docker-compose.yml`, keeps frontend `5173` and PostgreSQL `15432`, follows the selected bootstrap file's backend port, and defaults `PRISM_CONFIG_PATH` to the repo-local `config.json`
- Launcher startup only supports the local bootstrap contract: backend host stays local, backend port comes from the selected bootstrap file's `server.port`, and the bootstrap config resolves to the local PostgreSQL DSN `postgres://prism:prism@localhost:15432/prism?sslmode=disable`
- If you set `PRISM_CONFIG_PATH` in `.env`, `./start.sh` resolves relative paths from the repo root before launching the backend
- Direct backend runs should prefer an absolute `PRISM_CONFIG_PATH`
- Frontend build/runtime metadata uses `VITE_API_BASE`, `VITE_GIT_RUN_NUMBER`, and `VITE_GIT_REVISION`
- `./start.sh full` serves the browser through the launcher origin, with Vite proxying management traffic and supported runtime operation traffic to the backend so local browser traffic stays same-origin
- Standalone frontend development can still point at a remote backend with explicit `VITE_API_BASE`

If you compose a root `.env` for `./start.sh`, keep `PRISM_CONFIG_PATH` unset to use the repo-local `config.json`, or point it at another plaintext bootstrap file. The launcher seeds that file only when it is missing, using backend-owned canonical defaults plus the launcher-provided local PostgreSQL DSN on host port `15432`; fresh seeds still default backend port to `8000`. It does not rewrite an existing valid bootstrap file. Prism does not watch external edits to this file. After R2, every external `config.json` edit requires a Prism restart. To force a fresh seed, stop Prism, remove or relocate the bootstrap file, then restart. Disaster recovery should back up PostgreSQL with `pg_dump` and copy the selected plaintext `config.json`.

When `VITE_API_BASE` is unset, frontend requests stay same-origin. Local `./start.sh full` keeps management requests and supported runtime operation requests on the launcher origin through Vite proxying; standalone frontend workflows can still set `VITE_API_BASE` explicitly.

### Database

Prism uses PostgreSQL with Go-backend-managed migrations applied automatically on startup. Development contract changes are clean cut: incompatible local data should be reset and recreated, with no backfill path promised for old pricing, token, or Ban Policy semantics.

Load-balance strategy defaults are created explicitly from the Loadbalance Strategies page for frozen Default profile id=1 as explicit Ban Policy strategies. Retry-cycle exhaustion uses `cycle_retry_attempts >= cycle_retry_attempt_limit`; Ban Policy bans use `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; current-state views stay scoped to the model-private connection while loadbalance events keep policy threshold snapshots for history:

- `Default single routing`
- `Default fill-first routing`
- `Default round-robin routing`

---

## Security considerations

Prism is designed for trusted local or LAN deployments:

- Optional operator auth for `/api/*` and optional proxy API key enforcement for supported runtime operations under `/v1` and `/v1beta`
- Endpoint API keys encrypted at rest in PostgreSQL
- Operator-login lockout after repeated failures; no general-purpose rate limiting or abuse protection

Do not expose Prism directly to the public internet. Use a reverse proxy with authentication if remote access is needed.
