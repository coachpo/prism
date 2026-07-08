# BACKEND DOMAIN STATS KNOWLEDGE BASE

## OVERVIEW
`backend/internal/domain/stats/` owns PostgreSQL-backed read models for dashboards, usage analytics, spending, request logs, recent activity, topology graphs, and retained filter options.

## STRUCTURE
```text
stats/
├── aggregates.go                 # Shared aggregate buckets and cost/usage helpers
├── dashboard_snapshot_builder.go # Overview dashboard aggregate snapshot
├── dashboard_recent_activity.go  # Bounded request-history activity feed
├── dashboard_topology_graph.go   # Routing/topology graph projection
├── request_logs.go               # Request list/detail projections and filters
├── rollups.go, snapshot.go       # Usage/spending snapshots and rollups
└── types.go                      # JSON-facing read-model types
```

## WHERE TO LOOK
- Dashboard aggregate snapshot and routing map: `dashboard_snapshot_builder.go`, `dashboard_topology_graph.go`
- Recent activity feed and watermarks: `dashboard_recent_activity.go`
- Request-log list/detail filters and final-target fields: `request_logs.go`, `types.go`
- Usage snapshot, spending, endpoint/model/proxy-key rollups: `snapshot.go`, `rollups.go`, `aggregates.go`
- Usage snapshot latency trends expose hourly/daily p50 and p95 `response_time_ms` buckets through `latency_trends` beside request and token trends.
- HTTP management consumers: `../../httpapi/management/stats/`

## CONVENTIONS
- Keep this package HTTP-neutral. Selected-profile parsing, query params, and response writing stay in `httpapi/management/stats`.
- Use retained history as the source of truth: `request_logs`, `usage_request_events`, and endpoint label snapshots.
- Preserve server field names in JSON types; frontend contracts mirror these names.
- Keep request-log filtering aligned with current product semantics: `client_rule_id` matches caller User-Agent Client Rules, and `resolved_target_model_id` means final target.
- Keep pricing and usage-source math in backend read models. Frontend tables render supplied values; they do not recalculate totals.
- Dashboard overview and recent activity are separate read models; do not fold recent activity into aggregate snapshots.

## ANTI-PATTERNS
- Do not add HTTP handlers or route parsing here.
- Do not duplicate stats aggregation in frontend code.
- Do not use mutable endpoint labels when retained `endpoint_label_snapshot` is required for historical reporting.
