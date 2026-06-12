# BACKEND KNOWLEDGE BASE

## OVERVIEW
`backend/` is Prism's monorepo-owned Go backend tree. The live runtime is compiled from `cmd/prism-backend` and owns Prism's management API, operation-registered runtime proxy, realtime delivery, platform lifecycle, startup sequencing, SQL migrations, priority isolation, and durable background side effects.

## STRUCTURE
```text
backend/
├── cmd/prism-backend/          # Go process entrypoint
├── internal/{platform,gateway,httpapi}/AGENTS.md
├── internal/{domain,endpointdomain,profiledomain,vendordomain,pgxutil}/
├── migrations/                 # Fresh-install SQL baseline
├── testdata/                   # Regression fixtures
├── tests/AGENTS.md             # Go regression ownership
├── Dockerfile                  # Backend container contract
└── VERSION
```

## CHILD DOCS
- `internal/platform/AGENTS.md`: lifecycle, hot bootstrap runtime, DB lanes, scheduler, migrations, log retention, and side effects.
- `internal/gateway/AGENTS.md`: preserved gateway contracts, hooks, records, adapters, routing, reservations, and accounting.
- `internal/httpapi/AGENTS.md`: mounted management, runtime, realtime, proxy-key usage, retention jobs, and request context.
- `internal/httpapi/{runtime,realtime}/AGENTS.md`: operation registry, hook residency, telemetry/feedback, partitions, websocket delivery, and publishers.
- `internal/httpapi/management/*/AGENTS.md`: auth, bootstrap config, config bundles, routing config, endpoints, models, profiles, settings, sidecars, stats, vendors, audit.
- `tests/AGENTS.md`: Go regression boundaries for route matrix, rejected routes, bootstrap config, Dockerfile, sidecars, and pool priority.

## RUNTIME FACTS
- `cmd/prism-backend/main.go` starts the backend and reseeds bootstrap files still carrying retired `docsEnabled`.
- `internal/platform/` owns lifecycle assembly, startup/migrations, hot bootstrap runtime, DB lanes, scheduler, retention, and side-effect workers.
- `internal/platform/http/server.go` mounts `/health`, `/api`, `/v1`, and `/v1beta`; exact runtime operations are allowlisted later by `internal/httpapi/runtime/operations.go`.
- `internal/platform/config/` owns the plaintext bootstrap contract; steady-state startup settings live there, while `PRISM_CONFIG_PATH` and optional `DATABASE_URL` remain bootstrap-only env exceptions.
- `internal/httpapi/management/` fans out into auth, bootstrapconfig, configbundle, configrules, connections, endpoints, loadbalance, models, profiles, settings, sidecars, stats, vendors, and audit.
- `internal/httpapi/runtime/` owns operation-registered ingress, model binding, hooks, context overflow promotion, telemetry outbox enqueue, request logging, `operation_name`, and partition ensuring.
- `internal/gateway/` owns provider-agnostic gateway contracts used by runtime execution: hook phases, envelopes, operation records, adapters, route planning, and reservations.
- `Dockerfile` builds from the monorepo root, copies migrations, runs as `prism:prism` (`1000:1000`), and defaults `PRISM_CONFIG_PATH` to `/app/config/config.json`.
- Bootstrap config v1 is plaintext and file-backed with backend-owned fresh defaults; valid existing files are preserved until manual reset. Enabled SMTP must validate at startup and must not silently fall back.

## WHERE TO LOOK
- Process entrypoint and startup flow: `cmd/prism-backend/main.go`, `internal/platform/AGENTS.md`, `internal/platform/migrate/`
- Gateway and runtime contracts: `internal/gateway/AGENTS.md`, `internal/httpapi/runtime/AGENTS.md`, `internal/httpapi/runtime/operations.go`
- HTTP mounting, realtime, management fanout, and request context: `internal/httpapi/AGENTS.md`, `internal/httpapi/realtime/AGENTS.md`, `internal/httpapi/management/*/AGENTS.md`
- Bootstrap/config bundle/settings/sidecars dense seams: `internal/httpapi/management/bootstrapconfig/AGENTS.md`, `internal/httpapi/management/configbundle/AGENTS.md`, `internal/httpapi/management/settings/AGENTS.md`, `internal/httpapi/management/sidecars/AGENTS.md`
- Stats, audit, loadbalance, transactions, partitions, and schema: `internal/domain/`, `internal/pgxutil/tx.go`, `migrations/`, `internal/platform/logretention/`
- Container and regression boundaries: `Dockerfile`, `tests/integration/dockerfile_contract_test.go`, `tests/AGENTS.md`, `tests/`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep backend docs focused on the live Go runtime.
- Keep SQL migrations under `migrations/` as the live schema source of truth for startup.
- Keep the runtime contract operation-registered. `internal/httpapi/runtime/operations.go` is the source of truth for supported method/path pairs, hook collections, and model-binding rules.
- Keep management selected-profile behavior separate from runtime active-profile routing; sidecar management is global instance state and does not use selected-profile scope.
- Keep `api_family` as runtime compatibility truth. Vendor rows and `icon_key` are presentation metadata.
- Keep bootstrap config separate from PostgreSQL-backed profile/vendor bundle import and export.
- Keep request-path side effects on durable outboxes, scheduler-owned workers, or after-commit wakeups; do not put provider sends, cache invalidations, or dashboard materialization inline.
- Keep database pool lane ownership explicit. Background, realtime, telemetry, feedback, management, cache refresh, and runtime execution lanes are separate capacity budgets.
- Keep partitioned log tables under `internal/platform/logretention/` and runtime partition ensuring; managed tables are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- Keep backend container execution non-root with writable config ownership under `/app/config`; update `tests/integration/dockerfile_contract_test.go` when changing that contract.
- Keep implementation detail in the Go ownership tree instead of inventing alternate runtime surfaces.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not describe Prism as a mixed-runtime backend.
- Do not point readers to retired backend runtime surfaces as current implementation paths.
- Do not invent unsupported providers, routes, or CI jobs.
- Do not describe mounted `/v1` and `/v1beta` prefixes as broad passthrough runtime support; the runtime allowlist lives in `internal/httpapi/runtime/operations.go`.
- Do not describe all bootstrap writes as restart-only. Distinguish hot-eligible fields from restart-required fields.
- Do not bypass `internal/platform/logretention/` with ad hoc log cleanup, retention SQL, or partition creation outside runtime partition ensuring.
- Do not change container bootstrap defaults or writable ownership contracts without updating Dockerfile tests and docs.
- Do not treat enabled-but-invalid SMTP as recoverable no-op delivery.
