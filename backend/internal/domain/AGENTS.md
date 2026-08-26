# BACKEND DOMAIN KNOWLEDGE BASE

## OVERVIEW

`backend/internal/domain/` owns backend domain logic shared by management, runtime, and stats handlers without owning HTTP routing, platform lifecycle, or provider transport.

## STRUCTURE

```text
domain/
├── audit/          # Audit service helpers and weak request-log references
├── loadbalance/    # Runtime connection state, strategy math, bans, and events
│   └── AGENTS.md   # Ban Policy runtime-state and event rules
├── modelexport/    # Pi/OpenCode client-config export: presence-aware merge, price gates, digest, origin-based renderers
├── modelrouting/   # Ordered access-target and terminal-target routing helpers
├── safediag/       # Fixed safe-diagnostic scrub bottom line shared by audit and request logs
│   └── AGENTS.md   # Scrub, extract, code, metadata, and limit rules
├── stats/          # Dashboard, usage, spending, and request-log projections
│   └── AGENTS.md   # Stats read-model and retained-history rules
├── pricingkind/    # Typed standard/tiered/peak_valley kind and card-role constants
└── terminaltarget/ # Terminal Target helper types, pricing-window values, the shared custom request parameters validator/overlay, and the routing-schedule compiler
```

## WHERE TO LOOK

- Audit domain helpers and request-log weak-reference semantics: `audit/`
- Runtime connection state, Ban Policy transitions, round-robin cursors, and load-balance event payloads: `loadbalance/AGENTS.md`, `loadbalance/`
- Model-routing helper contracts shared with runtime planning and management authoring: `modelrouting/`, `terminaltarget/`
- Client model-config export domain contracts (merge presence rules, price gates, clock-free digest, deterministic Pi 0.84.3/OpenCode 1.18.23 renderers): `modelexport/`
- Terminal Target records and the single authoritative `custom_request_parameters` validator/overlay (limits, protected keys, canonicalization, deterministic shallow overlay): `terminaltarget/custom_request_parameters.go`
- Routing-window compilation and eligibility (`CompileRoutingSchedule`, `DecideAt`, `NextOpenAt`, `NextCloseAt`, `WindowsCoverFullWeek`, `ValidateRoutingSchedule`): `terminaltarget/routing_schedule.go`; pricing-window digest and peak/valley decision values: `terminaltarget/pricing_schedule.go`
- Typed pricing kind/card roles and independent selector evidence: `pricingkind/pricingkind.go`; pricing windows reuse the terminal-target half-open geometry but belong to a pricing revision and use the frozen ingress planning clock.
- Dashboard aggregate snapshots, request-log read models, spending, throughput, usage snapshots, and rollups: `stats/AGENTS.md`, `stats/`
- HTTP ownership that consumes these domains: `../httpapi/management/stats/AGENTS.md`, `../httpapi/runtime/AGENTS.md`

## CONVENTIONS

- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep this tree HTTP-neutral. Handlers, route parsing, auth, Default-profile management resolution, and response shaping stay under `../httpapi/`.
- Keep platform concerns out of domain packages. DB lane selection, transactions, scheduler work, migrations, retention workers, and side-effect dispatch stay under `../platform/` or `../pgxutil/`.
- Keep runtime load-balance state deterministic and profile/model scoped; policy thresholds and ban modes must match the management loadbalance contract.
- Keep stats/read models derived from retained PostgreSQL history and endpoint label snapshots; do not duplicate frontend aggregation or pricing math.
- Keep domain helpers provider-agnostic. Provider-native request/response behavior belongs under `../gateway/provider/` or runtime operation hooks.
- Keep client-config renderers pure: callers provide a validated Prism gateway origin, provider id, an explicit omitted-versus-included client-key value, server-owned enrichment, and current pricing facts. Renderers must never derive client URLs or credentials from upstream endpoints, read stored endpoint keys, trust a request-carried models.dev candidate, perform network I/O, or persist export state.
- Validate manual enhancements against the pinned platform schema before applying them and validate the complete generated document before serialization. Unknown or wrong-typed target fields fail closed as typed schema errors; OpenCode status/experimental fields and synthesized variants remain excluded, while Pi effort mappings are total across all seven levels with explicit nulls for unsupported levels.
- Keep export pricing fail closed across all actually reachable targets: only a consistent USD/PER_1M five-component shape with `reasoning_price == output_price` may produce `cost`. A null component emits `pricing_component_missing` and omits the whole group; explicit `"0"` alone means free. Pi may represent one strict positive-threshold tier, while OpenCode may represent only flat pricing or the exact 200,000-token `context_over_200k` tier.
- Keep `CompiledRoutingSchedule` methods on value receivers and never mutate `Windows` in place: planning snapshots are shared across requests through an atomic pointer with only shallow map copies, so an in-place write is a cross-request data race that no test detects.
- Keep the `custom_request_parameters` parse/validate/overlay semantics in exactly one place under `terminaltarget/`; management routes, runtime planning snapshots, and the frontend validator must not maintain independent copies of protected keys or limits.

## ANTI-PATTERNS

- Do not add HTTP handlers, middleware, cookies, or proxy-key checks here.
- Do not borrow protected runtime, telemetry, feedback, management, cache-refresh, or background-job DB lanes from domain helpers.
- Do not inline retention cleanup, partition creation, dashboard publishing, or telemetry/feedback side effects in domain calculations.
- Do not let frontend display requirements redefine stats, load-balance, or request-log domain semantics.
