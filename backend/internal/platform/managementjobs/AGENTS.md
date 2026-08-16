# Management Jobs Ownership

`managementjobs/` owns low-priority durable management work. `jobs.go` registers the scheduler and preserves the legacy audit-delete executor; `jobs_v2.go` owns global `log_retention` planning, claims, checkpoints, protection gates, fenced physical reclaim, recovery, and terminal publication; `retention_preflight.go` owns the worker-side sealed-preflight recheck.

## Local rules

- Keep `log_retention` global (`profile_id = 0`) and operation-registered through the Settings job-center list/read/cancel contract. Handlers enqueue durable work; this package is the only owner allowed to drop partitions or delete retention boundary rows.
- Automatic jobs use UTC day-aligned policy cutoffs, per-dataset policy and semantic fence generations, the Observe protection contract or Requests/Audit-owned fence, and a final publication step. Do not derive a cutoff from `now - N*24h`, use a second coverage owner, or count dropped-partition rows as `rows_deleted`.
- A manual job created by Settings must retain its sealed `preflight_id`. Before a fresh purge enters `purge_state = running`, lock the dataset resource and compare the current owner semantic snapshot with the sealed snapshot. A mismatch terminalizes the job as `preflight_stale_before_execution` without acquiring the purge fence or retrying deletion; recovery retries reuse only their existing fence.
- Keep append-only coverage evidence separate from semantic policy/floor/epoch/fence/materializer state. Coverage reads and final publish must use `internal/domain/stats/retention_source.go`; audit reader/materializer admission and fences must use `internal/domain/audit/retention_fence.go`.
- Durable job and operation identifiers, checkpoint/progress accuracy, cancellation rules, and error states must remain truthful across lease expiry, crash, retry, and response loss. Never expose secrets or replace unavailable facts with zero/success.

## Verification

Use the package tests for local changes, then the PostgreSQL-backed retention regressions when Docker is available:

```bash
cd backend && go test ./internal/platform/managementjobs
cd backend && go test ./tests/integration -run '^TestManualRetentionPreflightStaleBeforeExecutionIsTerminal$' -count=1
```
