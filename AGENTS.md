# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-01
**Branch:** main

## OVERVIEW

Prism is a self-hosted LLM proxy gateway with profile-scoped management and active-profile runtime routing. It fronts OpenAI, Anthropic, and Gemini behind OpenAI-compatible `/v1/*` and Gemini-style `/v1beta/*` routes, with failover, streaming passthrough, audit logging, and per-request costing.

Monorepo layout: root repo + `backend/` and `frontend/` git submodules.

## STRUCTURE

```
prism/
├── backend/                  # FastAPI + async SQLAlchemy + PostgreSQL (submodule)
│   ├── app/main.py           # Lifespan: validate DB URL, run migrations, seed providers/settings/blocklist
│   ├── app/dependencies.py   # Active vs effective profile dependencies
│   ├── app/models/models.py  # ORM models (profiles, routing config, logs, costing settings)
│   ├── app/routers/          # /api/* management + /v1/* and /v1beta/* proxy routes
│   ├── app/services/         # load balancing, proxying, stats, costing, audit
│   └── tests/                # pytest defect-driven regressions
├── frontend/                 # React 19 + TypeScript + Vite dashboard (submodule)
│   └── src/
│       ├── App.tsx           # 9 lazy routes in AppLayout (includes /pricing-templates)
│       ├── components/layout/AppLayout.tsx
│       ├── context/ProfileContext.tsx
│       ├── hooks/useConnectionNavigation.ts
│       ├── lib/api.ts        # Typed API client + X-Profile-Id injection for /api/*
│       └── pages/            # Dashboard, Models, ModelDetail, Endpoints, Statistics, RequestLogs, Audit, Settings, PricingTemplates
├── docs/                     # Architecture, API, data model, PRD, smoke tests
├── .github/workflows/        # Docker image build + cleanup
├── start.sh                  # `full` or `headless` local startup
└── .env.example
```

## RUNTIME MODEL

- Management plane (`/api/*`) uses effective profile: explicit `X-Profile-Id` on profile-scoped routes (`/api/profiles/*` are global).
- Data plane (`/v1/*`, `/v1beta/*`) always uses active profile and ignores management overrides.
- Profile lifecycle: create, update, CAS activate, soft-delete inactive profile, max 10 non-deleted profiles.

## KEY BACKEND FACTS

- Database is PostgreSQL via `asyncpg` (not SQLite).
- Migrations run on startup (`run_migrations()` in lifespan).
- Startup seeds default providers, default user settings, and system header blocklist rules.
- Supported providers are hardcoded: `openai`, `anthropic`, `gemini`.
- Failover trigger statuses: `403, 429, 500, 502, 503, 529`.
- Failover recovery state is in-memory and keyed per profile/connection; resets on process restart.
- Config export/import canonical format is `version: 2` with explicit IDs (`endpoint_id`, `connection_id`, `pricing_template_id`) and replace semantics.
- Costing stores integer micros (`*_micros`) and records pricing snapshots in request logs.

## KEY FRONTEND FACTS

- ProfileContext persists selected profile in localStorage and updates API header context via `setApiProfileId()`.
- App shell shows selected vs active profile mismatch and explicit activation controls.
- All backend calls use `frontend/src/lib/api.ts`; no raw fetch usage pattern for feature pages.
- Stats and spending pages rely on backend aggregates and costing micros formatting helpers.

## WHERE TO LOOK

- Proxy flow: `backend/app/routers/proxy.py`, `backend/app/services/proxy_service.py`, `backend/app/services/loadbalancer.py`
- Profile semantics: `backend/app/dependencies.py`, `backend/app/routers/profiles.py`, `frontend/src/context/ProfileContext.tsx`
- Config import/export + blocklist: `backend/app/routers/config.py`, `frontend/src/pages/SettingsPage.tsx`
- Costing + spending: `backend/app/services/costing_service.py`, `backend/app/services/stats_service.py`, `backend/app/routers/settings.py`, `backend/app/routers/stats.py`
- Dashboard/API wiring: `frontend/src/App.tsx`, `frontend/src/lib/api.ts`, `frontend/src/pages/*`
- Navigation to connection owner: `backend/app/routers/connections.py`, `frontend/src/hooks/useConnectionNavigation.ts`

## COMMANDS

```bash
./start.sh full
./start.sh headless
cd backend && ./venv/bin/python -m pytest tests/ -v
cd frontend && pnpm run lint
cd frontend && pnpm run build
```

## CONVENTIONS

- Keep backend async end-to-end; avoid lazy-loading ORM access patterns in request handlers.
- Treat backend schemas as source of truth; keep frontend `lib/types.ts` aligned.
- Use `@/` imports in frontend.
- Use `pnpm` for frontend package operations.
- Do not introduce new provider types without coordinated backend+frontend updates.

## ANTI-PATTERNS

- Do not use request-scoped DB session inside streaming generators.
- Do not chain proxy aliases.
- Do not add removed `round_robin` strategy back into behavior.
- Do not store money as float.
- Do not assume selected profile equals active runtime profile.

## NOTES

- `backend/` and `frontend/` are separate git repositories (submodules).
- Docker images in CI target `linux/arm64`.
- This project is intended for trusted local/LAN deployment (no built-in auth layer).