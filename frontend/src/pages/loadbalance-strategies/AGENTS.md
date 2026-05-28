# FRONTEND LOADBALANCE STRATEGIES DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/loadbalance-strategies/` owns the dedicated strategy-management route behind `../LoadbalanceStrategiesPage.tsx`. It covers the profile-scoped strategy list, create or edit dialog flows, delete confirmation, and the form normalization that mirrors the backend legacy Ban Policy contract for model access routing.

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
- Form defaults, validation, and request payload shaping for the legacy Ban Policy contract: strategies carry `legacy_strategy_type`, failure status codes, retry-window settings, retry attempt limits, and ban mode fields through `loadbalanceStrategyFormState.ts`
- Table rendering and destructive flow entrypoints: `LoadbalanceStrategiesTable.tsx`, `DeleteLoadbalanceStrategyDialog.tsx`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep backend access on the shared `api.*` boundary; this page should not create a parallel fetch layer.
- Keep strategy form normalization and request shaping in `loadbalanceStrategyFormState.ts` rather than scattering the rules across dialogs.
- Match the CRUD/page shell pattern used by other profile-scoped management pages such as pricing templates.
- Keep legacy routing and Ban Policy fields on the existing strategy dialog.
- Keep retry-window fields explicit in form state: failure status codes, base retry delay, backoff, jitter, maximum retry delay, retry attempts, ban mode, and ban duration.
- Keep failure-status editing and Ban Policy payload normalization inside `loadbalanceStrategyFormState.ts`; do not scatter contract shaping across dialog components.
- Keep summary wording in `LoadbalanceStrategiesTable.tsx` and shared page data helpers; do not duplicate retry-window or ban labels elsewhere.
- Keep the contract forward-only. Do not add compatibility shims, silent coercion, or a fallback path that reintroduces removed strategy families.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not let table components own API calls directly when `useLoadbalanceStrategiesPageData.ts` already centralizes CRUD orchestration.
- Do not reintroduce model-level cooldown, removed failover-policy, or removed routing-policy fields outside this strategy UI.
- Do not split ban controls into a second dialog or page; they belong to the existing strategy dialog.
- Do not create a second policy-management page or move strategy assignment out of the existing model dialogs.
- Do not add extra strategy family labels, selector options, or summary copy.
