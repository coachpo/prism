# BACKEND MANAGEMENT LOADBALANCE KNOWLEDGE BASE

## OVERVIEW
`management/loadbalance/` owns Default-profile load-balance management under `/api/loadbalance/*`. It manages strategy CRUD with the explicit `is_default` contract (CAS set-default, delete guards), canonical defaults completion, the bounded strategy impact read model, the side-effect-free effect preview, the global current-state read (configured-target union, process-local completeness, stable identity cursor, no-store), the signed events query-context, and the profile-scoped events timeline/detail with typed V1 summaries and the ±15-minute Requests handoff.

## STRUCTURE
```text
loadbalance/
├── service.go          # Service construction and `/loadbalance` route mounting
├── routes.go           # Strategy CRUD and canonical defaults
├── current_state_observability.go # Process-local current-state read/reset
├── event_query_context_routes.go # Signed event query-context issue/retention validation
├── event_routes.go     # Event list/detail query routes
├── incident_routes.go  # Incident projection route
├── observability_query.go # Observability query parsing
├── instance_identity.go # Process instance identity
├── policy.go           # Strategy policy normalization
├── store.go            # Strategy persistence
├── import_contract.go  # Import-facing strategy contract helpers
├── cursor.go           # Opaque current-state cursor encoding and scope validation
├── generation.go       # Configuration-revision stamping for observability reads
├── events_query_context.go # Event filter resolution shared by the event routes
└── types.go            # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Strategy list/get/create/update/delete and defaults: `routes.go`.
- Process-local current-state list/reset: `current_state_observability.go`.
- Signed event query-context issue and retention checks: `event_query_context_routes.go`, `events_query_context.go`.
- Event list/detail and incident projections: `event_routes.go`, `incident_routes.go`.
- Query parsing and process identity: `observability_query.go`, `instance_identity.go`.
- Shared domain operations: `../../../domain/loadbalance/`.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep strategy CRUD pinned to Default profile id `1`. `X-Profile-Id` compatibility headers are ignored here.
- Don't delete strategies attached to models.
- Keep current-state reset/list wired through `LocalRuntimeStateStore` and the loadbalance domain.
- Keep event reads here; request logs and statistics stay in `stats/`.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When routing-family or Ban Policy retry-window behavior changes, evaluate model access across supported operation shapes, not just one provider family.

## ANTI-PATTERNS
- Do not delete load-balance strategies while attached models exist.
- Do not treat current-state reset as strategy CRUD.
- Do not move request-log or statistics reads into this package.
- Do not bypass canonical default-strategy conflict checks.
