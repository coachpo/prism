# BACKEND KNOWLEDGE BASE

## OVERVIEW
`backend/` is Prism's monorepo-owned Go backend tree. The live runtime is compiled from `cmd/prism-backend`. The legacy Python runtime surface has been retired from the checked-in backend tree; only explicit non-runtime artifacts called out below remain.

## STRUCTURE
```text
backend/
├── cmd/prism-backend/
├── internal/
│   ├── domain/
│   ├── endpointdomain/
│   ├── httpapi/
│   ├── platform/
│   ├── profiledomain/
│   └── vendordomain/
├── migrations/
├── testdata/
├── tests/
├── docker-compose.yml
├── Dockerfile
├── pyproject.toml
├── VERSION
└── AGENTS.md
```

## CHILD DOCS
- `tests/AGENTS.md`: backend regression and cutover verification boundary.

## RUNTIME FACTS
- `cmd/prism-backend/main.go` is the backend process entrypoint.
- `internal/platform/http/server.go` mounts management, runtime proxy, realtime, health, and docs routes.
- `internal/platform/migrate/` owns the SQL migration runner, cutover compatibility logic, and migration-path helpers.
- `internal/httpapi/management/`, `internal/httpapi/runtime/`, and `internal/httpapi/realtime/` own the live HTTP and websocket surfaces.
- `docs/openapi.json` is the checked-in management/health contract served by the Go backend at `/openapi.json`.
- `pyproject.toml` is a non-runtime metadata stub retained only so backend-local cutover metadata stays explicit; it is not a Python package surface.
- `tests/contract/` and `tests/integration/` are the Go cutover-verification packages. Other files under `tests/` are regression/support artifacts and do not represent a live backend runtime.

## WHERE TO LOOK
- Process entrypoint and top-level dependency wiring: `cmd/prism-backend/main.go`
- Router and server assembly: `internal/platform/http/server.go`
- Management API handlers: `internal/httpapi/management/`
- Runtime proxy path handling: `internal/httpapi/runtime/`
- Realtime websocket delivery: `internal/httpapi/realtime/`
- SQL migrations and legacy cutover table compatibility: `internal/platform/migrate/`, `migrations/`, `testdata/schema/cutover-live.sql`
- Version loading: `internal/platform/version/`, `VERSION`
- Regression boundaries: `tests/AGENTS.md`, `tests/`
## CONVENTIONS
- Keep backend docs focused on the live Go runtime and explicitly label any retained cutover artifact as non-runtime.
- Keep SQL migrations under `migrations/` as the live schema source of truth for startup.
- Keep management selected-profile behavior separate from runtime active-profile routing.
- Keep implementation detail in the Go ownership tree instead of reintroducing retired `backend/app/**` paths.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not describe Prism as a mixed Go+Python live backend.
- Do not point readers to `backend/app/**`, `alembic.ini`, or `uv.lock` as current surfaces.
- Do not reintroduce live Alembic CLI, uv, or Python package setup flows.
- Do not invent unsupported providers, routes, or CI jobs.
