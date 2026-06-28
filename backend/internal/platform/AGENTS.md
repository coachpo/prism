# BACKEND PLATFORM KNOWLEDGE BASE

## OVERVIEW
`backend/internal/platform/` owns backend process infrastructure beneath `cmd/prism-backend`: config loading and hot bootstrap runtime, lifecycle assembly, HTTP server wiring, migrations and startup seeding, DB lane ownership, scheduler-backed work, partitioned log retention, side-effect dispatch, email delivery, CORS, and version metadata.

## STRUCTURE
```text
platform/
├── admission/               # Admission budgets and protected lane policy
├── asyncmetrics/            # Worker-side metric helpers
├── background/              # Scheduler, worker specs, coalescing, drain behavior
├── bodylimits/              # Shared request body-size limits
├── config/                  # Plaintext bootstrap config, validation, hot-apply planning
├── cors/                    # Runtime CORS snapshot matching
├── db/                      # Named PostgreSQL pool lanes
├── email/                   # Mailer construction and durable email outbox
├── http/                    # Server assembly, router mounting, hot bootstrap runtime
├── lifecycle/               # Production dependency assembly and shutdown order
├── logretention/            # Partitioned log horizon, retention, and maintenance worker
├── managementjobs/          # Low-priority management job store
├── managementsideeffects/   # After-commit side-effect dispatcher
├── migrate/                 # SQL migration runner and schema guards
├── priority/                # Priority metadata and context helpers
├── startup/                 # Startup sequencing, seeds, and secret normalization
├── telemetry/               # OTLP provider setup and shutdown
└── version/                 # Backend version loading
```

## WHERE TO LOOK
- Production dependency graph, service registration, runtime cache bootstrap, scheduler start, and shutdown order: `lifecycle/production.go`, `lifecycle/app.go`
- Router mounting, middleware, `/health`, `/api`, `/v1`, `/v1beta`, startup-JSON OTLP providers, and hot bootstrap runtime snapshots: `http/AGENTS.md`, `http/server.go`, `http/management_branch.go`, `http/runtime_branch.go`, `http/dependencies.go`, `http/hot_bootstrap_runtime.go`, `telemetry/`, `db/telemetry.go`
- Shared body-size limits used by management/runtime HTTP: `bodylimits/`, `http/management_body_limits.go`
- Management route profile-scope and runtime-cache invalidation contract: `http/management_route_contract.json`, consumed by frontend and backend drift tests
- Plaintext bootstrap contract, restart-required fields, hot-apply publishing, and safe secret metadata: `config/AGENTS.md`, `config/`
- Startup migration and seed flow: `startup/`, `migrate/`, `../../migrations/`
- DB lane budgets and pool handles: `db/`
- Partitioned log retention, daily partition horizon, retention deletes, and low-priority maintenance worker: `logretention/`, `managementjobs/jobs.go`
- Background worker contracts and side-effect stores: `background/`, `managementsideeffects/`, `managementjobs/`, `email/outbox/`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing surface is itself a shipped contract or guardrail.
- Keep `lifecycle/` as the production composition boundary. Feature services are wired there, while handlers and domain packages stay outside platform.
- Keep steady-state startup settings in the plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; `DATABASE_URL` is a bootstrap seeding override, not a general runtime config channel.
- Keep hot-eligible bootstrap state behind `http.HotBootstrapConfigRuntime`; it publishes CORS, auth, mail, runtime proxy transport, and admission snapshots without restarting the process. `runtime.transport.requestTimeout` is part of the hot-applicable runtime transport snapshot.
- Keep listener, database URL, pool budgets, runtime side-effect attempt timeout, runtime secret encryption key, JWT signing key, and state-transfer bundle key restart-required. `runtime.sideEffects.attemptTimeout` stays restart-required and is not hot-applied.
- Keep backend canonical defaults as the source of truth for fresh bootstrap seeds: server `0.0.0.0:8000`, standalone database URL `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, CORS `5173`, PostgreSQL pool total `24`, split `4/8/4/2/2/2/2`, transport `100/16/16/300s/90s/0s/10s/1s`, side-effect timeout `10s`, and admission `3/2`. Runtime buffering is automatic and internal.
- Preserve existing valid bootstrap files during startup. To reset defaults, stop Prism, remove or relocate the bootstrap file, then restart so the missing-file seed path runs.
- Keep database capacity lane-specific. Runtime execution, telemetry, feedback, management, realtime, cache refresh, and background jobs must not borrow each other's protected budgets.
- Keep request-path side effects on scheduler workers, durable outboxes, or after-commit wakeups.
- When changing management route profile-scope or runtime-cache invalidation semantics, update `http/management_route_contract.json` with the code change instead of duplicating route expectations in frontend or backend tests.
- Keep partitioned log-retention work on `logretention.Store` plus the low-priority `log_partition_maintenance` worker. Managed tables are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- Keep retention jobs low-priority and management-owned through `managementjobs/`; handlers should enqueue jobs, not run partition cleanup inline.
- Keep shutdown sequencing explicit: HTTP shutdown, realtime shutdown, side-effect drain, scheduler stop, service close, telemetry shutdown, then DB close.
- Keep migrations fresh-install-only and schema-history-aware. Existing app tables without the current `prism_schema_migrations` baseline must fail fast instead of rewriting historical schemas.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not put provider sends, cache invalidations, dashboard publishes, or email delivery inline on request paths.
- Do not treat external bootstrap file edits as watched state; use the Startup tab or bootstrap API to publish hot-eligible changes.
- Do not make enabled-but-invalid SMTP recover into no-op delivery.
- Do not run log cleanup, partition drops, or horizon creation outside `logretention/` and the scheduler/job ownership seams.
- Do not collapse named database lanes into a single shared pool when changing runtime, realtime, management, or background work.
