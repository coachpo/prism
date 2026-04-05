# FRONTEND LOADBALANCE STRATEGIES DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/loadbalance-strategies/` owns the dedicated strategy-management route behind `../LoadbalanceStrategiesPage.tsx`. It covers the profile-scoped strategy list, create or edit dialog flows, delete confirmation, and the form normalization that mirrors the merged backend strategy contract for native-model routing. The route must treat both `legacy` and `adaptive` strategy families as first-class UI choices.

## STRUCTURE
```
loadbalance-strategies/
├── LoadbalanceStrategiesTable.tsx     # Table rendering and row actions
├── LoadbalanceStrategyDialog.tsx      # Create-edit dialog
├── DeleteLoadbalanceStrategyDialog.tsx # Delete confirmation and dependency handling
├── loadbalanceStrategyFormState.ts    # Form defaults and payload transforms
└── useLoadbalanceStrategiesPageData.ts # Page bootstrap, CRUD orchestration, and local patching
```

## WHERE TO LOOK

- Route shell and page composition: `../LoadbalanceStrategiesPage.tsx`
- Strategy bootstrap, mutation orchestration, and optimistic patching: `useLoadbalanceStrategiesPageData.ts`
- Form defaults, validation, and request payload shaping for the dual-family contract: top-level `strategy_type` is `legacy` or `adaptive`; legacy strategies carry `legacy_strategy_type` plus `auto_recovery`, while adaptive strategies carry the full `routing_policy` document and must preserve untouched adaptive fields through edit/save round-trips: `loadbalanceStrategyFormState.ts`
- Table rendering and destructive flow entrypoints: `LoadbalanceStrategiesTable.tsx`, `DeleteLoadbalanceStrategyDialog.tsx`

## CONVENTIONS

- Keep backend access on the shared `api.*` boundary; this page should not create a parallel fetch layer.
- Keep strategy form normalization and request shaping in `loadbalanceStrategyFormState.ts` rather than scattering the rules across dialogs.
- Match the CRUD/page shell pattern used by other profile-scoped management pages such as pricing templates.
- Keep both strategy families on the existing strategy dialog; users must be able to intentionally choose `legacy` or `adaptive` when creating a strategy, while edits preserve the stored family.
- Keep family-specific fields explicit in the form state: legacy strategies own legacy routing choice, cooldown thresholds, status-code policy, and ban escalation; adaptive strategies own `routing_policy` inputs. Do not hide the family behind inferred defaults.
- Keep failure-status editing, adaptive-routing defaults, full adaptive policy preservation, and payload normalization inside `loadbalanceStrategyFormState.ts`; do not scatter contract shaping across dialog components.
- Keep kind-aware summary wording in `LoadbalanceStrategiesTable.tsx` and shared page data helpers; do not duplicate family label, objective label, or summary formatting elsewhere.
- Keep the merged contract forward-only. Do not add compatibility shims, silent coercion, or a fallback path that collapses both families back into one generic strategy type.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not let table components own API calls directly when `useLoadbalanceStrategiesPageData.ts` already centralizes CRUD orchestration.
- Do not reintroduce model-level cooldown, legacy failover-policy, or adaptive routing-policy fields outside this strategy UI.
- Do not split ban controls into a second dialog or page; they belong to the existing strategy dialog.
- Do not create a second policy-management page or move strategy assignment out of the existing model dialogs.
- Do not collapse `legacy` and `adaptive` into the same label, selector option, or summary copy.
