# FRONTEND MODEL DETAIL DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/model-detail/` owns the heavy route logic behind `../ModelDetailPage.tsx` and `../ProxyModelDetailPage.tsx`: bootstrap and redirect handling, family-aware strategy summary display, ordered proxy-target summary/editing for proxy models, connection mutation flows, manual health checks, model spending summaries, model-scoped loadbalance events, current recovery state (cooldown plus ban state), the OpenAI probe helper split in `connectionProbeBehavior.ts`, and the parent-covered `connections-list/` UI cluster.

## STRUCTURE
```
model-detail/
├── useModelDetailData.ts             # High-level page composition
├── useModelDetailPageShell.ts        # Route-shell tabs, header state, and page composition handoff
├── useModelDetailBootstrap.ts        # Parallel bootstrap fetches, redirects, and spending summary load
├── useModelDetailConnectionFlows.ts  # Create, edit, delete, and reorder orchestration
├── useModelDetailConnectionMutations.ts
├── useModelDetailDialogState.ts
├── useModelDetailDataSupport.ts      # Default form factories, redirect targets, optimistic helpers
├── useModelDetailModelForm.ts
├── useConnectionHealthChecks.ts
├── connectionProbeBehavior.ts       # OpenAI probe variant decomposition and normalization helpers
├── useConnectionFocus.ts
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
- Health checks and spending-summary loading: `useConnectionHealthChecks.ts`, `useModelDetailBootstrap.ts`, `useModelDetailData.ts`, `OverviewCards.tsx`
- Default forms, ordered proxy-target options, strategy summary helpers, and optimistic helpers: `useModelDetailDataSupport.ts`, `useModelDetailModelForm.ts`, `ModelSettingsDialog.tsx`, `ProxyTargetsCard.tsx`
- OpenAI probe variant decomposition and normalization: `connectionProbeBehavior.ts`
- Connection list shell plus local cluster: `ConnectionsList.tsx`, `connections-list/`
- Model-scoped loadbalance event refresh, paging, and detail wiring: `LoadbalanceEventsTab.tsx`, `useModelLoadbalanceEvents.ts`, `../../components/AGENTS.md`
- Current recovery-state fetch and reset actions: `useModelLoadbalanceCurrentState.ts`
- Shared latency and connection-label formatting: `modelDetailMetricsAndPaths.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `ModelDetailPage.tsx` and `ProxyModelDetailPage.tsx` thin. `useModelDetailData.ts` owns bootstrap, dialog state, and the cross-hook composition layer.
- Fetch model, endpoints, model list, and pricing templates in parallel during bootstrap.
- Use `Promise.allSettled` for health-check batches so one failing connection does not collapse the page.
- Keep model loadbalance current state in `useModelLoadbalanceCurrentState.ts`, including refresh and reset actions, instead of scattering cooldown/ban state inside cards or tabs.
- Keep optimistic priority reordering in the hook layer plus the connection-list helpers, and revert UI order if the backend PATCH fails.
- Keep connection-card recovery copy in `connections-list/ConnectionCardSectionsShared.ts`; tone labels and reset wording in `connections-list/ConnectionCardCooldownState.tsx` should stay presentation-only.
- Keep loadbalance event badge/detail rendering in the shared `src/components/loadbalance/` components; `LoadbalanceEventsTab.tsx` should remain a thin page shell.
- Keep proxy-target option building and update payload shaping in `useModelDetailDataSupport.ts` / `useModelDetailModelForm.ts`; proxy-target card/dialog rendering should stay presentation-focused.
- Keep OpenAI probe variant decomposition and normalization in `connectionProbeBehavior.ts` instead of scattering probe endpoint logic across dialog or form files.
- Treat `connections-list/` as a local cluster that stays documented here. It supports the parent route and should not get its own AGENTS file.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS

- Do not move orchestration state back into `ModelDetailPage.tsx`.
- Do not duplicate default form factories or redirect-target logic outside `useModelDetailDataSupport.ts`.
- Do not manage routing priority from `ConnectionDialog.tsx`. Ordering belongs to the connection-list flow.
- Do not leave ban-aware wording trapped only in one connection-card component; shared current-state copy must stay centralized.
- Do not split `connections-list/` into a separate AGENTS file. This parent doc owns that cluster.
