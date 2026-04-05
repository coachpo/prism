# FRONTEND MODEL DETAIL DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/model-detail/` owns the heavy route logic behind `../ModelDetailPage.tsx` and `../ProxyModelDetailPage.tsx`: bootstrap and redirect handling, family-aware strategy summary display, ordered proxy-target summary/editing for proxy models, connection mutation flows, manual health checks, 24-hour KPIs, model-scoped loadbalance events, current recovery state (cooldown plus ban state), and the parent-covered `connections-list/` UI cluster.

## STRUCTURE
```
model-detail/
├── useModelDetailData.ts             # High-level page composition
├── useModelDetailPageShell.ts        # Route-shell tabs, header state, and page composition handoff
├── useModelDetailBootstrap.ts        # Parallel bootstrap fetches and redirects
├── useModelDetailConnectionFlows.ts  # Create, edit, delete, and reorder orchestration
├── useModelDetailConnectionMutations.ts
├── useModelDetailDialogState.ts
├── useModelDetailDataSupport.ts      # Default form factories, redirect targets, optimistic helpers
├── useModelDetailModelForm.ts
├── useConnectionHealthChecks.ts
├── useConnectionFocus.ts
├── useModelDetailMetrics24h.ts
├── useModelLoadbalanceCurrentState.ts
├── OverviewCards.tsx
├── ModelDetailHeader.tsx
├── ModelDetailTabs.tsx
├── ProxyTargetsCard.tsx
├── ConnectionsList.tsx
├── LoadbalanceEventsTab.tsx
├── ConnectionDialog.tsx
├── ModelSettingsDialog.tsx
├── useModelLoadbalanceEvents.ts
├── modelDetailMetricsAndPaths.ts     # Shared latency and connection-label helpers
└── connections-list/                 # Connection card, sortable shell, and list helpers
```

## WHERE TO LOOK

- Thin route shells and page-shell composition: `../ModelDetailPage.tsx`, `../ProxyModelDetailPage.tsx`, `useModelDetailPageShell.ts`, `ModelDetailHeader.tsx`, `ModelDetailTabs.tsx`
- High-level composition and page-owned side effects: `useModelDetailData.ts`
- Bootstrap fetches, focus handoff, and redirect handling: `useModelDetailBootstrap.ts`, `useConnectionFocus.ts`
- Connection create, edit, delete, and reorder flows: `useModelDetailConnectionFlows.ts`, `useModelDetailConnectionMutations.ts`, `useModelDetailDialogState.ts`
- Health checks and 24-hour KPI loading: `useConnectionHealthChecks.ts`, `useModelDetailMetrics24h.ts`, `OverviewCards.tsx`
- Default forms, ordered proxy-target options, strategy summary helpers, and optimistic helpers: `useModelDetailDataSupport.ts`, `useModelDetailModelForm.ts`, `ModelSettingsDialog.tsx`, `ProxyTargetsCard.tsx`
- Connection list shell plus local cluster: `ConnectionsList.tsx`, `connections-list/`
- Model-scoped loadbalance event refresh, paging, and detail wiring: `LoadbalanceEventsTab.tsx`, `useModelLoadbalanceEvents.ts`, `../../components/AGENTS.md`
- Current recovery-state fetch and reset actions: `useModelLoadbalanceCurrentState.ts`
- Shared latency and connection-label formatting: `modelDetailMetricsAndPaths.ts`

## CONVENTIONS

- Keep `ModelDetailPage.tsx` and `ProxyModelDetailPage.tsx` thin. `useModelDetailData.ts` owns bootstrap, dialog state, and the cross-hook composition layer.
- Fetch model, endpoints, model list, and pricing templates in parallel during bootstrap.
- Use `Promise.allSettled` for health-check batches so one failing connection does not collapse the page.
- Keep model loadbalance current state in `useModelLoadbalanceCurrentState.ts`, including refresh and reset actions, instead of scattering cooldown/ban state inside cards or tabs.
- Keep optimistic priority reordering in the hook layer plus the connection-list helpers, and revert UI order if the backend PATCH fails.
- Keep connection-card recovery copy in `connections-list/ConnectionCardSectionsShared.ts`; tone labels and reset wording in `ConnectionCardCooldownState.tsx` should stay presentation-only.
- Keep loadbalance event badge/detail rendering in the shared `src/components/loadbalance/` components; `LoadbalanceEventsTab.tsx` should remain a thin page shell.
- Keep proxy-target option building and update payload shaping in `useModelDetailDataSupport.ts` / `useModelDetailModelForm.ts`; proxy-target card/dialog rendering should stay presentation-focused.
- Treat `connections-list/` as a local cluster that stays documented here. It supports the parent route and should not get its own AGENTS file.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not move orchestration state back into `ModelDetailPage.tsx`.
- Do not duplicate default form factories or redirect-target logic outside `useModelDetailDataSupport.ts`.
- Do not manage routing priority from `ConnectionDialog.tsx`. Ordering belongs to the connection-list flow.
- Do not leave ban-aware wording trapped only in one connection-card component; shared current-state copy must stay centralized.
- Do not split `connections-list/` into a separate AGENTS file. This parent doc owns that cluster.
