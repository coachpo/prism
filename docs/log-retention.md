# Log Retention

For public query and management endpoints, see the [Observability API](observability-api.md). The [data model](data-model.md) describes retained tables and ownership. [Priority enforcement](priority-enforcement.md) describes how retention jobs are admitted and drained.

## Global Log Retention

### Retention Concept

Historical `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events` are partitioned by UTC day and managed by global log-retention jobs. Normal retention is instance-wide across all profiles. Default-profile filtering still applies to profile-owned list and detail APIs, but `X-Profile-Id` does not scope retention settings or retention jobs.

The visible Settings `访问与数据` group (canonical `scope=instance&section=retention`) owns `/api/settings/log-retention` with a day count per retained dataset, the owner actual-coverage projection, fresh destructive preflight dialogs, and the durable job center (`scope=instance&section=retention-jobs`). Operators start a one-time cleanup through a fresh manual-cleanup preflight and a sealed manual job; manual queued jobs are cancellable, running manual purges are not.

Before the HTTP server starts, production ensures a 15-day horizon for every managed table: the current UTC day plus the next 14 days. The low-priority `log_partition_maintenance` worker refreshes that horizon hourly. Independently, the low-priority management-jobs worker runs every 5 seconds: it plans one idempotent UTC day-aligned v2 job per configured dataset (never `now - N*24h`), and processes due jobs under the per-dataset resource fence.

### Retention Job Flow

```
Operator -> Settings `访问与数据` group -> 自动保留策略
  -> Edits the four-field policy draft with PUT /api/settings/log-retention (operation_id + expected_revision)
  -> Destructive changes (enable/shorten) require POST /api/maintenance/log-retention/preflights
     (fresh token, exact affected-domain owner snapshots) + keyword DELETE
  -> PUT returns changes[] + scheduled_work[] (durable v2 job IDs)

Operator -> Settings `访问与数据` group -> 手动清理
  -> POST /api/maintenance/log-retention/preflights (manual_cleanup: keep_days|cutoff|delete_all)
  -> POST /api/maintenance/log-retention/jobs (operation_id + preflight_token + confirmation.keyword)
  -> Returns 202 with the created queued job (replayed=true on exact replay)
  -> Background worker executes the v2 job under the owning purge fence;
     delete-all freezes purge_to_time at the execution fence, running/recovery fail closed,
     final publish advances revocation epoch + published floor
  -> API callers list/detail/cancel through /api/management/jobs?scope=global&type=log_retention
```

### Partition Drop and Boundary Cleanup

Retention removes old partitions before it falls back to bounded row cleanup. Daily partitions use half-open UTC ranges, with `FROM` inclusive and `TO` exclusive. A whole child partition is expired when its upper bound is `<= cutoff`; those children are dropped as tables rather than cleaned with row deletes.

Only the single child partition that overlaps the cutoff receives a bounded delete for rows older than the cutoff. After that boundary delete, Prism runs `VACUUM (ANALYZE, PROCESS_TOAST TRUE)` on the boundary child so planner statistics and TOAST cleanup catch up without rewriting the whole table.

Operators can inspect managed root, child, and TOAST relation sizes with a read-only catalog query. Partitioned roots normally have `reltoastrelid = 0`; physical child partitions can own TOAST relations through `pg_class.reltoastrelid`.

```sql
WITH managed_roots(root_name) AS (
  VALUES
    ('request_logs'),
    ('audit_logs'),
    ('usage_request_events'),
    ('loadbalance_events')
)
SELECT
  parent.relname AS root_relation,
  parent.reltoastrelid::int8 AS root_reltoastrelid,
  pg_total_relation_size(parent.oid) AS root_total_bytes,
  pg_relation_size(parent.oid) AS root_main_bytes,
  child.relname AS child_partition,
  pg_get_expr(child.relpartbound, child.oid) AS child_partition_bound,
  child.reltoastrelid::int8 AS child_reltoastrelid,
  pg_total_relation_size(child.oid) AS child_total_bytes,
  pg_relation_size(child.oid) AS child_main_bytes,
  toast_ns.nspname AS toast_schema,
  toast.relname AS toast_relation,
  COALESCE(pg_total_relation_size(toast.oid), 0) AS toast_total_bytes,
  COALESCE(pg_relation_size(toast.oid), 0) AS toast_main_bytes
FROM managed_roots
JOIN pg_class parent ON parent.relname = managed_roots.root_name
JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
JOIN pg_inherits inheritance ON inheritance.inhparent = parent.oid
JOIN pg_class child ON child.oid = inheritance.inhrelid
JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
LEFT JOIN pg_class toast ON toast.oid = child.reltoastrelid
LEFT JOIN pg_namespace toast_ns ON toast_ns.oid = toast.relnamespace
WHERE parent_ns.nspname = 'public'
  AND child_ns.nspname = 'public'
ORDER BY parent.relname, child.relname;
```

If an operator performs manual bounded deletes against the cutoff-overlapping boundary child, the standard follow-up is the same child-only vacuum Prism uses automatically. Replace the child name with the actual boundary partition and do not run this against the partitioned root or already expired children.

```sql
VACUUM (ANALYZE, PROCESS_TOAST TRUE) public.request_logs_pYYYYMMDD;
```

Routine retention does not run parent-root bulk deletes. Operator-only shrink tools such as `VACUUM FULL`, `CLUSTER`, and `pg_repack` are manual or emergency database actions. They are not automatic retention steps, and `pg_repack` is not installed in the default local `postgres:16-alpine` image.

### Audit and Request Linkage

Audit rows keep weak request metadata rather than a hard dependency on live request-log rows. Request-log retention does not clear `request_log_id`, `request_log_created_at`, or `ingress_request_id`; audit APIs report `request_log_missing=true` only when both request-log link fields are non-null and the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves. Request detail links can therefore be unavailable after request-log retention expires before audit-log retention.

Deleting or expiring request logs does not delete audit rows. Deleting or expiring audit rows does not affect request logs. Operators should treat request-to-audit linking as best-effort historical context, not as guaranteed referential availability.

### Current Retention Boundaries

Partitioned retention manages the current log-table set only. Prism does not rewrite historical log storage shapes into the current partitions.

### Frontend Placement

Log retention controls live on the visible Settings `访问与数据` group (canonical `scope=instance`), with automatic policy + actual coverage, manual cleanup, and the retention job center. The job center is a static browser-side snapshot over the durable queue: it never polls; manual refresh and post-mutation calibration re-read the loaded pages serially with fresh cursors and swap atomically, while the management-jobs worker remains the only background processor. The visible `显示与记录` group (canonical `scope=global`) manages billing and reporting currency, timezone, audit & privacy, config rules, and other Default-profile state.


## Product Behavior

### Batch Data Deletion

Provide flexible bulk deletion of historical logs and statistics data to manage database growth.

- Supported Data Types: `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`
- The Settings UI offers only `1`, `7`, `30`, `90` days, or all data. The management API accepts an explicit cutoff timestamp for callers that need a custom range.
- Deletion requests create durable management jobs; the Settings retention job center is a static browser-side snapshot of the global v2 queue — it lists once per scope/filter, extends by explicit load-more, refreshes on demand, and recalibrates serially with fresh cursors after a policy save, job creation, or cancellation (all-or-nothing commit). It opens durable checkpoint details and cancels only queued manual jobs. The same list/get/cancel routes remain available as API contracts.
- Deleting `request_logs` does NOT delete linked `audit_logs`; audit rows retain weak request-log metadata, and audit reads expose `request_log_missing=true` only when both `request_log_id` and `request_log_created_at` are present but their profile-scoped tuple no longer resolves

