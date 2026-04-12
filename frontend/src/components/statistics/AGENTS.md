# FRONTEND STATISTICS COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`src/components/statistics/` holds the shared renderers used by the `/statistics` route and the dashboard statistics surface. It currently owns the spending card and token metric cell helpers, and stays presentation-only.

## WHERE TO LOOK
- `TopSpendingCard.tsx`
- `TokenMetricCell.tsx`
- `../../pages/dashboard/StatisticsPage.tsx`
- `../../pages/statistics/UsageStatisticsContent.tsx`

## CONVENTIONS
- Keep these components presentation-first.
- Keep statistics page orchestration and data fetching in the page layer.
- Keep null-vs-zero rendering rules and metric formatting decisions in the page helpers, not in these renderers.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not move snapshot orchestration or request-log drilldown state into these shared renderers.
- Do not duplicate page-local null-vs-zero rendering rules when the statistics page helpers already shape the inputs.
- Do not add page-specific fetches or route state to this shared leaf.
