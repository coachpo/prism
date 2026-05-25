# Prism

**A lightweight, self-hosted LLM proxy gateway with routing, load balancing, and observability.**

Prism fronts multiple LLM API families and vendor-backed catalogs, letting you configure, route, and load-balance requests through a single gateway with a web-based management dashboard.

---

## Features

### Core capabilities

- **Operation-registered runtime support**: allowed routes are `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/images/generations`, `POST /v1/images/edits`, `POST /v1/messages`, `POST /v1/messages/count_tokens`, `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent`, and `POST /v1beta/models/{model}:countTokens`
- **Not a full vendor API clone**: unsupported vendor routes are rejected before provider transport, telemetry, audit, or feedback side effects
- **Proxy model routing**: public model IDs can forward to native targets while preserving their vendor metadata
- **Dual routing strategies**: reusable native-model strategies can be `legacy` or `adaptive`, with canonical defaults available through an explicit selected-profile action on the Loadbalance Strategies page
- **Streaming**: operation hooks handle SSE responses for supported streaming routes

### Observability & management

- **Request telemetry**: latency, token usage, success rates, and error patterns
- **Audit logging**: optional request/response body capture with header redaction
- **Success-rate badges**: connection health based on recent request data
- **Startup bootstrap config**: strict plaintext `config.json` management through `/settings#startup`, with hot apply for eligible runtime fields, masked secret metadata, and explicit confirmation for dangerous structural changes
- **Config export/import**: PostgreSQL-backed profile and vendor bundles with profile-scoped replace-mode import
- **CLIProxyAPI sidecars**: global sidecar registrations, live auth-files, provider inventory, connection testing, manual sync, and direct auth-file mutations through `/sidecars` and `/api/sidecars/*`

### Architecture

- **Backend**: Go runtime service for the management API and runtime proxy surface
- **Frontend**: React 19 with TypeScript, Vite, TailwindCSS, and shadcn/ui
- **Database**: PostgreSQL with schema migrations managed by the backend runtime, including partitioned log tables, global retention jobs, and sidecar control-plane tables
- **Deployment**: GHCR images or local runs via `./start.sh`

---

## Quick Start

### Prerequisites

- Go 1.26.2 toolchain
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

> **Note**: there is no root full-stack `docker-compose.yml` in this repository. `backend/docker-compose.yml` only provisions PostgreSQL for local backend work. For a full deployment, create your own compose file based on the bootstrap-config mount, minimal external bootstrap env contract, and service layout documented in this repository.

If you create a `docker-compose.yml`, the frontend will commonly be published at `http://localhost:3000`, and the backend will listen on whatever port your chosen bootstrap file configures (`http://localhost:8000` is the fresh-seed default).

### Docker (manual)

```bash
docker pull ghcr.io/coachpo/prism-backend:latest
docker pull ghcr.io/coachpo/prism-frontend:latest

PRISM_CONFIG_DIR="/absolute/secure/path/prism-config"
sudo mkdir -p "$PRISM_CONFIG_DIR"
sudo chown -R 1000:1000 "$PRISM_CONFIG_DIR"
sudo chmod 0700 "$PRISM_CONFIG_DIR"

docker run -d \
  --name prism-backend \
  -p 8000:8000 \
  -v "$PRISM_CONFIG_DIR:/app/config:rw" \
  -e PRISM_CONFIG_PATH="/app/config/config.json" \
  ghcr.io/coachpo/prism-backend:latest

docker run -d \
  --name prism-frontend \
  -p 3000:3000 \
  ghcr.io/coachpo/prism-frontend:latest
```

Startup uses a plaintext bootstrap file owned by `PRISM_CONFIG_PATH`, with `config.json` as the default root launcher target. The only optional startup env vars are `PRISM_CONFIG_PATH` and `DATABASE_URL`; the default database URL is `postgres://prism:prism@localhost:15432/prism?sslmode=disable`. If an encrypted bootstrap file is still on disk, replace it before booting. Freshly seeded files use backend-owned canonical defaults, including `0.0.0.0:8000`, CORS for `http://localhost:5173`, `runtime.transport.requestTimeout` as `"300s"`, and `runtime.sideEffects.attemptTimeout` as `"10s"`. Existing valid bootstrap files are preserved, even when they contain older values. To reset to the current defaults, stop Prism, remove or relocate the bootstrap file, then restart so the launcher or backend can seed a missing file.

The backend image runs as `prism:prism`, UID/GID `1000:1000`. The bind-mounted directory that contains `PRISM_CONFIG_PATH`, such as `/absolute/secure/path/prism-config` for `/app/config/config.json`, must be writable by UID/GID `1000:1000` so the Startup API can create and replace the bootstrap file. For an existing root-owned bind mount, remediate the host directory once with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.

The frontend image defaults to same-origin API calls. In production, put frontend and backend behind a reverse proxy and route `/` to frontend, `/api` to backend management handlers, and the supported runtime operation paths under `/v1` and `/v1beta` to backend runtime handlers.

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [API Specification](docs/API_SPEC.md)
- [Data Model](docs/DATA_MODEL.md)
- [Workflows](docs/WORKFLOWS.md)
- [Requests Page Notes](docs/REQUESTS_PAGE.md)
- [Test Case Generation Methodology](docs/TEST_CASE_GENERATION_METHODOLOGY.md)
- [PRD](docs/PRD.md)
- [Smoke Test Plan](docs/SMOKE_TEST_PLAN.md)
- [Active plans](.sisyphus/plans/)

The checked-in `docs/` tree is reserved for durable reference material and archive notes. Active working plans are kept outside `docs/`. Sidecar workflow, API, and data-model details live in the active docs rather than only in archived smoke evidence.

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

---

## Configuration

### Environment variables

Use [`frontend/.env.example`](frontend/.env.example) as the frontend sample.

Plaintext bootstrap startup uses a single steady-state external input:

- `PRISM_CONFIG_PATH` points at a plaintext bootstrap file such as `config.json`
- `DATABASE_URL` is optional and defaults to `postgres://prism:prism@localhost:15432/prism?sslmode=disable`

The Startup tab at `/settings#startup` manages that plaintext file directly. GET returns masked metadata only, field-level apply capabilities, and pending apply state only when the file differs from the live applied baseline. PUT applies explicit preserve or replace secret actions with expected revision and etag checks, writes the file, and immediately publishes eligible hot fields. Dangerous host, port, database, JWT signing key, and bundle key changes require confirmation tokens. `runtime.secretEncryptionKey` is preserve only in v1, and redacted placeholders are not persisted.

That bootstrap file owns startup values directly. Hot-eligible fields include CORS origins, auth TTL and cookie metadata, mail and SMTP settings, runtime buffering and transport settings, and M2/M3 management admission limits. Listener host and port, database URL and pool budgets, runtime side-effects attempt timeout, runtime secret encryption key, JWT signing key, and bundle key changes still require restart. If an encrypted bootstrap file is still present, replace it before booting.

Plaintext bootstrap files must include `runtime.transport.requestTimeout`, seeded as `"300s"`, and `runtime.sideEffects.attemptTimeout`, seeded as `"10s"`. Missing either required field fails startup validation by design. `runtime.transport.requestTimeout` remains the whole-request upstream provider HTTP timeout and is hot-applicable through the Startup tab or API. `runtime.sideEffects.attemptTimeout` is the per-attempt background side-effect enqueue budget, is restart-required, and is not hot-applied.

Auth email delivery is disabled by default. Existing bootstrap files with no `mail` block still start and use no-op delivery with no SMTP network activity. Seeded local configs include the explicit disabled shape:

```json
{
  "mail": {
    "enabled": false
  }
}
```

To send password-reset and recovery-email verification messages, set `mail.enabled=true`, provide `mail.from`, and add SMTP settings. Prism validates enabled SMTP at startup, so missing or invalid enabled settings fail startup instead of falling back to no-op delivery. Supported SMTP modes are `starttls_required`, `implicit_tls`, and `plaintext_local_only`; `plaintext_local_only` is accepted only for localhost or loopback hosts, and auth over non-local plaintext is forbidden.

```json
{
  "mail": {
    "enabled": true,
    "from": "Prism <noreply@example.com>",
    "replyTo": "support@example.com",
    "smtp": {
      "host": "smtp.example.com",
      "port": 587,
      "mode": "starttls_required",
      "ehloHostname": "prism.example.com",
      "auth": "plain",
      "username": "smtp-user",
      "passwordFile": "/run/secrets/prism-smtp-password",
      "timeout": "15s",
      "tlsServerName": "smtp.example.com"
    }
  }
}
```

`mail.smtp.auth` may be `none` or `plain`. When it is `plain`, set `username` plus exactly one password source: `mail.smtp.password` or `mail.smtp.passwordFile`. Prefer `passwordFile` for deployed systems. If you store `mail.smtp.password` in the bootstrap file, the safe bootstrap API never returns the plaintext value; it reports secret metadata only and updates it through preserve or replace secret actions. SMTP changes apply immediately when saved through the Startup tab or API PUT and hot publish succeeds. To roll back delivery, remove `mail` or set `mail.enabled=false` through the Startup tab or API PUT; direct external file edits are not watched automatically.

Other configuration notes:

- `./start.sh` reads the root `.env`, provisions PostgreSQL from `backend/docker-compose.yml`, keeps frontend `5173` and PostgreSQL `15432`, follows the selected bootstrap file's backend port, and defaults `PRISM_CONFIG_PATH` to the repo-local `config.json`
- Launcher startup only supports the local bootstrap contract: backend host stays local, backend port comes from the selected bootstrap file's `server.port`, and the bootstrap config resolves to the local PostgreSQL DSN `postgres://prism:prism@localhost:15432/prism?sslmode=disable`
- If you set `PRISM_CONFIG_PATH` in `.env`, `./start.sh` resolves relative paths from the repo root before launching the backend
- Direct backend runs should prefer an absolute `PRISM_CONFIG_PATH`
- Frontend build/runtime metadata uses `VITE_API_BASE`, `VITE_GIT_RUN_NUMBER`, and `VITE_GIT_REVISION`
- `./start.sh full` serves the browser through the launcher origin, with Vite proxying management traffic and supported runtime operation traffic to the backend so local browser traffic stays same-origin
- Standalone frontend development can still point at a remote backend with explicit `VITE_API_BASE`

If you compose a root `.env` for `./start.sh`, keep `PRISM_CONFIG_PATH` unset to use the repo-local `config.json`, or point it at another plaintext bootstrap file. The launcher seeds that file only when it is missing, using backend-owned canonical defaults, including backend port `8000`, and the local PostgreSQL DSN. It does not rewrite an existing valid bootstrap file. Prism does not watch external edits to this file. Use `/settings#startup` or `PUT /api/config/bootstrap` when a hot-eligible edit should reach the running process. To force a fresh seed, stop Prism, remove or relocate the bootstrap file, then restart. Profile backup/restore, vendor catalog export/import, and other settings-page state flows remain PostgreSQL-backed state transport and are not loaded from `config.json`.

When `VITE_API_BASE` is unset, frontend requests stay same-origin. Local `./start.sh full` keeps management requests and supported runtime operation requests on the launcher origin through Vite proxying; standalone frontend workflows can still set `VITE_API_BASE` explicitly.

### Database

Prism uses PostgreSQL with Go-backend-managed migrations applied automatically on startup.

Load-balance strategy defaults are created explicitly from the Loadbalance Strategies page for the selected profile as:

- `Default legacy routing` (`fill-first`)
- `Default adaptive routing`

---

## Security considerations

Prism is designed for trusted local or LAN deployments:

- Optional operator auth for `/api/*` and optional proxy API key enforcement for supported runtime operations under `/v1` and `/v1beta`
- Endpoint API keys encrypted at rest in PostgreSQL
- No rate limiting or abuse protection

Do not expose Prism directly to the public internet. Use a reverse proxy with authentication if remote access is needed.
