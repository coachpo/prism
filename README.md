# Prism

> **Status**: active development at v1.1.7 — self-hosted on a home LAN, no external users, PostgreSQL-backed, with development and deployment convenience prioritized over data-security hardening. Two instances are running. [STATUS.md](STATUS.md) is authoritative for lifecycle, deployment, retained data, and compatibility.

Prism is a self-hosted gateway that sits between your tools and LLM providers, giving you one endpoint, one place to manage API keys, and a web dashboard to see what every request cost. It is built for developers and power users who juggle several providers and want failover, routing, and usage tracking without running heavy infrastructure.

A single Go binary, a React dashboard, and PostgreSQL are all it needs.

## Features

- Speaks three API styles through one gateway: OpenAI (chat completions, responses, models), Anthropic (messages), and Gemini (generateContent), streaming included.
- Routes by model name: a direct-entry model ID resolves through an ordered list of targets, where a *Terminal Target* is the final binding of a model to a specific provider endpoint. Models with `direct_request_enabled=false` remain valid recursive Model Targets but are not client-addressable. Swap or chain providers behind a stable model name.
- Load-balances across endpoints with `single`, `fill-first`, or `round-robin` strategies, with automatic retries.
- Applies *ban policies*: a failing Terminal Target is excluded temporarily or until reset, according to its configured strategy.
- Records request logs, token usage, latency, and spending in PostgreSQL, with dashboard analysis by entry request, final target, or routing attempt.
- Prices each request from reusable pricing templates you define per provider, with optional import from the [models.dev](https://models.dev) catalog: model metadata on the model detail page plus one-click source-linked price templates assigned atomically to a Terminal Target.
- Exports Pi 0.84.3 client configuration from `/route/models/export`, using independent pi.dev bindings for client templates and Prism routing/pricing facts. Review candidates and warnings, then copy or download `prism-pi-models.json`; [the product specification](docs/product.md#420-client-model-configuration-export-pi-0843) describes the workflow.
- Protects access with optional operator login for the dashboard and optional API keys for proxy callers; provider keys are encrypted at rest.
- Ships as one Docker image plus PostgreSQL.

## Data attribution

Model catalog metadata and catalog prices are sourced from [models.dev](https://models.dev), fetched read-only at operator request from its fixed official endpoint (`https://models.dev/api.json`). models.dev data is licensed under the MIT License (Copyright (c) 2025 models.dev); Prism stores only the metadata fields an operator explicitly binds or imports and never redistributes the catalog itself. Catalog lookups and metadata bindings stay on the management path and do not determine routing or capability. Accepted price imports become ordinary Prism pricing templates.

## Quick start

### Docker Compose (recommended)

```bash
git clone https://github.com/coachpo/prism.git
cd prism
docker compose up -d --build
```

Open <http://localhost:8080>. Compose builds the app image, runs PostgreSQL 16 next to it, and keeps both the database and the config file in named volumes. `docker compose down` preserves your data; `docker compose down -v` deletes it.

Useful `.env` overrides include `PRISM_PUBLIC_PORT`, `PRISM_DATABASE_PORT`, and `POSTGRES_PASSWORD`. Change the default database password for anything beyond local use.

### Single image

The root `Dockerfile` builds one image containing the Go backend, the built dashboard, and Nginx. PostgreSQL is not bundled; point the container at your own:

```bash
docker build -t prism .
docker run -p 8080:8080 \
  -v prism_config:/app/config \
  -e PRISM_CONFIG_PATH=/app/config/config.json \
  -e DATABASE_URL="postgres://prism:prism@your-postgres:5432/prism?sslmode=disable" \
  prism
```

The only prebuilt app image is `ghcr.io/coachpo/prism`, published for `linux/arm64`. It contains the Go backend, React dashboard, and Nginx; PostgreSQL remains a separate service. The container runs as UID/GID `1000:1000`; a host directory bind-mounted at `/app/config` must be writable by that user. Keep the complete config directory persistent across replacements.

### Local development

Requires Go 1.26.6, Node.js 24+, pnpm, and Docker. Backend and frontend live in this monorepo under `backend/` and `frontend/`.

```bash
./start.sh full      # backend + frontend dev server + PostgreSQL
./start.sh headless  # backend + PostgreSQL only
```

The launcher serves the frontend on port `5173`, runs PostgreSQL on `15432`, and defaults the backend to `8000`. It reads the root `.env`, preserves the selected bootstrap file, and uses its configured backend port. Full mode keeps browser requests same-origin through Vite. See [CONTRIBUTING.md](CONTRIBUTING.md) for source-tree workflows.

## Configuration

Prism boots from a plaintext JSON file (default `config.json`, path set by `PRISM_CONFIG_PATH`). It is seeded with defaults on first start and owns the listen address, database URL, timeouts, and secrets from then on. `DATABASE_URL` only seeds the database connection initially; afterwards the file is the source of truth. There is no config UI or hot reload — edit the file and restart Prism.

The backend requires one side-effect timeout field and seeds it automatically: `runtime.sideEffects.attemptTimeout` (`"10s"`, per-attempt background side-effect budget). The `runtime.transport` section was removed outright: the Go provider transport applies no connection or timeout limits, and a leftover `runtime.transport` block is rejected with a readable migration error.

Everything else — models, endpoints, load-balance strategies, pricing templates, proxy keys — is managed from the dashboard and stored in PostgreSQL. Schema migrations run automatically on startup.

To back up an instance, retain a consistent PostgreSQL dump and the matching `config.json`; see the [backup/restore procedure](.agents/skills/prism-backup-restore/SKILL.md) and the [retained-data policy](STATUS.md#data).

## Development

Development setup, tests, checks, builds, and the single-image release workflow are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

## Documentation

Start at the [Documentation Index](docs/README.md):

- [Status](STATUS.md) — lifecycle, deployment, users, data, and compatibility policy
- [Product Specification](docs/product.md) — product, scope, flows, and requirements
- [Architecture Overview](docs/architecture.md) — architecture, API reference, and data model reference
- [Development Rules](docs/development-rules.md) — project-specific implementation rules
- [Contributing Guide](CONTRIBUTING.md) — development workflow and shared principles

## Security

Prism is designed for trusted local or LAN deployments. Operator login and proxy API keys are available but there is no general rate limiting or abuse protection. Do not expose Prism directly to the public internet; put an authenticated reverse proxy in front if you need remote access.
