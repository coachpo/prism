# BACKEND KNOWLEDGE BASE

## OVERVIEW
`backend/` is Prism's monorepo-owned Go backend tree. The live runtime is compiled from `cmd/prism-backend` and owns Prism's management API, runtime proxy, realtime delivery, startup sequencing, SQL migrations, priority isolation, and durable background side effects.

## STRUCTURE
```text
backend/
├── cmd/prism-backend/
├── internal/
│   ├── domain/
│   ├── endpointdomain/
│   ├── httpapi/
│   ├── pgxutil/
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
- `tests/AGENTS.md`: backend Go regression boundary, including priority/lane isolation tests.

## RUNTIME FACTS
- `cmd/prism-backend/main.go` is the backend process entrypoint.
- `internal/platform/http/server.go` mounts management, runtime proxy, realtime, health, and docs routes.
- `internal/platform/config/` owns the plaintext bootstrap config contract loaded by `cmd/prism-backend/main.go`.
- `internal/platform/migrate/` owns the Go SQL migration runner and schema snapshot helpers.
- `internal/platform/startup/` owns startup sequencing, migration execution, vendor/profile/settings seeds, and endpoint-secret normalization.
- `internal/platform/priority/`, `internal/platform/admission/`, `internal/platform/background/`, `internal/platform/managementsideeffects/`, `internal/platform/managementjobs/`, and `internal/platform/email/outbox/` own priority metadata, lane admission, background scheduling, after-commit management side effects, low-priority jobs, and durable email delivery.
- `internal/httpapi/management/` fans out into the mounted management subpackages for auth, bootstrapconfig, configbundle, configrules, connections, endpoints, loadbalance, models, profiles, settings, stats, vendors, and audit.
- `internal/httpapi/openapi/`, `internal/httpapi/proxykeyusage/`, `internal/httpapi/requestcontext/`, `internal/httpapi/runtime/`, and `internal/httpapi/realtime/` cover the checked-in OpenAPI contract, proxy-key usage capture, request-context helpers, runtime proxy surface, and websocket delivery seams.
- `docs/openapi.json` is the checked-in management/health contract served by the Go backend at `/openapi.json`.
- `tests/contract/`, `tests/integration/`, `tests/runtime/`, and `tests/priority/` are the checked-in Go regression packages.
- Bootstrap config v1 is plaintext and file-backed. Existing files must carry `runtime.transport.requestTimeout`, and legacy encrypted bootstrap fields are rejected.
- Mail is controlled by bootstrap config. Missing or disabled mail means no-op delivery; enabled SMTP must validate at startup and must not silently fall back.

## WHERE TO LOOK
- Process entrypoint and top-level dependency wiring: `cmd/prism-backend/main.go`
- Router and server assembly: `internal/platform/http/`, `internal/platform/http/server.go`
- Management API handlers: `internal/httpapi/management/`
- Proxy-key usage, request-context, OpenAPI, realtime, and runtime handler seams: `internal/httpapi/proxykeyusage/`, `internal/httpapi/requestcontext/`, `internal/httpapi/openapi/`, `internal/httpapi/realtime/`, `internal/httpapi/runtime/`
- Config, migrations, startup, and version loading: `internal/platform/config/`, `internal/platform/migrate/`, `internal/platform/startup/`, `internal/platform/version/`
- Priority, admission, DB lanes, background workers, after-commit side effects, management jobs, and email outbox: `internal/platform/priority/`, `internal/platform/admission/`, `internal/platform/background/`, `internal/platform/db/`, `internal/platform/managementsideeffects/`, `internal/platform/managementjobs/`, `internal/platform/email/`
- Shared transaction helper: `internal/pgxutil/tx.go`
- SQL migrations and startup sequencing: `migrations/`, `internal/platform/migrate/`
- Runtime stats, request-log shaping, and loadbalance business logic: `internal/domain/stats/`, `internal/domain/loadbalance/`, `internal/domain/audit/`
- Regression boundaries: `tests/AGENTS.md`, `tests/`

## CONVENTIONS
- Keep backend docs focused on the live Go runtime.
- Keep SQL migrations under `migrations/` as the live schema source of truth for startup.
- Keep management selected-profile behavior separate from runtime active-profile routing.
- Keep `api_family` as runtime compatibility truth. Vendor rows and `icon_key` are presentation metadata.
- Keep bootstrap config separate from PostgreSQL-backed profile/vendor bundle import and export.
- Keep request-path side effects on durable outboxes, scheduler-owned workers, or after-commit wakeups; do not put provider sends, cache invalidations, or dashboard materialization inline.
- Keep database pool lane ownership explicit. Background, realtime, telemetry, feedback, management, cache refresh, and runtime execution lanes are separate capacity budgets.
- Keep implementation detail in the Go ownership tree instead of inventing alternate runtime surfaces.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not describe Prism as a mixed-runtime backend.
- Do not point readers to retired backend runtime surfaces as current implementation paths.
- Do not invent unsupported providers, routes, or CI jobs.
- Do not describe bootstrap updates as hot-reloadable runtime config; bootstrap writes apply after restart.
- Do not treat enabled-but-invalid SMTP as recoverable no-op delivery.
