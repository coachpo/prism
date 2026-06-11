# FRONTEND MODEL DETAIL COMPATIBILITY CLUSTER

## OVERVIEW
`pages/model-detail/` keeps model-detail helpers still imported by `src/features/models/detail/` and focused tests. The feature route owns page composition, tabs removal state, terminal-target rendering, and request-log handoff.

## STRUCTURE
```text
model-detail/
├── AccessTargetsCard.tsx
├── ConnectionDialog.tsx
├── connectionProbeBehavior.ts
├── ModelDetailHeader.tsx
├── modelDetailMetricsAndPaths.ts
├── OverviewCards.tsx
├── useConnectionFocus.ts
├── useConnectionHealthChecks.ts
├── useModelDetailBootstrap.ts
├── useModelDetailConnectionFlows.ts
├── useModelDetailConnectionMutations.ts
├── useModelDetailDataSupport.ts
├── useModelDetailDialogState.ts
├── useModelDetailModelForm.ts
└── useModelLoadbalanceCurrentState.ts
```

## WHERE TO LOOK
- Feature route and page composition: `../../features/models/detail/`
- Bootstrap fetches, focus handoff, and redirect handling: `useModelDetailBootstrap.ts`, `useConnectionFocus.ts`
- Connection create, edit, delete, and reorder flows: `useModelDetailConnectionFlows.ts`, `useModelDetailConnectionMutations.ts`, `useModelDetailDialogState.ts`
- Default forms, ordered access-target options, strategy summary helpers, and optimistic helpers: `useModelDetailDataSupport.ts`, `useModelDetailModelForm.ts`, `AccessTargetsCard.tsx`
- OpenAI probe variant decomposition and normalization: `connectionProbeBehavior.ts`
- Health checks and spending-summary loading: `useConnectionHealthChecks.ts`, `useModelDetailBootstrap.ts`, `OverviewCards.tsx`
- Current Ban Policy retry-window state fetch and reset actions: `useModelLoadbalanceCurrentState.ts`
- Shared latency and connection-label formatting: `modelDetailMetricsAndPaths.ts`
- E2E seams for model-to-request-log handoff and connection probe behavior: `../../../tests/e2e/model-detail-request-logs-handoff.spec.ts`, `../../../tests/e2e/model-detail-connection-dialog-probe.spec.ts`

## CONVENTIONS
- Keep access-target option building and update payload shaping in `useModelDetailDataSupport.ts` / `useModelDetailModelForm.ts`; access-target card/dialog rendering should stay presentation-focused.
- Keep OpenAI probe variant decomposition and normalization in `connectionProbeBehavior.ts` instead of scattering probe endpoint logic across dialog or form files.
- Do not duplicate default form factories or redirect-target logic outside `useModelDetailDataSupport.ts`.
- Do not manage routing priority from `ConnectionDialog.tsx`; ordering belongs to the connection-flow hooks.
