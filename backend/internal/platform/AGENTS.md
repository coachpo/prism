# Platform Ownership

`lifecycle/production.go` is the production composition boundary; `lifecycle/app.go` owns shutdown. Keep feature handlers and provider behavior outside platform assembly.

Read local guidance for [bootstrap](config/AGENTS.md), [HTTP assembly](http/AGENTS.md), [startup](startup/AGENTS.md), and [management jobs](managementjobs/AGENTS.md).

- `db/` allocates named pool lanes; production wiring must pass each service the pool for its responsibility. Preserve protected execution, telemetry, feedback, management, cache-refresh, and background-job capacity.
- `background/` owns worker registration, scheduling, coalescing, and draining. Use durable stores and `managementsideeffects/` after-commit dispatch; do not add inline fallback execution for background side effects.
- `logretention/` owns partition maintenance and the low-priority horizon worker. Retention handlers enqueue work in `managementjobs/`; physical reclaim and visibility publication must retain those ownership boundaries.
- `migrate/runner.go` applies pending SQL from `../../migrations/`, rejects missing/mismatched baselines and histories ahead of the binary, and enforces migration provenance. Preserve each migration's retained-data guard; migrations are not uniformly fresh-install-only.
- Preserve shutdown order: HTTP shutdown, side-effect drain, scheduler stop, service close, then database close. New resources need an explicit close/drain owner.
