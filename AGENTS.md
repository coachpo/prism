# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-28
**Commit:** a2c86c7
**Branch:** main

## OVERVIEW

Prism — self-hosted LLM proxy gateway with per-request costing. Routes requests to OpenAI, Anthropic, and Gemini through a single `/v1/*` endpoint with failover load balancing, streaming, audit logging, cost tracking, and a React dashboard. Monorepo with git submodules (backend + frontend).

## STRUCTURE

```
prism/
├── backend/              # FastAPI async API + proxy engine (git submodule: coachpo/prism-backend)
│   ├── app/
│   │   ├── main.py       # App factory, lifespan, CORS, 10 router mounts, Alembic migrations (213 lines)
│   │   ├── core/         # config.py (pydantic-settings) + database.py (async SQLAlchemy)
│   │   ├── models/       # 10 ORM models (477 lines): Profile, Provider, ModelConfig, Endpoint, Connection, RequestLog, UserSetting, EndpointFxRateSetting, HeaderBlocklistRule, AuditLog
│   │   ├── schemas/      # Pydantic request/response schemas (736 lines)
│   │   ├── routers/      # 10 API routers: profiles, providers, models, endpoints, connections, stats, audit, config, settings, proxy
│   │   └── services/     # proxy_service, loadbalancer, stats_service, audit_service, costing_service
│   └── tests/            # pytest + pytest-asyncio, 12 defect-driven regression test classes
├── frontend/             # React 19 SPA dashboard (git submodule: coachpo/prism-frontend)
│   └── src/
│       ├── pages/        # 8 pages: Dashboard, Models, ModelDetail, Endpoints, RequestLogs, Statistics, Audit, Settings
│       ├── components/   # 12 shared (8 top-level + 1 layout + 3 statistics) + 22 shadcn/ui primitives
│       ├── lib/          # api.ts, types.ts, utils.ts, costing.ts, configImportValidation.ts
│       ├── hooks/        # useEndpointNavigation
│       ├── context/      # ProfileContext
├── docs/                 # Architecture, API spec, data model, PRD, deployment, smoke tests
├── .github/workflows/    # docker-images.yml (GHCR build) + cleanup.yml (daily prune)
├── start.sh              # Unified dev launcher: ./start.sh full | headless
└── .env.example          # BACKEND_PORT, FRONTEND_PORT
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Proxy routing logic | `backend/app/routers/proxy.py` | Catch-all `/v1/{path}` + `/v1beta/{path}` (662 lines) |
| Load balancing | `backend/app/services/loadbalancer.py` | single + failover strategies (round_robin removed) |
| Failover recovery | `backend/app/services/loadbalancer.py` | In-memory `_recovery_state` dict, cooldown-based probing |
| Provider auth headers | `backend/app/services/proxy_service.py` | OpenAI=Bearer, Anthropic=x-api-key, Gemini=x-goog-api-key |
| Cost computation | `backend/app/services/costing_service.py` | 5 token types × prices, FX conversion, pricing snapshots |
| Spending reports | `backend/app/routers/stats.py` + `services/stats_service.py` | `/api/stats/spending` with 7 group-by modes |
| Costing settings | `backend/app/routers/settings.py` | `/api/settings/costing` — currency + FX rate mappings |
|| Add DB column | `backend/app/models/models.py` + new Alembic revision in `alembic/versions/` | Alembic is source of truth |
| Frontend API client | `frontend/src/lib/api.ts` | All backend calls via typed `request<T>()` helper (7 namespaces) |
| Frontend types | `frontend/src/lib/types.ts` | Must match backend Pydantic schemas (snake_case, 529 lines) |
| Frontend costing utils | `frontend/src/lib/costing.ts` | `formatMoneyMicros()`, `microsToDecimal()`, enum label formatters |
| Config import validation | `frontend/src/lib/configImportValidation.ts` | Zod schema for client-side config validation |
| Add frontend page | `frontend/src/App.tsx` + `pages/` + `AppLayout.tsx` nav link |
|| Connection management | `backend/app/routers/connections.py` | Model-scoped routing config, health checks, pricing |
|| Profile management | `backend/app/routers/profiles.py` | CRUD for profiles, activate/deactivate, soft delete (max 10) |
| Docker CI | `.github/workflows/docker-images.yml` | Builds linux/arm64 only, pushes to GHCR |
| Architecture docs | `docs/ARCHITECTURE.md` | Request flows, design decisions |

## ARCHITECTURE

```
Client → Prism /v1/* → resolve model (proxy→native) → select endpoint (failover strategy)
                                                       → forward to upstream
                                                       → compute costs (pricing + FX)
                                                       → log telemetry + costs
                                                       → audit (if enabled)
```

- Backend: Python 3.13+, FastAPI, async SQLAlchemy, asyncpg, httpx
- Frontend: React 19, TypeScript 5.9, Vite 7, TailwindCSS 4, shadcn/ui (new-york)
- Database: PostgreSQL (async with asyncpg driver, Alembic migrations)
- Providers: OpenAI, Anthropic, Gemini only (hardcoded, seeded on first run)
- Streaming: SSE pass-through with async generators
- Costing: Per-request cost computation with multi-currency FX support, stored as micros (1/1,000,000)

## COMMANDS

```bash
# Full stack (backend + frontend)
./start.sh full

# Backend only
./start.sh headless

# Backend manual
cd backend && ./venv/bin/python -m uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

# Frontend manual
cd frontend && pnpm run dev

# Backend tests
cd backend && ./venv/bin/python -m pytest tests/ -v

# Frontend lint
cd frontend && pnpm run lint

# Frontend build
cd frontend && pnpm run build
```

## CONVENTIONS

- Git submodules: `git submodule update --init --recursive` after clone
- Backend: async everywhere, `selectinload()` for eager loading, never lazy load
- Frontend: pnpm only (never npm/npx), `@/` import alias, no global state
- Types: backend Pydantic schemas are source of truth → frontend `types.ts` mirrors them in snake_case
- Migrations: Alembic migrations applied programmatically on startup via `run_migrations()`
- Docker: ARM64-only builds (`linux/arm64`), images on GHCR
- Security: trusted LAN only — no auth, wildcard CORS, plaintext API keys in PostgreSQL
- Costs stored as micros (int64) — `total_cost_micros / 1_000_000 = decimal amount`
- Config export/import version 1 — ref-only schema (`endpoint_ref`, `connection_ref`) with replace-mode import

## ANTI-PATTERNS

- Don't add providers beyond OpenAI/Anthropic/Gemini — hardcoded in backend seed + proxy_service
- Don't use request-scoped `db` session in StreamingResponse generators — use `AsyncSessionLocal()` directly
- Don't chain proxy aliases — exactly one redirect lookup
- Don't suppress type errors (`as any`, `@ts-ignore`)
- Don't use relative imports in frontend — always `@/` alias
- Don't use `npm`/`npx` — pnpm only
- Don't expose Prism to public internet — no auth layer
- Don't use `round_robin` LB strategy — removed, auto-migrated to `failover` on startup
- Don't create connections for proxy models — blocked at connection creation and config import
- Don't store costs as floats — always micros (int64) to avoid precision loss

## NOTES

 `backend/` and `frontend/` are separate git repos (submodules) — commits must be made inside each submodule
 Failover recovery state is in-memory — resets on backend restart
 Audit bodies truncated at 64KB with `[TRUNCATED]` marker
 Header blocklist rules (system + user-defined) filter proxy/CDN/tracing headers before forwarding
 No frontend tests — lint only (`pnpm run lint`)
 Backend test deps (pytest, pytest-asyncio) installed in venv but not in requirements.txt
 | Docker deployment via manual container commands (pull from GHCR, run with volumes) — no docker-compose.yml in repo root
 Docker images are ARM64-only (`linux/arm64`) — no amd64 support
 Frontend production uses custom Node.js server (server.mjs) on port 3000 instead of nginx/caddy
 Failover status codes: 403, 429, 500, 502, 503, 529 — other errors returned immediately without failover
 Pricing snapshots stored in request_logs for audit trail (unit, prices, policy, config version)