# Observability and Retention

For public query and management endpoints, see the [Observability API](observability-api.md). The [data model](data-model.md) describes retained tables and ownership.

## 7. Request Statistics

### 7.1 Concept

Materialized runtime activity creates retained, product-facing request history for analytics, debugging, spending, and dashboard views. The ingress and final-execution scopes use finalized usage events; route-attempt scope uses actual upstream request-log rows. Dataset coverage remains independent, so missing final-attempt history produces a missing latency sample rather than a fabricated value. This is not a guarantee that every ingress request or every upstream transport failure will have history.

### 7.2 Logging Flow

```
Client -> Operation registry -> Router / Planner -> Terminal target -> Endpoint -> Upstream
                                                         ↓
                                              Applicable telemetry handoff
                                                         ↓
  Buffered 2xx: completed durable outbox row before response commit
  Passthrough SSE or non-SSE 2xx: durable accepted row before first flush, then final payload update
  Captured non-2xx: bounded in-memory side-effect queue, then outbox attempt
  Eligible PlanningFailure or terminal execution error: bounded side-effect queue, then outbox attempt
                                                         ↓
                              Background materializer transaction:
                                - Write request_logs rows
                                - Write audit_logs rows when audit was enabled
                                - Write usage_request_events and proxy-key usage
                                - Delete the durable outbox row
```

Unsupported routes and wrong methods are rejected before telemetry. Early request/planning errors such as malformed bodies, unknown models, and API-family mismatches do not carry `PlanningFailure` and therefore do not create synthetic history. Eligible planning failures, captured non-`2xx` activity, and terminal execution errors use the bounded scheduler handoff and can be lost before they reach the outbox. A final all-transport-failures `502` preserves each launched attempt and its safe diagnostic in that handoff; a zero-launch execution failure uses a synthetic admission row. Retained history is therefore a materialized record, not a guaranteed ledger of every ingress.

### 7.3 Data Captured

- Profile ID attribution, requested model ID, final logical target model ID, the Terminal Target's frozen `upstream_model_id`, API family, Terminal Target compatibility ID, endpoint base URL, and endpoint description
- Prism `ingress_request_id`, per-request `attempt_number`, persisted ingress `operation_name`, additive `upstream_operation_name`, `operation_translation_mode`, `upstream_request_path`, and best-effort `provider_correlation_id`
- Scoped HTTP status (`upstream_status_code`/`gateway_status_code`/`legacy_status_code`) and scoped duration (`COALESCE(completion_duration_ms, attempt_duration_ms)` for upstream rows; `legacy_duration_ms` for other rows); the old un-scoped `status_code`/`response_time_ms` are nullable legacy projections only and are never written by the current runtime writer
- `row_kind` (`planning|admission|upstream|legacy_unknown`), `attempt_trigger`/`attempt_result`/`is_winner` lifecycle facts, and the unified failure projection: `error_source`/`error_code`/`failure_stage` plus a scrubbed, 4 KiB-bounded `error_detail` and an independent `stream_error_kind`/`stream_error_detail` with redacted/truncated flags
- Token usage (input, output, total), extracted by native upstream operation response or stream hooks
- Flat final-target attribution, including requested model, resolved target model, selected terminal target, endpoint, operation, upstream operation, current-or-historical translation mode, and sanitized upstream request path.
- Four-state pricing (`pricing_status`), `unpriced_reason`, canonical `pricing_resolution_kind`/`missing_price_components`, `pricing_evidence_trust`, template identity snapshots, reporting-currency epoch, and `cost_segment_key`; `priced_flag`/`billable_flag` are not part of the current contract
- Stream flag, ingress request path, sanitized upstream request path, error details, scrub provenance arrays (`metadata_redacted_fields`/`metadata_truncated_fields`), and `url_scrub_provenance`

Request-log semantics are per-materialized attempt: one incoming runtime request can create multiple request-log rows when failover or retries occur. `ingress_request_id` groups those rows while `request_id` remains the unique identifier for one stored attempt row. Final usage ownership stays with the final returned response. The default Requests view is the server-side retained ingress chain (`view=ingress_chains`): finalized usage evidence (`usage_request_events`), expected/retained counts, routing evidence flags (`same_target_retry_occurred`/`hedge_occurred`/`failover_occurred`), and signed opaque chain/row cursors that never split an ingress across pages.

### 7.4 Query Capabilities

- Filter by model, final target model, caller client rule, endpoint, api family, status family/exact status, error text, pricing status (`priced|unpriced|ineligible|unknown`), unpriced reason, and time range; unknown query keys return `422 unknown_query_key` (the old `priced` boolean alias is rejected)
- Attempt view (`view=attempts`) with scoped status/duration filters and `sort_by` over `created_at|display_status|ttft_ms|total_tokens|total_cost_user_currency_micros`; rows with no value for the selected key (no TTFT on a non-stream row, no cost on an unpriced row) sort last in both directions, and `created_at`/`id` break ties so offset pages stay stable
- Ingress-chain view (`view=ingress_chains`, default) with cohort filters (`ingress_final_result`, `confirmed_failover`, pricing cohort), whole-ingress outer pagination via signed chain cursors, and bounded retained-row inner pages with row cursors; the ordinary ingress set is the finalized usage events merged with the chains that have retained rows but no usage event (request-only chains stay visible with an unavailable finalized summary), and rows with a NULL `ingress_request_id` never form a chain
- Server-side full filtered CSV export (`GET /api/stats/requests/export`) from a single `REPEATABLE READ` snapshot with 100,000-row/128 MiB/31-day bounds, formula-injection escaping, SHA-256 digest verification, and no partial files
- Exact v2 detail (`GET /api/stats/requests/{request_id}`) with scoped statuses, the unified failure projection, canonical terminal-target/endpoint refs, routing provenance, pricing layers, and `legacy_pricing_evidence` for legacy-untrusted rows
- Cost segment catalogue (`GET /api/stats/cost-segments`, `/symbols`) with canonical `e.N`/`l.AAA`/`l.__unknown__` keys
- Aggregated statistics with grouping by model/api family/endpoint using stored endpoint label snapshots
- Pagination for request log listing

All Requests list/detail/chain/export responses send `Cache-Control: private, no-store` with profile-sensitive `Vary`. These query APIs intentionally remain product-facing retained-history surfaces for the UI and operators. Prism no longer exposes local metrics or tracing exporters.

## 8. Request Audit Logging

### 8.1 Concept

Audit logging records request-time provenance without changing routing choices or client-facing response handling. Before the ordinary backend outbox, it applies the fixed safe-diagnostic scrub bottom line (Bearer/Basic/JWT/API-key-like/URL-secret redaction from `safediag`) plus the request-time effective Header Blocklist, using the shared matcher in `internal/domain/safediag`. Canonical sorted `[{name,value}]` header entries retain per-direction scrub provenance. Runtime snapshots load audit policy from `profile_api_family_audit_settings` by profile and model API family, then retain request-time booleans in the telemetry envelope. Materialization creates one audit row for each audited upstream attempt, including failover attempts, and metadata-only requests still create audit metadata when audit is enabled. Body capture is allowed only when audit is enabled for that family; request bodies may be captured per attempted upstream request while response body capture is associated with the final attempt.

Captured bodies persist as **BYTEA byte-exact stored prefixes** (`request_body`/`response_body`, migration `000010_request_logs_audit_observability`); the telemetry envelope carries `[]byte` (base64 JSON) so bytes never round-trip through TEXT. Capture is bounded by a per-body 4 MiB cap and per-ingress budgets: request copies 12 MiB, final response 4 MiB, scrubbed header blocks 64 KiB with 1 MiB per direction (response reserves 64 KiB for the final winner). Each audit row records ingress and per-direction byte counters, typed capture/truncation statuses and the enumerated limit reasons; allocation follows immutable launch order and budget exhaustion only stops extra audit storage, never proxy traffic.

Image operations redact their audit bodies before persistence. `b64_json`, `image`, and `mask` values are replaced with `[redacted image bytes]`, and an `image_url` value is redacted only when it is an inline `data:` URL; short https URLs and uploaded file ids stay readable so an audited edit request remains reproducible. Streamed image responses are redacted event by event so the event names, partial-image indices, and the terminal `usage` object survive. A body that does not parse — a stream truncated mid-event, or a JSON document cut off by the 4 MiB cap — is replaced wholesale rather than stored partially. The capture counters (`observed`, `stored`, `truncated`) keep describing the bytes seen on the wire: redaction is a separate transformation applied on the way to persistence.

`GET /api/audit/logs/{id}/body/request` and `/body/response` return the stored bytes as `attachment`, `application/octet-stream`, `nosniff`, `private, no-store`, and `Content-Security-Policy: sandbox`, with a fixed safe `.bin` filename and exact `Content-Length`. A missing audit row returns `404`, disabled request-time audit returns `409`, and evidence below the published retention floor returns `410 audit_evidence_revoked`; a retained row with no captured body currently returns an empty body. The audit list accepts `anchor_id`: when the anchored row falls outside the first page the first response carries it exactly once as `anchor_item`, and in-page or unknown anchors emit no `anchor_item`. Successful Requests/Audit list responses carry a same-snapshot `coverage` projection (`known|legacy_unknown`, requested/effective/retained bounds, completeness, gaps and source revision) from the owning actual-coverage read model. Requests attempts/chains and CSV export accept `time_range=1h|6h|24h|7d|30d|all|custom`; `all` and the effective SQL lower bound use owner materialization, while a dirty/stale/gapped projection is explicit `legacy_unknown`. Windows reaching before the retention floor or actual retained intersection never enter true-empty.

### 8.2 Audit Flow

```
Client -> supported runtime operation
  -> Operation registry and planner resolve the attempt
  -> ProxyService forwards request to upstream
  -> Runtime captures audit metadata in the telemetry envelope
  -> Durable telemetry materialization, when reached:
       -> One audit row for each audited upstream attempt
       -> Persist the already-scrubbed request/response headers and connection metadata snapshots
       -> Link weakly to materialized request-log metadata when available
       -> Store immutable profile_id attribution
       -> Store request bodies when body capture is enabled
       -> Store the response body only for the final attempt
```

### 8.3 Non-Interference Guarantees

- Audit policy and body capture never change routing, target selection, or client-facing response adaptation.
- Audit materialization occurs with durable runtime telemetry rather than inline `INSERT`s in the proxy handler.
- Telemetry handoff has its own durability rules: buffered successful responses require a completed outbox handoff before response commit; passthrough SSE and non-SSE success require an accepted outbox row before first flush and a later final update. Asynchronous captured non-`2xx` and eligible synthetic failure activity can be rejected or abandoned before materialization.

### 8.4 Redaction

Applied at write time before INSERT:

- Header values are scrubbed with the fixed safe-diagnostic matcher (exact sensitive names plus fragment-sensitive names) and stored as canonical sorted `[{name,value}]` entries with `request_headers_scrub_provenance`/`response_headers_scrub_provenance`.
- Body capture is bounded: 4 MiB per body enforced during the copy, a 12 MiB shared request-body budget per ingress with a separate 4 MiB final-response reservation (16 MiB aggregate), and typed `captured|truncated|omitted_ingress_budget` statuses with observed/stored byte counts.
- Text body capture can contain sensitive user data and remains an explicit request-time setting. Image-operation bodies apply the payload redaction described in §8.1 before persistence.
- Stored bodies are BYTEA prefixes; raw downloads (`/api/audit/logs/{log_id}/body/request|response`) return the exact stored bytes with attachment/octet-stream/nosniff/no-store/sandbox headers and byte-exact round-trip for invalid UTF-8, NUL, and mid-codepoint truncation.

### 8.5 Dedicated Audit Detail Page

The request-content view is reached from request investigation at `/observe/requests/:requestId/audit`, retaining the existing audit API and route identity. It shows the request outcome, readable messages extracted from recognized stored request/response formats, natural-language tool activity, and capture limitations. Raw headers, JSON/SSE envelopes, tool arguments/results, and raw-download controls are absent from the UI. Invalid-UTF-8, binary, unsupported, missing, and truncated bodies keep explicit explanations and byte/capture evidence instead of manufactured conversation text. The backend still retains and serves its documented audit byte representation; recognized captured text may contain user-provided sensitive content. The overview-only sheet does not load bodies, and the content page preserves the original request-list state through a validated local `return_to` target.

### 8.6 Upstream Response Encoding

Prism's upstream transport sets `DisableCompression: true`, removes the client `Accept-Encoding` header, and applies no connection or timeout limits. `MaxIdleConnsPerHost` is explicitly unlimited (`math.MaxInt32`) rather than left at Go's default of two idle connections per host; all other connection counts, idle lifetimes, and the client timeout are zero/unlimited after the removal of the `runtime.transport` config section. Prism neither asks Go to automatically decompress upstream responses nor conditionally decompresses them for audit capture. Audit captures the response bytes actually read from the native upstream response.

## 9. Global Log Retention

### 9.1 Concept

Historical `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events` are partitioned by UTC day and managed by global log-retention jobs. Normal retention is instance-wide across all profiles. Default-profile filtering still applies to profile-owned list and detail APIs, but `X-Profile-Id` does not scope retention settings or retention jobs.

The visible Settings `访问与数据` group (canonical `scope=instance&section=retention`) owns `/api/settings/log-retention` with a day count per retained dataset, the owner actual-coverage projection, fresh destructive preflight dialogs, and the durable job center (`scope=instance&section=retention-jobs`). Operators start a one-time cleanup through a fresh manual-cleanup preflight and a sealed manual job; manual queued jobs are cancellable, running manual purges are not.

Before the HTTP server starts, production ensures a 15-day horizon for every managed table: the current UTC day plus the next 14 days. The low-priority `log_partition_maintenance` worker refreshes that horizon hourly. Independently, the low-priority management-jobs worker runs every 5 seconds: it plans one idempotent UTC day-aligned v2 job per configured dataset (never `now - N*24h`), and processes due jobs under the per-dataset resource fence.

### 9.2 Retention Job Flow

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

### 9.3 Partition Drop and Boundary Cleanup

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

### 9.4 Audit and Request Linkage

Audit rows keep weak request metadata rather than a hard dependency on live request-log rows. Request-log retention does not clear `request_log_id`, `request_log_created_at`, or `ingress_request_id`; audit APIs report `request_log_missing=true` only when both request-log link fields are non-null and the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves. Request detail links can therefore be unavailable after request-log retention expires before audit-log retention.

Deleting or expiring request logs does not delete audit rows. Deleting or expiring audit rows does not affect request logs. Operators should treat request-to-audit linking as best-effort historical context, not as guaranteed referential availability.

### 9.5 Current Retention Boundaries

Partitioned retention manages the current log-table set only. Prism does not rewrite historical log storage shapes into the current partitions.

### 9.6 Frontend Placement

Log retention controls live on the visible Settings `访问与数据` group (canonical `scope=instance`), with automatic policy + actual coverage, manual cleanup, and the retention job center. The job center is a static browser-side snapshot over the durable queue: it never polls; manual refresh and post-mutation calibration re-read the loaded pages serially with fresh cursors and swap atomically, while the management-jobs worker remains the only background processor. The visible `显示与记录` group (canonical `scope=global`) manages billing and reporting currency, timezone, audit & privacy, config rules, and other Default-profile state.

### 3.8 Dashboard And Analytics REST Polling

The Observe page reads independent REST fragments on initial load, relevant URL/filter changes, and explicit refresh. `frontend/src/features/observe/useObserveFragments.ts` owns the fixed-ingress query context, usage summary and Now strip; `useObserveAnalysisContext` and `useObserveSeries.ts` own the selected-scope analysis context and chart. Superseded reads are aborted and generation-fenced. This page does not install a 30-second polling timer or consume the older dashboard aggregate/recent-activity feed.

```text
Proxy execution or response capture
  -> Durable success handoff or scheduler-owned failure handoff
  -> Background materialization writes retained request logs and usage events
  -> Observe obtains query contexts from the owning retained-coverage projections
  -> Independent summary, Now, chart, errors and activity reads consume those facts
```

- `GET /api/stats/query-context` resolves a preset/custom window into signed opaque `query_context` (HMAC subkey derived from the server secret encryption key, 24h TTL) using the Observe owner actual-coverage projections for request logs, usage events, and loadbalance events. The token freezes per-domain requested/effective bounds, retention epoch/generation/fence, source revision, coverage revision/hash, materialization cut, freshness, and gaps; `all` uses the owner earliest bound and an empty half-open interval when no retained intersection exists. Fragments never re-parse presets or synthesize a policy-day window. Fragment validation re-reads the owning retention source: a running/recovery purge fails closed with the owning 503 and a manual-purge final publish revokes older tokens with `410 dataset_snapshot_revoked`.
- `GET /api/stats/usage-summary?query_context=` consumes the frozen token bounds directly, including `all` windows longer than 30 days; it never reinterprets them as a browser `custom` preset. Its scope-discriminated response initializes arrays/maps, names the correct outcome and latency basis, carries trusted-zero-aware cost samples only where the scope owns cost, preserves selected-card `pricing_card_role_breakdown`, and distinguishes zero from missing evidence.
- `GET /api/stats/usage-series?query_context=&metric=&group_by=&interval=` returns the scoped main chart with 24–120 buckets and Top entities plus a re-aggregated Other. Counts and raw latency samples are conserved before percentile calculation; exact capacity is not `truncated`. Endpoint/Terminal Target labels use the catalog/retained identity resolver rather than bare numeric IDs. A zero-length owner-complete `all` interval returns an empty chart, not an invalid-range error.
- `GET /api/stats/usage-errors?query_context=&group_by=&limit=` validates every scope/filter enum before SQL, binds all values as parameters, and uses the same outcome classifier as summary and Requests. Route-attempt mode reads upstream attempts only, distinguishes HTTP/stream/transport/disconnect outcomes, honors scope filters and limit, emits non-null replayable `request_filters`, and computes Other from the full denominator.
- `GET /api/stats/dashboard/now` returns the Now strip: 30-minute rolling RPM/TPM with token sample coverage plus enabled model count.
- `GET /api/loadbalance/events` accepts an optional `model_id` (empty selects the profile global timeline).
- The outcome classifier (`completed / http_error / stream_error / client_disconnected` detail and `completed / failed / client_disconnected` final result) and the pricing four-state classifier are implemented once and shared by all read models; SQL CASE expressions mirror the Go pure functions.
- Failures never produce synthetic zeros: fragments keep independent loading/ready/error states, 422/410 typed errors for invalid/expired query contexts, and null values for missing samples. Scoped request coverage is presented independently from the fixed-ingress usage lane, and JSON export carries per-fragment scope, bounds, caliber and dataset coverage rather than one mixed top-level claim.
- Fragment list fields (series, timeline, error rankings, stream error kinds, coverage gaps) are always JSON arrays: an empty aggregate serializes as `[]`, never `null`. Fragment coverage also carries the retention floor and gaps frozen in the query-context domain snapshot rather than re-deriving them.


The mounted `GET /api/stats/dashboard`, `/dashboard/recent-activity`, `/usage-snapshot`, and endpoint-model drilldown APIs remain available for their own consumers. Their snapshot revision and feed-watermark fields describe those APIs; they do not govern the current Observe fragment lifecycle. Window KPI success is `completed_count / request_count`, with absent denominators shown as unavailable.

### 2.5 Priority Enforcement And Operator-Facing Failure Modes

Prism assigns trusted backend priority metadata before work touches shared resources. Runtime proxy traffic is `proxy`, management routes are `management` with an explicit `M1`, `M2`, or `M3` tier, and scheduler-owned workers are `background` with a declared subclass, budget, coalescing policy, retry policy, and drain policy. Priority-sensitive backend changes should stay covered by the standard priority regression tests, including `go test ./tests/priority/...`.

PostgreSQL capacity is split into finite named lanes: `runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `cache_refresh`, and `background_jobs`. The defaults derive `unit = clamp(GOMAXPROCS(0), 8, 16)`: management is `unit + 1`, runtime execution is `unit`, telemetry is `unit / 2`, and feedback, cache refresh, and background jobs are each `unit / 4`. The resulting six-lane maximum is 27 through 53 connections. Operators should treat lane saturation by owner: proxy execution pressure is separate from management UI pressure, telemetry drain pressure, lossy feedback drain pressure, cache refresh, and generic background jobs. Background or management saturation must not consume protected proxy capacity.

Management overload is reported as typed admission failure with retry metadata; the overload response carries a `Retry-After` header (delay-seconds) that the frontend parses into `ApiError.retryAfterMs` and renders as "服务暂不可用（过载保护），请在约 N 秒后重试" guidance on Observe fragments. Default M2 concurrency is `unit` and is clamped to `management.maxConns - 1`, reserving at least one M1 database slot. Default M3 concurrency is `unit / 2`; an M3 request must acquire both its M3 slot and a shared M2 slot, while the controller reserves one M2 slot from M3. Lower-priority M3 reporting and maintenance routes therefore shed before M2 and M1 management work. HTTP-level proxy admission has no global proxy counter; terminal-target admission remains part of the local runtime state. When overload appears, retry after the advertised delay rather than increasing client concurrency.

Scheduler lag means background workers are queued, coalesced, delayed, retried, or dropped according to their worker policy. Lag can delay dashboard materialization, telemetry materialization, management side-effect dispatch, cache warming, and proxy-key usage flushing, but it must not make request-path handlers borrow direct goroutines, direct DB handles, or unmanaged timers.

Durable outboxes expose failure as queued, retry, sent/succeeded, dead-letter, or permanent-failure state depending on the store. Management mutations place follow-up events in `management_outbox` in the primary transaction and wake the `management_side_effect_outbox` dispatcher after commit; handler failures retry or become visibly permanent failures without rolling back the committed management mutation. Dashboard snapshot invalidation is one such after-commit side effect. Failover incident webhook alerts use `alert_webhook_outbox` and the `alert_webhook_worker`; runtime feedback writes enqueue alert payloads in the same transaction as the loadbalance event, and webhook HTTP POSTs run only in background work.

Runtime telemetry has durable success handoffs, scheduled activity handoffs, and background materialization. Every provider-forwarded successful `2xx` response requires a durable `runtime_telemetry_outbox` row: buffered responses commit a completed envelope before the response is committed, while passthrough SSE and non-SSE responses commit an accepted row before the first flush and finalize that row after response capture completes. Captured non-`2xx` activity, telemetry-eligible planning failures carrying `PlanningFailure`, and terminal execution errors (including all-transport failure and the 64-launch attempt limit) first use a bounded in-memory scheduler side-effect queue, which later attempts durable outbox insertion and can be lost if rejected, terminally failed, or abandoned during shutdown. A worker materializes accepted outbox rows into `request_logs`, `audit_logs`, `usage_request_events`, and proxy-key usage in one transaction before deleting the outbox row. Runtime feedback is separately and intentionally lossy under pressure; queue-full, invalid, closed, or store-failure cases drop feedback with accounting and never block proxy responses.

Audit and statistics reads are bounded. Raw audit lists require backend-enforced time windows and keyset cursors. `GET /api/stats/dashboard` still returns backend-computed `routing_health_map`, but the current dashboard adapter does not render it; the production Models table presents retained success rate, P95 latency, and 24-hour request count as text rather than health badges. The connection-success-rate API also exists without a current production UI consumer. Broad deletes run as durable management jobs.

Runtime cache correctness is generation-based. Management mutations advance durable runtime-cache generations in the same transaction as the primary state change, runtime reads validate generation vectors and refresh or fail closed when stale, and post-response cache warming is non-authoritative. Cache generation lag may delay warm snapshots, but auth-sensitive runtime reads reject stale or unverifiable snapshots instead of accepting old state. The bump is carried by the pgxutil before-commit hook and only runs inside read-write transactions; read-only transactions never advance generations (see `pgxutil/tx.go`).
