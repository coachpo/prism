# BACKEND MANAGEMENT LOADBALANCE KNOWLEDGE BASE

## OVERVIEW
`management/loadbalance/` owns selected-profile load-balance management under `/api/loadbalance/*`. It manages strategy CRUD, canonical default-strategy creation, current runtime state inspection/reset, and load-balance event reads.

## STRUCTURE
```text
loadbalance/
├── service.go          # Service construction and `/loadbalance` route mounting
├── routes.go           # Strategy CRUD and canonical defaults
├── observability.go    # Current-state and event routes
├── policy.go           # Strategy policy normalization
├── store.go            # Strategy persistence
├── import_contract.go  # Import-facing strategy contract helpers
└── types.go            # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Strategy list/get/create/update/delete and defaults: `routes.go`.
- Current-state list/reset and loadbalance events: `observability.go`.
- Shared domain operations: `../../../domain/loadbalance/`.

## CONVENTIONS
- Keep strategy CRUD selected-profile scoped.
- Don't delete strategies attached to models.
- Keep current-state reset/list wired through `LocalRuntimeStateStore` and the loadbalance domain.
- Keep event reads here; request logs and statistics stay in `stats/`.

## LLM UPSTREAM MATRIX
- When routing-family or Ban Policy retry-window behavior changes, evaluate model access across supported operation shapes, not just one provider family.

## ANTI-PATTERNS
- Do not delete load-balance strategies while attached models exist.
- Do not treat current-state reset as strategy CRUD.
- Do not move request-log or statistics reads into this package.
- Do not bypass canonical default-strategy conflict checks.
