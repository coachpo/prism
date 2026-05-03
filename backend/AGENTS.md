# BACKEND KNOWLEDGE BASE

## OVERVIEW
`backend/` is Prism's monorepo-owned Go backend tree. The live runtime is compiled from `cmd/prism-backend` and owns Prism's management API, runtime proxy, realtime delivery, platform lifecycle, startup sequencing, SQL migrations, priority isolation, and durable background side effects.

## STRUCTURE
```text
backend/
├── cmd/prism-backend/
├── internal/
│   ├── domain/
│   ├── endpointdomain/
│   ├── httpapi/
│   │   └── AGENTS.md
│   ├── pgxutil/
│   ├── platform/
│   │   └── AGENTS.md
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
- `internal/platform/AGENTS.md`: backend process infrastructure, lifecycle assembly, hot bootstrap runtime, DB lanes, scheduler, migrations, and side-effect ownership.
- `internal/httpapi/AGENTS.md`: mounted management, runtime, realtime, OpenAPI, proxy-key usage, and request-context seams.
- `tests/AGENTS.md`: backend Go regression boundary, including priority/lane isolation tests.

## RUNTIME FACTS
- `cmd/prism-backend/main.go` is the backend process entrypoint.
- `internal/platform/lifecycle/` wires production services, DB lanes, runtime cache bootstrap, scheduler workers, side-effect drains, and shutdown order.
- `internal/platform/http/server.go` mounts `/health`, DB-backed `/metrics`, `/api`, `/openapi.json`, `/docs`, `/redoc`, `/v1`, and `/v1beta`.
- `internal/platform/http/hot_bootstrap_runtime.go` publishes hot snapshots for CORS, auth, mail, runtime proxy transport, and admission limits.
- `internal/platform/config/` owns the plaintext bootstrap contract loaded by `cmd/prism-backend/main.go`; eligible runtime fields hot-apply through the Startup tab or bootstrap API, while structural fields stay restart-required.
- `internal/platform/startup/` and `internal/platform/migrate/` own startup sequencing, SQL migration execution, vendor/profile/settings seeds, and endpoint-secret normalization.
- `internal/httpapi/management/` fans out into mounted management subpackages for auth, bootstrapconfig, configbundle, configrules, connections, endpoints, loadbalance, models, profiles, settings, stats, vendors, and audit.
- `internal/httpapi/runtime/` owns OpenAI, Anthropic, and Gemini-compatible proxy routes plus runtime cache, request logging, telemetry outbox, streaming, and load-balance helpers.
- `../docs/openapi.json` is the checked-in management/health contract served by the Go backend at `/openapi.json`; runtime proxy routes are documented narratively instead.
- `tests/contract/`, `tests/integration/`, `tests/runtime/`, and `tests/priority/` are the checked-in Go regression packages.
- Bootstrap config v1 is plaintext and file-backed. Existing files must carry `runtime.transport.requestTimeout`, and legacy encrypted bootstrap fields are rejected.
- Mail is controlled by bootstrap config. Missing or disabled mail means no-op delivery; enabled SMTP must validate at startup and must not silently fall back.

## WHERE TO LOOK
- Process entrypoint: `cmd/prism-backend/main.go`
- Platform lifecycle, server assembly, hot bootstrap runtime, DB lanes, startup, migrations, scheduler, and side effects: `internal/platform/AGENTS.md`
- Mounted management, runtime, realtime, OpenAPI, proxy-key usage, and request-context seams: `internal/httpapi/AGENTS.md`
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
- Do not describe all bootstrap writes as restart-only. Distinguish hot-eligible fields from restart-required fields.
- Do not treat enabled-but-invalid SMTP as recoverable no-op delivery.
