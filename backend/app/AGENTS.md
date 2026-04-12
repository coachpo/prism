# BACKEND APP KNOWLEDGE BASE

## OVERVIEW
`app/` is the live Prism backend runtime. It owns app assembly, shared lifespan infrastructure, router and contract boundaries, and the app-local package maps that point to the deeper child AGENTS files.

## STRUCTURE
```
app/
├── main.py
├── alembic/AGENTS.md
├── bootstrap/AGENTS.md
├── dependencies.py
├── core/AGENTS.md
├── models/AGENTS.md
├── routers/AGENTS.md
├── routers/shared/AGENTS.md
├── routers/{auth,config,endpoints,models,pricing_templates,profiles,settings,stats}_domains/AGENTS.md
├── routers/connections_domains/AGENTS.md
├── routers/proxy_domains/AGENTS.md
├── schemas/AGENTS.md
├── services/AGENTS.md
└── services/{auth,loadbalancer,proxy_support,realtime,stats,webauthn}/AGENTS.md
```

## CHILD DOCS

- `alembic/AGENTS.md`: packaged migration runtime and revision source of truth.
- `bootstrap/AGENTS.md`: startup sequence and middleware auth split.
- `core/AGENTS.md`: settings, database, auth helpers, crypto, and migrations.
- `models/AGENTS.md`: ORM model ownership and domain splits.
- `routers/AGENTS.md`: router shells, standalone routers, and leaf handoff.
- `routers/shared/AGENTS.md`: reusable router-layer helpers shared across routers.
- `routers/{auth,config,endpoints,models,pricing_templates,profiles,settings,stats}_domains/AGENTS.md`: management router-domain leaves.
- `routers/connections_domains/AGENTS.md`, `routers/proxy_domains/AGENTS.md`: dense router packages.
- `schemas/AGENTS.md`: contract ownership and the `schemas.py` boundary.
- `services/AGENTS.md` and `services/{auth,loadbalancer,proxy_support,realtime,stats,webauthn}/AGENTS.md`: service facades and deeper package detail.

## APP FACTS

- `main.py` mounts the backend routers, builds shared app state, and exposes `/health`.
- Lifespan wiring owns startup, shared client setup, background-task setup, and teardown.
- Middleware auth stays split by plane, with `/api/*` using operator session rules and `/v1/*` plus `/v1beta/*` using proxy-key rules.
- Routers stay intentionally thin. Dense logic belongs in router-domain packages and service modules.
- Service-level reporting helpers keep their own package boundaries, especially `services/loadbalancer/` and `services/stats/`.

## WHERE TO LOOK

- App assembly, router mounts, lifespan startup, and shared infra ownership: `main.py`
- Startup sequence and startup-only seed logic: `bootstrap/startup.py`
- Migration packaging, env wiring, and revision layout: `alembic/AGENTS.md`, `alembic/env.py`, `alembic/script.py.mako`, `alembic/versions/`
- Management profile overrides versus runtime active-profile routing: `dependencies.py`
- Router surface, shared router helpers, and router-domain leaf docs: `routers/AGENTS.md`, `routers/shared/AGENTS.md`, `routers/`
- Contract exports and schema ownership: `schemas/AGENTS.md`, `schemas/schemas.py`
- Shared worker lifecycle and service public boundaries: `services/AGENTS.md`, `services/background_tasks.py`, `services/connection_health.py`
- Reporting helpers for load-balance events and model metrics: `services/loadbalance_event_summary.py`, `services/stats/model_metrics.py`
- Websocket auth and room-state handoff: `routers/realtime.py`, `services/realtime/connection_manager.py`

## CONVENTIONS

- Keep app-owned infrastructure in `main.py`. Feature code should consume `app.state.http_client` and `app.state.background_task_manager`.
- Keep profile-scope rules in `dependencies.py` instead of parsing headers inside handlers.
- Keep parent AGENTS files focused on package maps and ownership boundaries, not leaf implementation details.
- Keep auth, runtime proxy routing, realtime fanout, and stats assembly inside their existing service or domain boundaries.
- Keep Alembic revisions as the schema source of truth and use `core/migrations.py` for the programmatic migration seam.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not duplicate startup-sequence detail across route or service docs when `bootstrap/` already owns it.
- Do not treat management profile overrides and runtime proxy profile resolution as the same model.
- Do not bypass `services/realtime/connection_manager.py` with ad hoc websocket room state in routers.
- Do not import schema leaf modules directly when `schemas/schemas.py` already defines the supported surface.
- Do not stale-claim that most router-domain packages are covered only by `routers/AGENTS.md`; the management `*_domains/` packages now have their own leaf docs.
