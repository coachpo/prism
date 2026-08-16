# BACKEND DOMAIN STATS KNOWLEDGE BASE

## OVERVIEW
`backend/internal/domain/stats/` owns PostgreSQL-backed read models for dashboards, usage analytics, spending, request logs, recent activity, routing health, and retained filter options.

## STRUCTURE
```text
stats/
├── aggregates.go                 # Shared aggregate buckets and cost/usage helpers
├── dashboard_health.go            # Dashboard freshness/coverage helper types
├── dashboard_snapshot_builder.go # Overview dashboard aggregate snapshot
├── dashboard_recent_activity.go  # Bounded request-history activity feed
├── request_logs.go               # Attempt-view list projections, scoped filters, and v2 slim rows
├── request_logs_chain.go         # Retained ingress-chain view: finalized summaries, cohort filters, signed cursors
├── request_logs_export.go        # Server-side full filtered CSV export (RR snapshot, bounds, digest)
├── request_logs_detail_v2.go     # Exact v2 detail: scoped statuses, failure projection, pricing layers
├── cost_segments.go              # Canonical cost-segment catalogue (e.N / l.AAA / l.__unknown__)
├── snapshot.go                   # Usage snapshots
├── retention_source.go           # Retention source and actual-coverage owner projection
├── observe_query.go              # Query-context signing/verification and coverage resolution
├── query_coverage.go             # Non-null Requests/Audit coverage union
└── types.go                      # JSON-facing read-model types
```

## WHERE TO LOOK
- Dashboard aggregate snapshot and routing health map: `dashboard_snapshot_builder.go`
- Recent activity feed and watermarks: `dashboard_recent_activity.go`
- Request-log attempt list/detail, chain view, CSV export, and cost segments: `request_logs.go`, `request_logs_chain.go`, `request_logs_export.go`, `request_logs_detail_v2.go`, `cost_segments.go`, `types.go`
- Usage snapshot, spending, endpoint/model/proxy-key aggregates: `snapshot.go`, `aggregates.go`
- Usage snapshot latency trends expose hourly/daily p50 and p95 `response_time_ms` buckets through `latency_trends` beside request and token trends.
- HTTP management consumers: `../../httpapi/management/stats/`

## CONVENTIONS
- Keep this package HTTP-neutral. Selected-profile parsing, query params, and response writing stay in `httpapi/management/stats`.
- Use retained history as the source of truth: `request_logs`, `usage_request_events`, and endpoint label snapshots.
- Preserve server field names in JSON types; frontend contracts mirror these names.
- Keep request-log filtering aligned with current product semantics: `client_rule_id` matches caller User-Agent Client Rules, `resolved_target_model_id` means final target, and `pricing_status` is the four-state pricing filter (the retired `priced` boolean alias is rejected as an unknown query key).
- Keep row scoping strict in every read model: `upstream_status_code`/`gateway_status_code`/`legacy_status_code` and `attempt_duration_ms`/`legacy_duration_ms` are selected by `row_kind`; never COALESCE across scopes in public DTOs.
- Keep the Observe retention source authoritative: `retention_source.go` projects `log_retention_policy_resources` per dataset (configured UTC-day cutoff, published floor, revocation epoch, purge state). Observe query contexts, ordinary Requests reads, Audit coverage, Events, manual purge final publish, and the Settings projection all consume this source; never re-derive a floor from policy days or `MIN(created_at)`, and fail closed with the owning 503 while `running|recovery_required`.
- Keep actual coverage owner-authored: the owner materialization cut is `{kind, committed/raw cut, optional manifest/build identity}`; `RefreshActualCoverageProjection` is the only bounded aggregate refresh, and `RecordActualCoverageAppend` is the same-transaction append handoff. Consumers must preserve `coverage_revision`, `coverage_hash`, `source_revision`, generation/fence, freshness, and explicit gaps instead of synthesizing intervals or zero counts.
- Keep query contexts opaque and server-validated: fragments never re-parse presets; a manual-purge final publish revokes older tokens via the retention epoch (`410 dataset_snapshot_revoked`).
- Keep the chain view server-owned: whole-ingress outer pages with signed chain cursors, bounded retained-row inner pages, and finalized-summary facts from `usage_request_events` only.
- Keep CSV export server-side from a single `READ ONLY REPEATABLE READ` snapshot with typed rejection before any file bytes.
- Keep pricing and usage-source math in backend read models. Frontend tables render supplied values; they do not recalculate totals.
- Dashboard overview and recent activity are separate read models; do not fold recent activity into aggregate snapshots.

## ANTI-PATTERNS
- Do not add HTTP handlers or route parsing here.
- Do not duplicate stats aggregation in frontend code.
- Do not use mutable endpoint labels when retained `endpoint_label_snapshot` is required for historical reporting.
