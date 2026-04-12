# BACKEND KNOWLEDGE BASE

## OVERVIEW
`backend/` is Prism's monorepo-owned backend. It owns the management API, the runtime proxy API, the packaged `app/` runtime, and the backend-local release and setup surfaces. Keep this doc as the top-level map, and push package detail into the child AGENTS files.

## STRUCTURE
```
backend/
├── app/AGENTS.md
├── app/alembic/AGENTS.md
├── app/bootstrap/AGENTS.md
├── app/core/AGENTS.md
├── app/models/AGENTS.md
├── app/routers/AGENTS.md
├── app/routers/shared/AGENTS.md
├── app/routers/{auth,config,endpoints,models,pricing_templates,profiles,settings,stats}_domains/AGENTS.md
├── app/routers/connections_domains/AGENTS.md
├── app/routers/proxy_domains/AGENTS.md
├── app/schemas/AGENTS.md
├── app/services/AGENTS.md
├── app/services/{auth,loadbalancer,proxy_support,realtime,stats,webauthn}/AGENTS.md
├── alembic.ini
├── docker-compose.yml
├── pyproject.toml
└── uv.lock
```

## CHILD DOCS

- `app/AGENTS.md`: runtime map and app-owned boundaries.
- `app/alembic/AGENTS.md`, `app/bootstrap/AGENTS.md`, `app/core/AGENTS.md`, `app/models/AGENTS.md`, `app/schemas/AGENTS.md`: startup, migrations, shared infra, ORM, and contract boundaries.
- `app/routers/AGENTS.md`: API surface map and router handoff rules.
- `app/routers/shared/AGENTS.md`: shared router-layer helpers.
- `app/routers/{auth,config,endpoints,models,pricing_templates,profiles,settings,stats}_domains/AGENTS.md`: management router-domain leaves.
- `app/routers/connections_domains/AGENTS.md`, `app/routers/proxy_domains/AGENTS.md`: dense router packages with their own boundaries.
- `app/services/AGENTS.md` and `app/services/{auth,loadbalancer,proxy_support,realtime,stats,webauthn}/AGENTS.md`: service-root boundaries and deeper package ownership.

## RUNTIME FACTS

- `pyproject.toml` exposes `prism-backend = "app.main:main"` as the CLI entrypoint.
- `app/main.py` owns app assembly, router mounting, CORS and auth middleware, and shared lifespan-managed infrastructure.
- Management requests use effective-profile scope, while runtime proxy traffic uses the active profile only.
- `/api/*` keeps operator session auth, and `/v1/*` plus `/v1beta/*` keep proxy API-key auth.
- Realtime room state lives in `services/realtime/connection_manager.py`, and dashboard update emission lives in `services/stats/logging.py`.

## WHERE TO LOOK

- App assembly, router registration, lifespan startup, and shared infra wiring: `app/main.py`
- Startup sequencing and startup-only seed logic: `app/bootstrap/startup.py`
- Management versus runtime scope rules: `app/dependencies.py`
- Router map, shared router helpers, and router-domain leaves: `app/routers/AGENTS.md`, `app/routers/shared/AGENTS.md`, `app/routers/`
- Public schema and model import boundaries: `app/schemas/AGENTS.md`, `app/models/AGENTS.md`
- Shared worker lifecycle, realtime room state, dashboard updates, and reporting helpers: `app/services/AGENTS.md`, `app/services/realtime/connection_manager.py`, `app/services/stats/logging.py`
- Migration source of truth: `alembic.ini`, `app/alembic/`, `app/alembic/AGENTS.md`, `app/core/migrations.py`

## CONVENTIONS

- Keep backend workflow and commands uv-native.
- Keep parent docs summary-oriented and push package detail down into child AGENTS files.
- Keep app-owned shared infrastructure in `app/main.py`, and let feature code consume `app.state.http_client` and `app.state.background_task_manager`.
- Keep routers thin. Dense logic belongs in `*_domains/`, `connections_domains/`, `proxy_domains/`, or service modules.
- Use `app.schemas.schemas`, `app.models.models`, and the service-root `*_service.py` modules as the supported re-export boundaries.
- Keep management auth and profile rules separate from runtime proxy auth and API-family-native routing semantics.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not invent unsupported vendors, API families, routes, or CI jobs.
- Do not describe schema state as coming from ORM models or startup side effects; Alembic revisions under `app/alembic/` are the source of truth.
- Do not reintroduce manual venv or `pip install` setup language.
- Do not describe `docker-compose.yml` as a full stack definition. It provisions PostgreSQL only.
- Do not import schema, model, or service leaf modules when a documented re-export boundary exists.
- Do not blur management effective-profile behavior with runtime active-profile routing or proxy-key auth.
- Do not stale-claim that most router-domain packages are parent-covered; the management `*_domains/` packages now have their own AGENTS leaves.
