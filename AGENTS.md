# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-20
**Commit:** 6352bac
**Branch:** main

## OVERVIEW

LLM Proxy Gateway — a lightweight reverse proxy that routes LLM API requests to OpenAI, Anthropic, and Gemini with load balancing, failover, model aliasing, request telemetry, and audit logging. Python/FastAPI backend + React/TypeScript frontend.

## STRUCTURE

```
transparent-agents/
├── backend/          # FastAPI API + proxy engine (git submodule)
├── frontend/         # React SPA dashboard (git submodule)
├── docs/             # Architecture, API spec, data model, PRD, deployment, smoke tests, design docs
├── .github/workflows # CI/CD: Docker image builds (GHCR) + scheduled cleanup
├── start.sh          # Unified dev launcher (headless | full)
└── .gitmodules       # backend + frontend are local submodules
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Proxy routing logic | `backend/app/routers/proxy.py` | Catch-all `/v1/{path}`, streaming + non-streaming |
| Load balancing | `backend/app/services/loadbalancer.py` | single/round_robin/failover strategies |
| Provider auth headers | `backend/app/services/proxy_service.py` | `PROVIDER_AUTH` dict, `build_upstream_headers()` |
| Model alias resolution | `backend/app/services/loadbalancer.py` | `get_model_config_with_endpoints()` resolves proxy→native |
| ORM models | `backend/app/models/models.py` | Provider, ModelConfig, Endpoint, RequestLog, AuditLog |
| Audit logging | `backend/app/services/audit_service.py` + `routers/audit.py` | Header redaction, body capture, per-provider toggle |
| Config export/import | `backend/app/routers/config.py` | Full config backup/restore with validation |
| API client (frontend) | `frontend/src/lib/api.ts` | Typed fetch wrapper, all API calls |
| TypeScript types | `frontend/src/lib/types.ts` | Must mirror backend Pydantic schemas |
| Architecture docs | `docs/ARCHITECTURE.md` | Request flows, provider routing table, design decisions |
| API specification | `docs/API_SPEC.md` | Full endpoint documentation |
| Data model | `docs/DATA_MODEL.md` | Database schema reference |
| Audit design | `docs/DESIGN_REQUEST_AUDIT.md` | Audit feature design and decisions |
| Config export design | `docs/DESIGN_CONFIG_EXPORT_IMPORT.md` | Config export/import feature design |

## CONVENTIONS

- **Submodules**: `backend/` and `frontend/` are git submodules with their own `.git` — commit in each separately
- **No root package manager**: No root package.json or pyproject.toml; each submodule manages its own deps
- **3 providers only**: OpenAI, Anthropic, Gemini — hardcoded in seed data and throughout the codebase
- **No auth**: Designed for trusted local/LAN — wildcard CORS, no authentication layer
- **API keys in plaintext**: Stored directly in SQLite — acceptable for single-user local deployment
- **Async everything**: Backend uses async SQLAlchemy + aiosqlite + httpx.AsyncClient
- **Frontend imports**: Use `@/` path alias (maps to `src/`)
- **UI components**: shadcn/ui via `components.json` — add with `npx shadcn add <component>`
- **7 backend routers**: providers, models, endpoints, stats, audit, config, proxy (mounted in this order in `main.py`)

## ANTI-PATTERNS (THIS PROJECT)

- **No chained proxies**: Proxy model must redirect to a native model, never another proxy
- **No cross-provider proxying**: OpenAI model can only alias another OpenAI model
- **Double `/v1/v1` path bug**: `build_upstream_url()` has auto-correction + input validation — do not bypass
- **No request body modification**: Gateway only rewrites `model` field for alias resolution; all other fields pass through unchanged
- **Streaming log session**: Stream logging uses a separate `AsyncSessionLocal()` in the generator's `finally` block — do not use the request-scoped DB session (it's closed by then)
- **Audit header redaction**: `audit_service.py` redacts `authorization`, `x-api-key`, `x-goog-api-key` and any header matching `key|secret|token|auth` pattern — do not log raw auth headers

## COMMANDS

```bash
# Full stack (backend + frontend)
./start.sh full

# Backend only (headless)
./start.sh headless

# Backend manually
cd backend && ./venv/bin/python -m uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

# Frontend manually
cd frontend && npm run dev

# Backend tests
cd backend && ./venv/bin/python -m pytest tests/

# Frontend lint
cd frontend && npm run lint

# Frontend build
cd frontend && npm run build    # tsc -b && vite build
```

## NOTES

- SQLite DB file: `backend/gateway.db` (auto-created on first run)
- Smoke test DB: `backend/gateway_smoke.db` (separate file for manual testing)
- Schema migrations are manual — see `_add_missing_columns()` in `main.py` for the pattern
- Migrated columns: `auth_type`, `custom_headers` (endpoints); `audit_enabled`, `audit_capture_bodies` (providers)
- `start.sh` auto-creates venv and installs deps if missing
- Backend port: 8000 (env: `BACKEND_PORT`), Frontend port: 5173 (env: `FRONTEND_PORT`)
- API docs at `http://localhost:8000/docs` (Swagger) and `/redoc`
- Frontend API base URL configurable via `VITE_API_BASE` env var (default: `http://localhost:8000`)
- Test deps (pytest, pytest-asyncio) are installed in venv but NOT listed in requirements.txt
- CI/CD: GitHub Actions builds Docker images to GHCR on push to main/tags; daily cleanup job at 3am UTC
