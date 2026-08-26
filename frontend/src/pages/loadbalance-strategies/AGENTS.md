# FRONTEND LOADBALANCE STRATEGIES COMPATIBILITY CLUSTER

## OVERVIEW
`pages/loadbalance-strategies/` keeps strategy widgets imported by the feature-owned `/route/ban-policies` route under `src/features/loadbalance/`: the strategies table (explicit default badge, bound-model impact list, built-in completion, set-default) and the delete dialog (attachment and default-replacement guards). The feature route owns page composition while `useBanPolicyStrategyCollection.ts`, `useBanPolicyMutations.ts`, and `useStrategyImpactPager.ts` own the independent collection/read, CRUD/default, and impact lifecycles.

## STRUCTURE
```text
loadbalance-strategies/
├── DeleteLoadbalanceStrategyDialog.tsx # Delete confirmation and dependency handling
├── LoadbalanceStrategiesTable.tsx      # Table rendering and row actions
├── StrategyPreviewTimeline.tsx         # Retry/backoff preview rendered as a timeline
└── strategyValueBadges.tsx             # Duration and retry badges in reading units, not raw milliseconds
```

## WHERE TO LOOK
- Feature route and Ban Policy form schema: `../../features/loadbalance/`
- Strategy collection/read, CRUD/default mutations, and impact cursor pager: `../../features/loadbalance/useBanPolicyStrategyCollection.ts`, `../../features/loadbalance/useBanPolicyMutations.ts`, `../../features/loadbalance/useStrategyImpactPager.ts`
- Table rendering and destructive flow entrypoints: `LoadbalanceStrategiesTable.tsx`, `DeleteLoadbalanceStrategyDialog.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep backend access on the shared `api.*` boundary; this cluster should not create a parallel fetch layer.
- Keep retry-window fields explicit in feature form state: failure status codes, base retry delay, backoff, jitter, maximum retry delay, cycle retry attempts, cumulative ban threshold, ban mode, and ban duration.
- Do not let table components own API calls directly when the named feature lifecycle owners centralize reads and CRUD orchestration.
- Keep strategy collection/read state, CRUD/default mutation state, and impact cursor state in their named feature owners; `useBanPoliciesFeatureData.ts` only composes their page-facing values.
- Do not reintroduce model-level cooldown, removed failover-policy, or removed routing-policy fields outside this strategy UI.
