# BACKEND PLATFORM KNOWLEDGE BASE

## OVERVIEW
`backend/internal/platform/` owns backend process infrastructure beneath `cmd/prism-backend`: config loading and hot bootstrap runtime, lifecycle assembly, HTTP server wiring, migrations and startup seeding, DB lane ownership, scheduler-backed work, side-effect dispatch, email delivery, CORS, and version metadata.

## STRUCTURE
```text
platform/
├── admission/               # Admission budgets and protected lane policy
├── background/              # Scheduler, worker specs, coalescing, drain behavior
├── config/                  # Plaintext bootstrap config, validation, hot-apply planning
├── cors/                    # Runtime CORS snapshot matching
├── db/                      # Named PostgreSQL pool lanes
├── email/                   # Mailer construction and durable email outbox
├── http/                    # Server assembly, router mounting, hot bootstrap runtime
├── lifecycle/               # Production dependency assembly and shutdown order
├── managementjobs/          # Low-priority management job store
├── managementsideeffects/   # After-commit side-effect dispatcher
├── migrate/                 # SQL migration runner and schema guards
├── priority/                # Priority metadata and context helpers
├── startup/                 # Startup sequencing, seeds, and secret normalization
└── version/                 # Backend version loading
```

## WHERE TO LOOK
- Production dependency graph, service registration, runtime cache bootstrap, scheduler start, and shutdown order: `lifecycle/production.go`, `lifecycle/app.go`
- Router mounting, middleware, `/health`, `/metrics`, docs routes, and hot bootstrap runtime snapshots: `http/server.go`, `http/hot_bootstrap_runtime.go`
- Plaintext bootstrap contract, restart-required fields, hot-apply publishing, and safe secret metadata: `config/`
- Startup migration and seed flow: `startup/`, `migrate/`, `../../migrations/`
- DB lane budgets and pool handles: `db/`
- Background worker contracts and side-effect stores: `background/`, `managementsideeffects/`, `managementjobs/`, `email/outbox/`

## CONVENTIONS
- Keep `lifecycle/` as the production composition boundary. Feature services are wired there, while handlers and domain packages stay outside platform.
- Keep hot-eligible bootstrap state behind `http.HotBootstrapConfigRuntime`; it publishes CORS, auth, mail, runtime proxy transport, and admission snapshots without restarting the process.
- Keep listener, docs, database URL, pool budgets, runtime side-effect attempt timeout, runtime secret encryption key, JWT signing key, and state-transfer bundle key restart-required.
- Keep database capacity lane-specific. Runtime execution, telemetry, feedback, management, realtime, cache refresh, and background jobs must not borrow each other's protected budgets.
- Keep request-path side effects on scheduler workers, durable outboxes, or after-commit wakeups.
- Keep shutdown sequencing explicit: HTTP shutdown, realtime shutdown, side-effect drain, scheduler stop, service close, then DB close.
- Keep migrations forward-only and schema-history-aware. Existing app tables without `prism_schema_migrations` must fail fast instead of relying on cutover bridges.

## ANTI-PATTERNS
- Do not put provider sends, cache invalidations, dashboard publishes, or email delivery inline on request paths.
- Do not treat external bootstrap file edits as watched state; use the Startup tab or bootstrap API to publish hot-eligible changes.
- Do not make enabled-but-invalid SMTP recover into no-op delivery.
- Do not collapse named database lanes into a single shared pool when changing runtime, realtime, management, or background work.
