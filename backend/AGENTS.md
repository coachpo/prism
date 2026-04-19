# BACKEND KNOWLEDGE BASE

## OVERVIEW
`backend/` is Prism's monorepo-owned Go backend tree. The live runtime is compiled from `cmd/prism-backend` and owns Prism's management API, runtime proxy, realtime delivery, startup sequencing, and SQL migrations.

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
├── VERSION
└── AGENTS.md
```

## CHILD DOCS
- `tests/AGENTS.md`: backend Go regression boundary.

## RUNTIME FACTS
- `cmd/prism-backend/main.go` is the backend process entrypoint.
- `internal/platform/http/server.go` mounts management, runtime proxy, realtime, health, and docs routes.
- `internal/platform/migrate/` owns the Go SQL migration runner and schema snapshot helpers.
- `internal/platform/startup/` owns startup sequencing, migration execution, and seed/backfill steps.
- `internal/httpapi/management/`, `internal/httpapi/runtime/`, and `internal/httpapi/realtime/` own the live HTTP and websocket surfaces.
- `docs/openapi.json` is the checked-in management/health contract served by the Go backend at `/openapi.json`.
- `tests/contract/`, `tests/integration/`, and `tests/runtime/` are the checked-in Go regression packages.

## WHERE TO LOOK
- Process entrypoint and top-level dependency wiring: `cmd/prism-backend/main.go`
- Router and server assembly: `internal/platform/http/server.go`
- Management API handlers: `internal/httpapi/management/`
- Runtime proxy path handling: `internal/httpapi/runtime/`
- Realtime websocket delivery: `internal/httpapi/realtime/`
- SQL migrations and startup sequencing: `internal/platform/migrate/`, `internal/platform/startup/`, `migrations/`
- Version loading: `internal/platform/version/`, `VERSION`
- Regression boundaries: `tests/AGENTS.md`, `tests/`

## CONVENTIONS
- Keep backend docs focused on the live Go runtime.
- Keep SQL migrations under `migrations/` as the live schema source of truth for startup.
- Keep management selected-profile behavior separate from runtime active-profile routing.
- Keep implementation detail in the Go ownership tree instead of inventing alternate runtime surfaces.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not describe Prism as a mixed-runtime backend.
- Do not point readers to retired backend runtime surfaces as current implementation paths.
- Do not invent unsupported providers, routes, or CI jobs.
