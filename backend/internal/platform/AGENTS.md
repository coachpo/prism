# BACKEND PLATFORM KNOWLEDGE BASE

## OVERVIEW

`backend/internal/platform/` owns backend process infrastructure beneath `cmd/prism-backend`: config loading, startup runtime snapshots, lifecycle assembly, HTTP server wiring, migrations and startup seeding, DB lane ownership, scheduler-backed work, partitioned log retention, side-effect dispatch, CORS, and version metadata.

## STRUCTURE

```text
platform/
├── admission/               # Admission budgets and protected lane policy
├── alerting/                # Webhook incident outbox store
├── background/              # Scheduler, worker specs, coalescing, drain behavior
├── bodylimits/              # Shared request body-size limits
├── config/                  # Plaintext bootstrap config, validation, startup snapshots
│   └── AGENTS.md            # Bootstrap contract and restart-applied field rules
├── cors/                    # Runtime CORS snapshot matching
├── db/                      # Named PostgreSQL pool lanes
├── http/                    # Server assembly, router mounting, startup config runtime
│   └── AGENTS.md            # Route mounting, admission, and invalidation rules
├── lifecycle/               # Production dependency assembly and shutdown order
├── logretention/            # Partitioned log horizon, retention, and maintenance worker
├── managementjobs/          # Low-priority management job store
│   └── AGENTS.md            # Retention v2 fence, preflight, recovery, and job-state rules
├── managementsideeffects/   # After-commit side-effect dispatcher
├── migrate/                 # SQL migration runner and schema guards
├── priority/                # Priority metadata and context helpers
├── startup/                 # Startup sequencing, seeds, and secret normalization
│   └── AGENTS.md            # Startup seed/default and preservation rules
└── version/                 # Backend version loading
```

## WHERE TO LOOK

- Production dependency graph, service registration, runtime cache bootstrap, scheduler start, and shutdown order: `lifecycle/production.go`, `lifecycle/app.go`
- Router mounting, middleware, `/health`, `/api`, `/v1`, `/v1beta`, and startup config runtime snapshots: `http/AGENTS.md`, `http/server.go`, `http/management_branch.go`, `http/runtime_branch.go`, `http/dependencies.go`, `http/startup_config_runtime.go`
- Shared body-size limits used by management/runtime HTTP: `bodylimits/`, `http/management_body_limits.go`
- Management route registry (tier, profile scope, runtime-cache effect): `http/management_route_specs.go` 的 managementRouteSpecs；contract generator owner 是 `http/server_route_contract_test.go`，`http/management_route_contract.json` 是由它根据 registry 生成的产物，供前端漂移测试消费
- Management admission controller and middleware: `http/management_admission.go`; settings-schema guard: `http/settings_schema_guard.go`; runtime proxy admission: `http/runtime_proxy_admission.go`; typed admission errors: `http/admission_errors.go`
- Plaintext bootstrap contract, restart-applied fields, and safe secret metadata: `config/AGENTS.md`, `config/`
- Startup migration and seed flow: `startup/AGENTS.md`, `startup/service.go`, `startup/{profiles.go,user_settings_seed.go,app_auth_settings_seed.go,user_agent_client_rule_seed.go,header_blocklist_seed.go,strategies.go,audit_settings_seed.go,retention_coverage_seed.go}`, `migrate/`, `../../migrations/`
- DB lane budgets and pool handles: `db/`
- Partitioned log retention, daily partition horizon, retention deletes, and low-priority maintenance worker: `logretention/`, `managementjobs/jobs.go`
- Retention planning/execution, separate policy/fence generations, manual purge fence and final publish, legacy drain, and global job DTOs: `managementjobs/jobs.go`, `managementjobs/retention_planning.go`, `managementjobs/retention_execute.go`, `managementjobs/retention_legacy.go`, `managementjobs/retention_api.go`, `managementjobs/retention_dto.go`
- Shared retention source projection (per-dataset cutoff/floor/epoch/purge state): `../../internal/domain/stats/retention_source.go`
- Background worker contracts and side-effect stores: `background/`, `managementsideeffects/`, `managementjobs/`

## CONVENTIONS

- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.

- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing surface is itself a shipped contract or guardrail.
- Keep `lifecycle/` as the production composition boundary. Feature services are wired there, while handlers and domain packages stay outside platform.
- Keep steady-state startup settings in the plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; `DATABASE_URL` is a bootstrap seeding override, not a general runtime config channel.
- Keep bootstrap state behind `http.StartupConfigRuntime`; it provides CORS, auth, runtime proxy transport, and admission snapshots seeded at startup. The snapshot is built once at startup and is not replaceable in the running process; external file edits take effect only on restart.
- Keep listener, database URL, pool budgets, runtime side-effect attempt timeout, runtime secret encryption key, JWT signing key, CORS, auth TTL/cookie metadata, management admission, and parse-compatible telemetry fields restart-applied.
- Keep backend canonical defaults as the source of truth for fresh bootstrap seeds: server `0.0.0.0:8000`, standalone database URL `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, CORS `5173`, PostgreSQL pools and admission derived from CPU count via `unit = clamp(GOMAXPROCS, 8, 16)` (management `unit+1`, execution `unit`, telemetry `unit/2`, feedback/cache/jobs `unit/4`, total = lane sum 27–53, admission m2 `unit` / m3 `unit/2`; see `config/config.go` `derivedPoolUnit`), and side-effect timeout `10s`. The `runtime.transport` section was removed outright: outbound provider requests carry no connection or timeout limits, and a leftover `runtime.transport` block fails startup with a readable migration error. Runtime buffering is automatic and internal.
- Preserve existing valid bootstrap files during startup. To reset defaults, stop Prism, remove or relocate the bootstrap file, then restart so the missing-file seed path runs.
- Keep database capacity lane-specific. Runtime execution, telemetry, feedback, management, cache refresh, and background jobs must not borrow each other's protected budgets.
- Keep request-path side effects on scheduler workers, durable outboxes, or after-commit wakeups.
- 新增或修改管理路由时改 `http/management_route_specs.go` 的 managementRouteSpecs，然后跑 `go test ./internal/platform/http -run TestManagementRouteContractMatchesRouteSpecs -update-route-contract` 重新生成契约文件；不要手工编辑该 JSON。
- Keep partitioned log-retention work on `logretention.Store` plus the low-priority `log_partition_maintenance` worker. Managed tables are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- Keep retention jobs low-priority and management-owned through `managementjobs/`; handlers should enqueue jobs, not run partition cleanup inline.
- Keep v2 jobs durable and recoverable: automatic work waits behind a manual reservation, claims bind the current policy/fence/cutoff, `purge_to_time` is frozen at the execution fence for delete-all, and final publish is the only owner transition that advances visibility/revocation.
- Keep shutdown sequencing explicit: HTTP shutdown, side-effect drain, scheduler stop, service close, then DB close.
- Keep migrations fresh-install-only and schema-history-aware. Existing app tables without the current `prism_schema_migrations` baseline must fail fast instead of rewriting historical schemas.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX

- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not put provider sends, cache invalidations, or dashboard publishes inline on request paths.
- Do not treat external bootstrap file edits as watched or hot-published state.
- Do not run log cleanup, partition drops, or horizon creation outside `logretention/` and the scheduler/job ownership seams.
- Do not collapse named database lanes into a single shared pool when changing runtime, management, or background work.
