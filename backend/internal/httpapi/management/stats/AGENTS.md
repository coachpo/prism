# BACKEND MANAGEMENT STATS KNOWLEDGE BASE

## OVERVIEW
`management/stats/` owns observability reads under `/api/stats/*` pinned to Default profile id `1`. It serves stats-only dashboard aggregate snapshots, separate dashboard recent activity, request-log list or detail, summary, spending, throughput, model metrics, connection success rates, usage snapshots, and endpoint model statistics. Dashboard snapshot invalidation is an internal side-effect seam in this package, not a public stats route. `X-Profile-Id` may be accepted but is ignored; storage `profile_id` columns remain.

## STRUCTURE
```text
stats/
├── service.go                    # Stats service lifecycle
├── routes.go                     # Stats management route dispatch
├── dashboard_snapshot_cache.go  # Dashboard snapshot cache
├── dashboard_handlers.go        # Dashboard HTTP handlers
├── request_log_handlers.go      # Request-log HTTP handlers
├── request_log_export.go        # Request-log CSV export
├── request_log_query.go         # Request-log query parsing
├── ingress_chain_query.go       # Ingress-chain query parsing
├── usage_aggregate_handlers.go  # Usage aggregate HTTP handlers
├── cost_segments_handlers.go    # Cost-segment HTTP handlers
├── query_parameters.go          # Stats query parameter parsing
├── errors.go                    # Stats error translation
├── observe_handlers.go          # Query-context issuing/resolution, usage summary/series, dashboard-now, retention floor
└── observe_endpoint_handlers.go # Endpoint Terminal Target statistics and the Observe activity feed
```

## WHERE TO LOOK
- Route list and mount contract: `routes.go`
- Service construction and shared seams: `service.go`
- Dashboard snapshot cache plus side-effect invalidation: `dashboard_snapshot_cache.go`, `../../../domain/stats/`
- Dashboard reads: `dashboard_handlers.go`, `../../../domain/stats/`
- Request-log attempts, details, and filter options: `request_log_handlers.go`, `request_log_query.go`, `../../../domain/stats/`
- Server-side request-log CSV: `request_log_export.go`, `../../../domain/stats/`
- Ingress-chain request parameters: `ingress_chain_query.go`
- Spending, throughput, model metrics, and usage snapshot: `usage_aggregate_handlers.go`
- Cost segments and endpoint model statistics: `cost_segments_handlers.go`
- Shared query values and typed errors: `query_parameters.go`, `errors.go`
- Observe query contexts, usage summary/series, and dashboard-now: `observe_handlers.go`
- Endpoint Terminal Target statistics and the Observe activity feed: `observe_endpoint_handlers.go`
- Invalidation event source outside the public stats routes: `../../../platform/managementsideeffects/`, `../../../platform/http/runtime_cache_invalidation.go`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep these as read-oriented management observability routes; runtime request execution stays in `runtime/`.
- Keep request logs and statistics in this package, not in runtime handlers.
- Use dashboard aggregate snapshots only for matching default dashboard windows.
- Keep dashboard snapshots free of recent activity rows and request-log freshness keys; `/api/stats/dashboard/recent-activity` owns the bounded activity feed.
- Keep snapshot invalidation behind management side-effect events, not inline request-path rebuilds or public `/api/stats/*` mutation routes.
- Request-log attempts, ingress chains, and server-side CSV export must resolve `time_range` (`1h|6h|24h|7d|30d|all|custom`) through the shared `Requests.actual_coverage` owner projection. Policy days, `MIN/MAX(created_at)`, a browser clock, and a second coverage route are not valid substitutes; `all` uses the owner earliest bound and an owner-complete empty domain remains an explicit empty interval.
- The request `coverage` union is emitted from the same owner bounds used by SQL and never carries page-size row counts as completeness evidence. Dirty/stale/gapped owner materialization is `legacy_unknown` with explicit gaps, while append-only coverage changes do not revoke an already sealed Observe predicate.
- Public aggregate routes use `scope=ingress|final_execution|route_attempt`, the scope-specific group/filter grammar from `domain/stats/scope.go`, and typed 422 rejection for unknown or incompatible keys. Query contexts bind the scope and freeze the existing usage/request/event coverage owners; fragments never accept a second scope that can disagree with the token.
- Treat a present malformed timestamp, ID, limit, cursor, metric, group, or filter as a typed error; defaults apply only when the field is absent. Ordinary Requests triage is unsigned, while true Observe final/attempt selectors require and consume `query_context` in list and export paths.
- Every metric envelope carries its caliber, dataset coverage, and observation/latency/cost sample and missing counts. Catalog routes carry a fixed catalog caliber. Route-attempt responses never expose served-final cost.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When observability shaping changes, preserve operation-name and token/cost visibility across OpenAI, Anthropic, and Gemini operation shapes.

## ANTI-PATTERNS
- Do not move request-log list/detail or statistics reads into runtime handlers.
- Do not rebuild dashboard snapshots inline for non-dashboard query windows.
- Do not create or drop log partitions from stats handlers.
- Do not treat stats retention settings as owned here; they belong to `settings/`.
