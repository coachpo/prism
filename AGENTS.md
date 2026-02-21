# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-21
**Commit:** bc219ad
**Branch:** main

## OVERVIEW

Prism — a lightweight reverse proxy that routes LLM API requests to OpenAI, Anthropic, and Gemini with load balancing, failover, model aliasing, request telemetry, and audit logging. Python/FastAPI backend + React/TypeScript frontend.

## STRUCTURE

```
prism/
├── backend/          # FastAPI API + proxy engine (git submodule → coachpo/prism-backend)
├── frontend/         # React SPA dashboard (git submodule → coachpo/prism-frontend)
├── docs/             # Architecture, API spec, data model, PRD, deployment, smoke tests
├── .github/workflows # CI/CD: Docker image builds (GHCR) + scheduled cleanup
├── docker-compose.yml # Production deployment: backend (8000) + frontend (3000)
├── .env.example      # Docker Compose env template (ports, image refs)
├── start.sh          # Unified dev launcher (headless | full)
└── .gitmodules       # backend + frontend submodule URLs + branch pinning
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Proxy routing logic | `backend/app/routers/proxy.py` | Catch-all `/v1/{path}` + `/v1beta/{path}`, streaming + non-streaming |
| Load balancing | `backend/app/services/loadbalancer.py` | single/round_robin/failover strategies |
| Provider auth headers | `backend/app/services/proxy_service.py` | `PROVIDER_AUTH` dict, `build_upstream_headers()` |
| Model alias resolution | `backend/app/services/loadbalancer.py` | `get_model_config_with_endpoints()` resolves proxy→native |
| ORM models | `backend/app/models/models.py` | Provider, ModelConfig, Endpoint, RequestLog, AuditLog |
| Audit logging | `backend/app/services/audit_service.py` + `routers/audit.py` | Header redaction, body capture, per-provider toggle |
| Config export/import | `backend/app/routers/config.py` | Full config backup/restore with validation |
| API client (frontend) | `frontend/src/lib/api.ts` | Typed fetch wrapper, all API calls (6 namespaces) |
| TypeScript types | `frontend/src/lib/types.ts` | Must mirror backend Pydantic schemas |
| Endpoint navigation | `frontend/src/hooks/useEndpointNavigation.ts` | Click-to-navigate from stats/audit to model detail |
| Architecture docs | `docs/ARCHITECTURE.md` | Request flows, provider routing table, design decisions |
| API specification | `docs/API_SPEC.md` | Full endpoint documentation |
| Data model | `docs/DATA_MODEL.md` | Database schema reference |
| Deployment guide | `docs/DEPLOYMENT.md` | Docker, docker-compose, manual setup |

## CONVENTIONS

- **Submodules**: `backend/` and `frontend/` are git submodules with their own `.git` — commit in each separately (3 commits for cross-module changes: backend, frontend, root pointer update)
- **No root package manager**: No root package.json or pyproject.toml; each submodule manages its own deps
- **3 providers only**: OpenAI, Anthropic, Gemini — hardcoded in seed data and throughout the codebase
- **No auth**: Designed for trusted local/LAN — wildcard CORS, no authentication layer
- **API keys in plaintext**: Stored directly in SQLite — acceptable for single-user local deployment
- **Async everything**: Backend uses async SQLAlchemy + aiosqlite + httpx.AsyncClient
- **Frontend package manager**: pnpm 10.30.1 (pinned via `packageManager` field in package.json)
- **Frontend imports**: Use `@/` path alias (maps to `src/`)
- **UI components**: shadcn/ui (new-york style) — add with `pnpm dlx shadcn add <component>` (22 components installed)
- **7 backend routers**: providers, models, endpoints, stats, audit, config, proxy (mounted in this order in `main.py`)
- **Health endpoint**: `GET /health` returns `{"status": "ok", "version": "0.1.0"}`

## ANTI-PATTERNS (THIS PROJECT)

- **No chained proxies**: Proxy model must redirect to a native model, never another proxy
- **No cross-provider proxying**: OpenAI model can only alias another OpenAI model
- **Proxy models cannot have endpoints**: Blocked at creation and config import
- **Double `/v1/v1` path bug**: `build_upstream_url()` has auto-correction + input validation — do not bypass
- **No request body modification**: Gateway only rewrites `model` field for alias resolution; all other fields pass through unchanged
- **Streaming log session**: Stream logging uses a separate `AsyncSessionLocal()` in the generator's `finally` block — do not use the request-scoped DB session (it's closed by then)
- **Audit header redaction**: `audit_service.py` redacts `authorization`, `x-api-key`, `x-goog-api-key` and any header matching `key|secret|token|auth` pattern — do not log raw auth headers
- **No hop-by-hop headers**: `HOP_BY_HOP_HEADERS` frozenset in proxy_service.py — never add `content-length` or hop-by-hop headers to upstream requests
- **base_url trailing slash**: `normalize_base_url()` strips trailing `/` on create/update — don't store URLs ending with `/`

## COMMANDS

```bash
# Full stack (backend + frontend)
./start.sh full

# Backend only (headless)
./start.sh headless

# Docker Compose (production)
docker compose up -d

# Backend manually
cd backend && ./venv/bin/python -m uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

# Frontend manually
cd frontend && pnpm run dev

# Backend tests
cd backend && ./venv/bin/python -m pytest tests/

# Frontend lint
cd frontend && pnpm run lint

# Frontend build
cd frontend && pnpm run build    # tsc -b && vite build
```

## NOTES

- SQLite DB file: `backend/data/gateway.db` (auto-created on first run; Docker volume at `/app/data`)
- Schema migrations are manual — see `_add_missing_columns()` in `main.py` for the pattern
- Migrated columns: `auth_type`, `custom_headers` (endpoints); `audit_enabled`, `audit_capture_bodies` (providers); `endpoint_description` (request_logs); `endpoint_id`, `endpoint_base_url`, `endpoint_description` (audit_logs)
- `start.sh` auto-creates venv and installs deps if missing
- Backend port: 8000 (env: `BACKEND_PORT`), Frontend dev port: 5173 (env: `FRONTEND_PORT`), Frontend Docker port: 3000
- API docs at `http://localhost:8000/docs` (Swagger) and `/redoc`
- Frontend API base URL configurable via `VITE_API_BASE` env var (default: `http://localhost:8000`)
- Test deps (pytest, pytest-asyncio) are installed in venv but NOT listed in requirements.txt
- CI/CD: GitHub Actions builds arm64 Docker images to GHCR (`ghcr.io/coachpo/prism-{backend|frontend}`) on push to main/tags; daily cleanup at 3am UTC
- Docker Compose: frontend depends on backend health (`service_healthy`); backend healthcheck hits `/api/providers`
- Round-robin LB state is in-memory (`_rr_counters` dict) — resets on server restart
