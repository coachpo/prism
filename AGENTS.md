# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-22
**Commit:** 804dea8
**Branch:** main

## OVERVIEW

Prism — self-hosted LLM proxy gateway. Routes requests to OpenAI, Anthropic, and Gemini through a single `/v1/*` endpoint with load balancing, failover, streaming, audit logging, and a React dashboard. Monorepo with git submodules (backend + frontend).

## STRUCTURE

```
prism/
├── backend/              # FastAPI async API + proxy engine (git submodule: coachpo/prism-backend)
│   ├── app/
│   │   ├── main.py       # App factory, lifespan, CORS, 7 router mounts, manual migrations
│   │   ├── core/         # config.py (pydantic-settings) + database.py (async SQLAlchemy)
│   │   ├── models/       # 6 ORM models: Provider, ModelConfig, Endpoint, RequestLog, AuditLog, HeaderBlocklistRule
│   │   ├── schemas/      # Pydantic request/response schemas (434 lines)
│   │   ├── routers/      # 7 API routers: providers, models, endpoints, stats, audit, config, proxy
│   │   └── services/     # proxy_service, loadbalancer, stats_service, audit_service
│   └── tests/            # pytest + pytest-asyncio, defect-driven regression tests
├── frontend/             # React 19 SPA dashboard (git submodule: coachpo/prism-frontend)
│   └── src/
│       ├── pages/        # 6 pages: Dashboard, Models, ModelDetail, Statistics, Audit, Settings
│       ├── components/   # 8 shared + 22 shadcn/ui primitives
│       ├── lib/          # api.ts (typed fetch), types.ts (backend mirrors), utils.ts
│       └── hooks/        # useEndpointNavigation
├── docs/                 # Architecture, API spec, data model, PRD, deployment, smoke tests
├── .github/workflows/    # docker-images.yml (GHCR build) + cleanup.yml (daily prune)
├── start.sh              # Unified dev launcher: ./start.sh full | headless
└── .env.example          # BACKEND_PORT, FRONTEND_PORT
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Proxy routing logic | `backend/app/routers/proxy.py` | Core catch-all `/v1/{path}` + `/v1beta/{path}` (578 lines) |
| Load balancing | `backend/app/services/loadbalancer.py` | single, round_robin, failover strategies |
| Provider auth headers | `backend/app/services/proxy_service.py` | OpenAI=Bearer, Anthropic=x-api-key, Gemini=x-goog-api-key |
| Add DB column | `backend/app/models/models.py` + `main.py` `_add_missing_columns()` | No Alembic — manual ALTER TABLE |
| Frontend API client | `frontend/src/lib/api.ts` | All backend calls via typed `request<T>()` helper |
| Frontend types | `frontend/src/lib/types.ts` | Must match backend Pydantic schemas (snake_case) |
| Add frontend page | `frontend/src/App.tsx` + `pages/` + `AppLayout.tsx` nav link |
| Docker CI | `.github/workflows/docker-images.yml` | Builds linux/arm64 only, pushes to GHCR |
| Architecture docs | `docs/ARCHITECTURE.md` | Request flows, design decisions |
| Smoke test plan | `docs/SMOKE_TEST_PLAN.md` | Manual test scenarios |

## ARCHITECTURE

```
Client → Prism /v1/* → resolve model (proxy→native) → select endpoint (LB strategy) → forward to upstream
                                                                                      → log telemetry
                                                                                      → audit (if enabled)
```

- Backend: Python 3.11+, FastAPI, async SQLAlchemy, aiosqlite, httpx
- Frontend: React 19, TypeScript 5.9, Vite 7, TailwindCSS 4, shadcn/ui (new-york)
- Database: SQLite (single-file, auto-created at `backend/data/gateway.db`)
- Providers: OpenAI, Anthropic, Gemini only (hardcoded, seeded on first run)
- Streaming: SSE pass-through with async generators

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
- No Alembic: schema migrations via `_add_missing_columns()` in `main.py`
- Docker: ARM64-only builds (`linux/arm64`), images on GHCR
- Security: trusted LAN only — no auth, wildcard CORS, plaintext API keys in SQLite

## ANTI-PATTERNS

- Don't add providers beyond OpenAI/Anthropic/Gemini — hardcoded in backend seed + proxy_service
- Don't use request-scoped `db` session in StreamingResponse generators — use `AsyncSessionLocal()` directly
- Don't chain proxy aliases — exactly one redirect lookup
- Don't suppress type errors (`as any`, `@ts-ignore`)
- Don't use relative imports in frontend — always `@/` alias
- Don't use `npm`/`npx` — pnpm only
- Don't expose Prism to public internet — no auth layer

## NOTES

- `backend/` and `frontend/` are separate git repos (submodules) — commits must be made inside each submodule
- Round-robin LB state is in-memory — resets on backend restart
- Audit bodies truncated at 64KB with `[TRUNCATED]` marker
- Header blocklist rules (system + user-defined) filter proxy/CDN/tracing headers before forwarding
- No frontend tests — lint only (`pnpm run lint`)
- Backend test deps (pytest, pytest-asyncio) installed in venv but not in requirements.txt