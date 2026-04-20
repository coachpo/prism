# Prism

**A lightweight, self-hosted LLM proxy gateway with routing, load balancing, and observability.**

Prism fronts multiple LLM API families and vendor-backed catalogs, letting you configure, route, and load-balance requests through a single gateway with a web-based management dashboard.

---

## Features

### Core capabilities

- **Multi-API-family support**: OpenAI on `/v1/*`, Anthropic on `/v1/messages*`, and Gemini on `/v1beta/models/*`
- **Proxy model routing**: public model IDs can forward to native targets while preserving their vendor metadata
- **Dual routing strategies**: reusable native-model strategies can be `legacy` or `adaptive`, with canonical defaults available through an explicit selected-profile action on the Loadbalance Strategies page
- **Streaming**: SSE responses pass through transparently

### Observability & management

- **Request telemetry**: latency, token usage, success rates, and error patterns
- **Audit logging**: optional request/response body capture with header redaction
- **Success-rate badges**: connection health based on recent request data
- **Config export/import**: current JSON config with profile-scoped replace-mode import

### Architecture

- **Backend**: Go runtime service for the management API and runtime proxy surface
- **Frontend**: React 19 with TypeScript, Vite, TailwindCSS, and shadcn/ui
- **Database**: PostgreSQL with schema migrations managed by the backend runtime
- **Deployment**: GHCR images or local runs via `./start.sh`

---

## Quick Start

### Prerequisites

- Go 1.26.2 toolchain
- Node.js 24+
- pnpm
- Git

### Local development

```bash
git clone https://github.com/coachpo/prism.git
cd prism
./start.sh full
./start.sh headless
```

Prism is a monorepo: the backend and frontend live in the same checkout under `backend/` and `frontend/`.

The launcher uses backend `18000`, frontend `15173`, and PostgreSQL `15432`.

For subproject-specific setup and commands, use:

- [`backend/README.md`](backend/README.md) for backend runtime, migrations, and verification
- [`frontend/README.md`](frontend/README.md) for frontend-only dev, build, and lint flows

### Docker Compose

> **Note**: there is no root full-stack `docker-compose.yml` in this repository. `backend/docker-compose.yml` only provisions PostgreSQL for local backend work. For a full deployment, create your own compose file based on the environment variables and service layout documented in this repository.

If you create a `docker-compose.yml`, the backend will be available at `http://localhost:8000` and the frontend at `http://localhost:3000`.

### Docker (manual)

```bash
docker pull ghcr.io/coachpo/prism-backend:latest
docker pull ghcr.io/coachpo/prism-frontend:latest

docker run -d \
  --name prism-backend \
  -p 8000:8000 \
  -e DATABASE_URL="postgres://prism:prism@<postgres-host>:5432/prism?sslmode=disable" \
  -e AUTH_JWT_SECRET="replace-with-a-long-random-jwt-secret" \
  -e SECRET_ENCRYPTION_KEY="replace-with-a-long-random-encryption-key" \
  -e CONFIG_BUNDLE_ENCRYPTION_KEY="replace-with-a-long-random-bundle-encryption-key" \
  ghcr.io/coachpo/prism-backend:latest

docker run -d \
  --name prism-frontend \
  -p 3000:3000 \
  ghcr.io/coachpo/prism-frontend:latest
```

The frontend image defaults to same-origin API calls. In production, put frontend and backend behind a reverse proxy and route `/` to frontend, and `/api`, `/v1`, and `/v1beta` to backend.

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [API Specification](docs/API_SPEC.md)
- [Data Model](docs/DATA_MODEL.md)
- [Requests Page Notes](docs/REQUESTS_PAGE.md)
- [Test Case Generation Methodology](docs/TEST_CASE_GENERATION_METHODOLOGY.md)
- [PRD](docs/PRD.md)
- [Smoke Test Plan](docs/SMOKE_TEST_PLAN.md)
- [Active plans](.sisyphus/plans/)

The checked-in `docs/` tree is reserved for durable reference material and archive notes. Active working plans are kept outside `docs/`.

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
go test ./tests/contract ./tests/integration ./tests/runtime
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

---

## Configuration

### Environment variables

Use [`.env.example`](.env.example) as a direct-run sample, not as a launcher-safe copy without edits.

- `./start.sh` loads the root `.env`, provisions PostgreSQL from `backend/docker-compose.yml`, and uses backend `18000`, frontend `15173`, and PostgreSQL `15432`
- Direct backend runs use environment variables such as `HOST`, `PORT`, `DATABASE_URL`, `CORS_ALLOWED_ORIGINS`, auth settings, WebAuthn settings, and SMTP settings from `.env.example`
- Frontend build/runtime metadata uses `VITE_API_BASE`, `VITE_GIT_RUN_NUMBER`, and `VITE_GIT_REVISION`
- `./start.sh full` serves the browser through the launcher origin, with Vite proxying `/api`, `/v1`, and `/v1beta` to the backend so local browser traffic stays same-origin
- Standalone frontend development can still point at a remote backend with explicit `VITE_API_BASE`
- `CONFIG_BUNDLE_ENCRYPTION_KEY` controls config profile/vendor bundle encryption; when unset, the backend falls back to `SECRET_ENCRYPTION_KEY`

If you copy `.env.example` to `.env` for `./start.sh`, update launcher-sensitive values such as `DATABASE_URL` and `WEBAUTHN_ORIGIN` to match the launcher ports, or leave them unset so `start.sh` can supply its own defaults.

When `VITE_API_BASE` is unset, frontend requests stay same-origin (`/api`, `/v1`, `/v1beta`). Local `./start.sh full` keeps browser traffic same-origin through the launcher origin and Vite proxying; standalone frontend workflows can still set `VITE_API_BASE` explicitly.

### Database

Prism uses PostgreSQL with Go-backend-managed migrations applied automatically on startup.

Load-balance strategy defaults are created explicitly from the Loadbalance Strategies page for the selected profile as:

- `Default legacy routing`
- `Default adaptive routing`

---

## Security considerations

Prism is designed for trusted local or LAN deployments:

- Optional operator auth for `/api/*` and optional proxy API key enforcement for `/v1/*` and `/v1beta/*`
- Endpoint API keys encrypted at rest in PostgreSQL
- No rate limiting or abuse protection

Do not expose Prism directly to the public internet. Use a reverse proxy with authentication if remote access is needed.
