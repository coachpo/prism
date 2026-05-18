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
- `internal/platform/AGENTS.md`: backend process infrastructure, lifecycle assembly, hot bootstrap runtime, DB lanes, scheduler, migrations, partitioned log retention, and side-effect ownership.
- `internal/httpapi/AGENTS.md`: mounted management, runtime, realtime, OpenAPI, proxy-key usage, retention-job, and request-context seams.
- `internal/httpapi/runtime/AGENTS.md`: runtime proxy handlers, request planning, telemetry outbox, feedback pipeline, partition ensuring, and side-effect seams.
- `internal/httpapi/management/settings/AGENTS.md`: profile-scoped costing/timezone settings, global log-retention settings, and retention-job endpoints.
- `internal/httpapi/management/auth/AGENTS.md`: auth status/session/bootstrap, proxy-key, WebAuthn, reset-email, realtime, and runtime-cache seams.
- `internal/httpapi/management/sidecars/AGENTS.md`: global CLIProxyAPI sidecar registrations, sync, auth/provider inventory, direct auth-file mutation, and worker seams.
- `tests/AGENTS.md`: backend Go regression boundary, including partitioned logs, Dockerfile, sidecars, and priority/lane isolation tests.

## RUNTIME FACTS
- `cmd/prism-backend/main.go` is the backend process entrypoint.
- `internal/platform/lifecycle/` wires production services, DB lanes, runtime cache bootstrap, scheduler workers, side-effect drains, and shutdown order.
- `internal/platform/http/server.go` mounts `/health`, DB-backed `/metrics`, `/api`, `/openapi.json`, `/docs`, `/redoc`, `/v1`, and `/v1beta`.
- `internal/platform/http/hot_bootstrap_runtime.go` publishes hot snapshots for CORS, auth, mail, runtime proxy transport, and admission limits.
- `internal/platform/config/` owns the plaintext bootstrap contract loaded by `cmd/prism-backend/main.go`; eligible runtime fields hot-apply through the Startup tab or bootstrap API, while structural fields stay restart-required.
- `internal/platform/startup/` and `internal/platform/migrate/` own startup sequencing, SQL migration execution, vendor/profile/settings seeds, and endpoint-secret normalization.
- `internal/platform/logretention/` owns daily partitions, 15-day horizon creation, retention deletes, and low-priority partition maintenance for `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- `internal/httpapi/management/` fans out into mounted management subpackages for auth, bootstrapconfig, configbundle, configrules, connections, endpoints, loadbalance, models, profiles, settings, sidecars, stats, vendors, and audit.
- `internal/httpapi/management/sidecars/` owns global CLIProxyAPI sidecar control-plane routes, sidecar snapshots, direct auth-file mutations, and the low-priority sync worker.
- `internal/httpapi/management/settings/` owns global log-retention settings and management-job creation in addition to profile-scoped costing and timezone settings.
- `internal/httpapi/runtime/` owns OpenAI, Anthropic, and Gemini-compatible proxy routes plus runtime cache, request logging, telemetry outbox, streaming, load-balance helpers, and runtime partition ensuring.
- `../docs/openapi.json` is the checked-in management/health contract served by the Go backend at `/openapi.json`; runtime proxy routes are documented narratively instead.
- `Dockerfile` builds from the monorepo root, copies migrations and `docs/openapi.json`, runs as `prism:prism` (`1000:1000`), and defaults `PRISM_CONFIG_PATH` to `/app/config/config.json`.
- `tests/contract/`, `tests/integration/`, `tests/runtime/`, and `tests/priority/` are the checked-in Go regression packages.
- Bootstrap config v1 is plaintext and file-backed. Existing files must carry `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout`, and legacy encrypted bootstrap fields are rejected.
- Mail is controlled by bootstrap config. Missing or disabled mail means no-op delivery; enabled SMTP must validate at startup and must not silently fall back.

## WHERE TO LOOK
- Process entrypoint: `cmd/prism-backend/main.go`
- Platform lifecycle, server assembly, hot bootstrap runtime, DB lanes, startup, migrations, scheduler, log retention, and side effects: `internal/platform/AGENTS.md`
- Mounted management, runtime, realtime, OpenAPI, proxy-key usage, retention-job, and request-context seams: `internal/httpapi/AGENTS.md`
- Runtime proxy entry, request planning, telemetry outbox, feedback pipeline, partition ensuring, and side-effect seams: `internal/httpapi/runtime/AGENTS.md`
- Management settings costing, timezone, retention settings, and maintenance-job endpoints: `internal/httpapi/management/settings/AGENTS.md`
- Management auth status/session/bootstrap, proxy-key, WebAuthn, reset-email, realtime, and runtime-cache seams: `internal/httpapi/management/auth/AGENTS.md`
- Global sidecar registration, CLIProxyAPI sync, auth/provider inventory, and direct auth-file mutation: `internal/httpapi/management/sidecars/AGENTS.md`
- Shared transaction helper: `internal/pgxutil/tx.go`
- SQL migrations, partitioned log schema, and startup sequencing: `migrations/`, `internal/platform/migrate/`, `internal/platform/logretention/`
- Runtime stats, request-log shaping, runtime partition ensuring, and loadbalance business logic: `internal/domain/stats/`, `internal/domain/loadbalance/`, `internal/domain/audit/`, `internal/httpapi/runtime/log_partitions.go`
- Container contract: `Dockerfile`, `tests/integration/dockerfile_contract_test.go`
- Regression boundaries: `tests/AGENTS.md`, `tests/`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep backend docs focused on the live Go runtime.
- Keep SQL migrations under `migrations/` as the live schema source of truth for startup.
- Keep management selected-profile behavior separate from runtime active-profile routing; sidecar management is global instance state and does not use selected-profile scope.
- Keep `api_family` as runtime compatibility truth. Vendor rows and `icon_key` are presentation metadata.
- Keep bootstrap config separate from PostgreSQL-backed profile/vendor bundle import and export.
- Keep request-path side effects on durable outboxes, scheduler-owned workers, or after-commit wakeups; do not put provider sends, cache invalidations, or dashboard materialization inline.
- Keep database pool lane ownership explicit. Background, realtime, telemetry, feedback, management, cache refresh, and runtime execution lanes are separate capacity budgets.
- Keep partitioned log tables under `internal/platform/logretention/` and runtime partition ensuring; managed tables are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- Keep backend container execution non-root with writable config ownership under `/app/config`; update `tests/integration/dockerfile_contract_test.go` when changing that contract.
- Keep implementation detail in the Go ownership tree instead of inventing alternate runtime surfaces.

## ANTI-PATTERNS
- Do not describe Prism as a mixed-runtime backend.
- Do not point readers to retired backend runtime surfaces as current implementation paths.
- Do not invent unsupported providers, routes, or CI jobs.
- Do not describe all bootstrap writes as restart-only. Distinguish hot-eligible fields from restart-required fields.
- Do not bypass `internal/platform/logretention/` with ad hoc log cleanup, retention SQL, or partition creation outside runtime partition ensuring.
- Do not change container bootstrap defaults or writable ownership contracts without updating Dockerfile tests and docs.
- Do not treat enabled-but-invalid SMTP as recoverable no-op delivery.
