# BACKEND MANAGEMENT STATS KNOWLEDGE BASE

## OVERVIEW
`management/stats/` owns selected-profile observability reads under `/api/stats/*`. It serves stats-only dashboard aggregate snapshots, separate dashboard recent activity, request-log list or detail, summary, spending, throughput, model metrics, connection success rates, usage snapshots, and endpoint model statistics. Dashboard snapshot invalidation is an internal side-effect seam in this package, not a public stats route.

## STRUCTURE
```text
stats/
└── service.go    # Service construction, route mounting, snapshots, handlers, parsers, invalidation handler
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`
- Dashboard aggregate snapshot reads plus side-effect invalidation handler: `service.go`, `../../../domain/stats/`
- Dashboard recent activity and request-log list/detail routes: `service.go`, `../../../domain/stats/`
- Summary, spending, throughput, model metrics, usage snapshot, and endpoint model statistics: `service.go`
- Invalidation event source outside the public stats routes: `../../../platform/managementsideeffects/`, `../../../platform/http/runtime_cache_invalidation.go`

## CONVENTIONS
- Keep these as read-oriented management observability routes; runtime request execution stays in `runtime/`.
- Keep request logs and statistics in this package, not in runtime handlers.
- Use dashboard aggregate snapshots only for matching default dashboard windows.
- Keep dashboard snapshots free of recent activity rows and request-log freshness keys; `/api/stats/dashboard/recent-activity` owns the bounded activity feed.
- Keep snapshot invalidation behind management side-effect events, not inline request-path rebuilds or public `/api/stats/*` mutation routes.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When observability shaping changes, preserve operation-name and token/cost visibility across OpenAI, Anthropic, and Gemini operation shapes.

## ANTI-PATTERNS
- Do not move request-log list/detail or statistics reads into runtime handlers.
- Do not rebuild dashboard snapshots inline for non-dashboard query windows.
- Do not create or drop log partitions from stats handlers.
- Do not treat stats retention settings as owned here; they belong to `settings/`.
