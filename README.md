# Prism

> **Status**: self-hosted for personal home-LAN use. [Product Scope](CONTRIBUTING.md#product-scope) records the lifecycle and users, [Operations](docs/operations.md#deployed-instances) the deployments, and [CONTRIBUTING.md](CONTRIBUTING.md#retained-data-and-compatibility-policy) the retained-data and compatibility policy.

Prism is a self-hosted gateway that sits between your tools and LLM providers, giving you one endpoint, one place to manage API keys, and a web dashboard to see what every request cost. It is built for developers and power users who juggle several providers and want failover, routing, and usage tracking without running heavy infrastructure.

A single Go binary, a React dashboard, and PostgreSQL are all it needs.

## Features

- Speaks three API styles through one gateway: OpenAI (chat completions, responses, models), Anthropic (messages), and Gemini (generateContent), streaming included.
- Routes by model name: a direct-entry model ID resolves through an ordered list of targets, where a *Terminal Target* is the final binding of a model to a specific provider endpoint. Models with `direct_request_enabled=false` remain valid recursive Model Targets but are not client-addressable. Swap or chain providers behind a stable model name.
- Load-balances across endpoints with `single`, `fill-first`, or `round-robin` strategies, with automatic retries.
- Applies *ban policies*: a failing Terminal Target is excluded temporarily or until reset, according to its configured strategy.
- Records request logs, token usage, latency, and spending in PostgreSQL, with dashboard analysis by entry request, final target, or routing attempt.
- Prices each request from reusable pricing templates you define per provider, with optional import from the [models.dev](https://models.dev) catalog: model metadata on the model detail page plus one-click source-linked price templates assigned atomically to a Terminal Target.
- Exports Pi 0.84.3 and OpenCode 1.18.27 client configuration from `/route/models/export`. Pi uses its independent pi.dev bindings; OpenCode uses saved models.dev metadata and overrides. Both use Prism entry IDs, routing and current prices. Review final values and warnings, then copy or download the client file; [the product specification](docs/catalog-and-export.md) describes the workflow.
- Protects access with optional operator login for the dashboard and optional API keys for proxy callers; provider keys are encrypted at rest.
- Ships as one Docker image plus PostgreSQL.

## Client configuration export

Open **模型与服务 → 模型 → 导出客户端配置** to generate a Pi or OpenCode configuration for your Prism model entries. See the [client setup guide](docs/operations.md#client-configuration-export-quick-guide) for loading the generated file.

## Quick start

### Docker Compose (recommended)

Use Docker with the Compose plugin. Run from the repository root after cloning.

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

Run from the repository root with Go 1.26.6 or newer and Docker with Compose. Full mode also requires Node.js 24 or newer and pnpm 10.30.1. Backend and frontend live in this monorepo under `backend/` and `frontend/`.

```bash
./start.sh full      # backend + frontend dev server + PostgreSQL
./start.sh headless  # backend + PostgreSQL only
```

The launcher serves the frontend on port `5173`, runs PostgreSQL on `15432`, and defaults the backend to `8000`. It reads the root `.env`, preserves the selected bootstrap file, and uses its configured backend port. Full mode keeps browser requests same-origin through Vite. See [CONTRIBUTING.md](CONTRIBUTING.md) for source-tree workflows.

## Configuration

Prism boots from a plaintext JSON file (default `config.json`, path set by `PRISM_CONFIG_PATH`). It is seeded with defaults on first start and owns the listen address, database URL, timeouts, and secrets from then on. `DATABASE_URL` only seeds the database connection initially; afterwards the file is the source of truth. There is no config UI or hot reload — edit the file and restart Prism.

The backend requires one side-effect timeout field and seeds it automatically: `runtime.sideEffects.attemptTimeout` (`"10s"`, per-attempt background side-effect budget). The `runtime.transport` section was removed outright: the Go provider transport applies no connection or timeout limits, and a leftover `runtime.transport` block is rejected with a readable migration error.

Everything else — models, endpoints, load-balance strategies, pricing templates, proxy keys — is managed from the dashboard and stored in PostgreSQL. Schema migrations run automatically on startup.

See [Operations](docs/operations.md#startup-config-file) for the full bootstrap schema and [backup and recovery](docs/operations.md#back-up-upgrade-and-recover) for retained instances.

## Development

Development setup, tests, checks, builds, and the single-image release workflow are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

## Documentation

- [Architecture](ARCHITECTURE.md) covers module boundaries, data flow, and the API and persistence references.
- [Contributing](CONTRIBUTING.md) covers development setup, validation, release commands, and product scope.
- [Changelog](CHANGELOG.md) records tagged versions and user-visible changes.
- [Operations](docs/operations.md) covers configuration, inspection, backup, upgrades, and recovery.
- [Product scope](CONTRIBUTING.md#product-scope) describes the single-operator scope, requirements, and exclusions; the [retained-data policy](CONTRIBUTING.md#retained-data-and-compatibility-policy) records the compatibility commitments.
- The topic documents linked from [Architecture](ARCHITECTURE.md#product-data-and-operations-contracts) describe shipped behavior.
- [Architecture decisions](docs/adr/) record decisions with repository evidence.

## Security

Prism is designed for trusted local or LAN deployments. Operator login and proxy API keys are available but there is no general rate limiting or abuse protection. Do not expose Prism directly to the public internet; put an authenticated reverse proxy in front if you need remote access.
