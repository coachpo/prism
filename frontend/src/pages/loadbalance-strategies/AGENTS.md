# FRONTEND LOADBALANCE STRATEGIES COMPATIBILITY CLUSTER

## OVERVIEW
`pages/loadbalance-strategies/` keeps strategy widgets still imported by the feature-owned `/route/ban-policies` route under `src/features/loadbalance/`. The feature route owns Ban Policy form state, CRUD orchestration, and page composition.

## STRUCTURE
```text
loadbalance-strategies/
├── DeleteLoadbalanceStrategyDialog.tsx # Delete confirmation and dependency handling
└── LoadbalanceStrategiesTable.tsx      # Table rendering and row actions
```

## WHERE TO LOOK
- Feature route, Ban Policy form schema, and mutation orchestration: `../../features/loadbalance/`
- Table rendering and destructive flow entrypoints: `LoadbalanceStrategiesTable.tsx`, `DeleteLoadbalanceStrategyDialog.tsx`

## CONVENTIONS
- Keep backend access on the shared `api.*` boundary; this cluster should not create a parallel fetch layer.
- Keep retry-window fields explicit in feature form state: failure status codes, base retry delay, backoff, jitter, maximum retry delay, cycle retry attempts, cumulative ban threshold, ban mode, and ban duration.
- Do not let table components own API calls directly when the feature data hook centralizes CRUD orchestration.
- Do not reintroduce model-level cooldown, removed failover-policy, or removed routing-policy fields outside this strategy UI.
