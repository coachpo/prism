# Management Jobs

`scheduler.go` owns worker registration/dispatch; `jobs.go` owns the store facade. Keep profile-scoped `audit_delete` in `audit_delete.go`/`job_queries.go` separate from global `log_retention` work and the Settings job-center API in `retention_api.go`/`retention_dto.go`.

- `retention_planning.go`, `retention_create.go`, and `retention_execute.go` own policy-bound claims, checkpoints, fenced physical reclaim through the retention store, and terminal publication. Handlers enqueue work; they do not delete retention rows or drop partitions inline.
- Keep `log_retention` global (`profile_id = 0`). Automatic cutoffs are UTC-day aligned and claims bind separate policy/fence generations; automatic work waits behind manual reservations. Do not substitute `now - N*24h` or count dropped-partition rows as `rows_deleted`.
- `retention_preflight.go` rechecks a Settings manual job's sealed `preflight_id` against the current semantic owner snapshot under dataset lock before entering the purge fence. Drift terminalizes as `preflight_stale_before_execution` without deletion/retry; recovery reuses its existing fence.
- Freeze delete-all `purge_to_time` at the execution fence. Final publication alone advances the published visibility floor/revocation epoch; preserve checkpoint and cancel/error truth across lease expiry, restart, retry, and response loss.
- Append-only coverage is separate from semantic policy/floor/epoch/fence/materializer state. Use `../../domain/stats/retention_source.go` for source/coverage publication and `../../domain/audit/retention_fence.go` for Audit reader/materializer fencing.
- `retention_legacy.go` owns frozen contract-version-1 drain/supersede behavior. Keep persisted `contract_version`, `v2_exact`, `superseded_by_v2_planning`, and cursor domain separators unchanged during source refactors.

Run from `backend/` for local changes and the relevant PostgreSQL-backed boundary:

```bash
go test ./internal/platform/managementjobs
go test ./tests/integration -run '^TestManualRetentionPreflightStaleBeforeExecutionIsTerminal$' -count=1
```
