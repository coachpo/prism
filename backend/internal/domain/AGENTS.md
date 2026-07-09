# BACKEND DOMAIN KNOWLEDGE BASE

## OVERVIEW
`backend/internal/domain/` owns backend domain logic shared by management, runtime, and stats handlers without owning HTTP routing, platform lifecycle, or provider transport.

## STRUCTURE
```text
domain/
├── audit/          # Audit service helpers and weak request-log references
├── loadbalance/    # Runtime connection state, strategy math, bans, and events
│   └── AGENTS.md   # Ban Policy runtime-state and event rules
├── modelrouting/   # Ordered access-target and terminal-target routing helpers
├── stats/          # Dashboard, usage, spending, topology, and request-log projections
│   └── AGENTS.md   # Stats read-model and retained-history rules
└── terminaltarget/ # Terminal Target helper types
```

## WHERE TO LOOK
- Audit domain helpers and request-log weak-reference semantics: `audit/`
- Runtime connection state, Ban Policy transitions, round-robin cursors, and load-balance event payloads: `loadbalance/AGENTS.md`, `loadbalance/`
- Model-routing helper contracts shared with runtime planning and management authoring: `modelrouting/`, `terminaltarget/`
- Dashboard aggregate snapshots, topology graphs, request-log read models, spending, throughput, usage snapshots, and rollups: `stats/AGENTS.md`, `stats/`
- HTTP ownership that consumes these domains: `../httpapi/management/stats/AGENTS.md`, `../httpapi/runtime/AGENTS.md`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep this tree HTTP-neutral. Handlers, route parsing, auth, Default-profile management resolution, and response shaping stay under `../httpapi/`.
- Keep platform concerns out of domain packages. DB lane selection, transactions, scheduler work, migrations, retention workers, and side-effect dispatch stay under `../platform/` or `../pgxutil/`.
- Keep runtime load-balance state deterministic and profile/model scoped; policy thresholds and ban modes must match the management loadbalance contract.
- Keep stats/read models derived from retained PostgreSQL history and endpoint label snapshots; do not duplicate frontend aggregation or pricing math.
- Keep domain helpers provider-agnostic. Provider-native request/response behavior belongs under `../gateway/provider/` or runtime operation hooks.

## ANTI-PATTERNS
- Do not add HTTP handlers, middleware, cookies, or proxy-key checks here.
- Do not borrow protected runtime, telemetry, feedback, management, cache-refresh, or background-job DB lanes from domain helpers.
- Do not inline retention cleanup, partition creation, dashboard publishing, or telemetry/feedback side effects in domain calculations.
- Do not let frontend display requirements redefine stats, load-balance, or request-log domain semantics.
