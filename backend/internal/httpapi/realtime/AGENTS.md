# BACKEND REALTIME KNOWLEDGE BASE

## OVERVIEW
`realtime/` owns Prism's mounted `/api/realtime/ws` websocket surface, channel subscription contract, connection manager, auth-gated session bootstrap, and async dashboard plus analytics publishers.

## STRUCTURE
```text
realtime/
├── service.go                  # Websocket mount, auth gate, channel contract, publish entrypoints
├── manager.go                  # Connection registry, subscribe or unsubscribe bookkeeping, broadcast fanout
├── publisher.go                # Dashboard snapshot and activity publication helpers
├── async_publisher.go          # Pending dashboard snapshot worker
├── async_analytics_publisher.go # Pending analytics worker
├── analytics_snapshot.go       # Analytics snapshot shaping and preset contract
└── types.go                    # Realtime payload shapes
```

## WHERE TO LOOK
- Mounted websocket route and auth gate: `service.go`
- Connection lifecycle, channel subscriptions, and broadcast fanout: `manager.go`, `types.go`
- Dashboard snapshot and activity publication paths: `publisher.go`, `async_publisher.go`
- Analytics publication path and preset handling: `async_analytics_publisher.go`, `analytics_snapshot.go`
- Auth-state resolution for websocket sessions: `../management/auth/realtime.go`, `../management/auth/AGENTS.md`
- Frontend consumers: `../../../../frontend/src/hooks/useRealtimeData.ts`, `../../../../frontend/src/lib/websocket.ts`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep websocket mounting under `/api/realtime/ws`; runtime `/v1` and `/v1beta` routes stay separate.
- Keep auth gating on the management-auth seam instead of duplicating cookie or token parsing here.
- Keep dashboard and analytics as the supported channel families.
- Keep dashboard messages split between `dashboard.snapshot` for stats-only aggregate state and `dashboard.activity` for one request-history activity item.
- Keep async publication on the dedicated publishers instead of inline fanout from unrelated request handlers.
- Keep analytics preset validation centralized in this package.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## ANTI-PATTERNS
- Do not add ad hoc websocket routes outside this package.
- Do not bypass the connection manager for subscription or broadcast bookkeeping.
- Do not publish dashboard or analytics updates inline from management handlers when the async publishers already own that flow.
- Do not drift frontend realtime clients away from the supported channel or preset contract here.
