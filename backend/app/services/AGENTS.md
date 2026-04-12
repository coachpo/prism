# BACKEND SERVICES ROOT KNOWLEDGE BASE

## OVERVIEW
`services/` is the backend service boundary. It holds the public facades imported by routers, shared runtime infrastructure, split child packages, and a small set of root helpers that don't need their own package yet.

## STRUCTURE
```
services/
├── auth_service.py
├── webauthn_service.py
├── stats_service.py
├── proxy_service.py
├── connection_health.py
├── loadbalancer/
├── audit_service.py
├── costing_service.py
├── background_tasks.py
├── background_cleanup.py
├── loadbalance_cleanup.py
├── loadbalance_event_summary.py
├── profile_invariants.py
├── user_settings.py
├── auth/AGENTS.md
├── loadbalancer/AGENTS.md
├── proxy_support/AGENTS.md
├── realtime/AGENTS.md
├── stats/AGENTS.md
└── webauthn/AGENTS.md
```

## WHERE TO LOOK

- Shared worker lifecycle and metrics snapshots: `background_tasks.py`, `../main.py`
- Public auth boundary: `auth_service.py`, `auth/AGENTS.md`
- Public passkey boundary: `webauthn_service.py`, `webauthn/AGENTS.md`
- Manual connection-health request building and preview payloads: `connection_health.py`
- Runtime routing, attempt planning, candidate scoring, deadline-aware execution, runtime leases/state, and upstream forwarding: `loadbalancer/AGENTS.md`, `proxy_service.py`, `proxy_support/AGENTS.md`
- Observability, request logging, dashboard payload shaping, and batch model or connection metrics: `stats_service.py`, `audit_service.py`, `stats/AGENTS.md`
- Load-balance event detail wording and cooldown summaries: `loadbalance_event_summary.py`, `loadbalancer/AGENTS.md`
- Realtime room-state ownership: `realtime/AGENTS.md`, `realtime/connection_manager.py`
- Startup-enforced defaults and retention cleanup: `profile_invariants.py`, `user_settings.py`, `background_cleanup.py`, `loadbalance_cleanup.py`

## SERVICE FACTS

- `background_tasks.py` defines `BackgroundTaskManager`, queue and worker lifecycle, retry handling, enqueue rejection tracking, and metrics snapshots.
- FastAPI lifespan in `../main.py` configures `background_task_manager`, starts it, stores it on `app.state`, and shuts it down during teardown.
- `auth_service.py`, `stats_service.py`, and `webauthn_service.py` are intended public import surfaces over deeper packages.
- `loadbalancer/` carries candidate scoring, deadline-aware execution, runtime lease and state persistence, strategy CRUD, and management-facing current-state and event helpers.
- `loadbalance_event_summary.py` is the root helper for human-readable load-balance event labels, reasons, and cooldown text used by load-balance detail responses.
- Realtime route handlers depend on `services/realtime/connection_manager.py` for connection tracking and room membership instead of owning that state themselves.
- `services/stats/logging.py` owns request-log side effects and emits `dashboard.update` payloads.

## CONVENTIONS

- Keep routers thin by importing service-root facades or package-owned helpers instead of duplicating durable business logic.
- Treat the shared background task manager as app-owned infrastructure. Start and stop it in lifespan, then consume it from feature code.
- Keep passkey logic separate from the auth package. The public boundary is `webauthn_service.py` plus `services/webauthn/`.
- Keep cleanup helpers explicit and separate from request-serving code so retention work stays testable.
- Keep one-off reporting helpers at the service root only when they don't warrant a new package. `loadbalance_event_summary.py` is that kind of module.
- Keep the loadbalancer package decomposed. Execution, scoring, runtime-store, limiter, recovery, and strategy-admin seams should stay separate instead of collapsing back into a flat service module.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not import deep package internals when a service-root facade already exists.
- Do not spawn ad hoc worker pools or background queues from feature code when `background_tasks.py` already owns the shared worker model.
- Do not hide load-balance event presentation logic inside routers when `loadbalance_event_summary.py` already defines the supported summary payload.
- Do not push routing, auth, or observability logic back into route handlers once an established service boundary already owns it.
- Do not flatten `services/loadbalancer/` back into one module or bypass its runtime-store, scoring, or execution seams from routers.
