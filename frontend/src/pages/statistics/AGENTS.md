# FRONTEND STATISTICS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/statistics/` owns the shared usage-statistics implementation consumed by `../DashboardPage.tsx`. The dashboard analytics surface is backend-snapshot driven and coordinated by `useUsageStatisticsPageState.ts` for local persisted presentation state plus `useUsageStatisticsPageData.ts` for snapshot orchestration. Keep this area focused on aggregate snapshot views split across `charts/`, `sections/`, and `tables/`.

## STRUCTURE
```
statistics/
├── charts/                         # Usage-snapshot charts and local chart helpers
├── sections/                       # Page sections and snapshot summaries
├── tables/                         # Aggregate endpoint, model, and proxy-api-key tables
├── UsageStatisticsPageSkeleton.tsx # Page-level loading shell for the dashboard analytics surface
├── useUsageStatisticsPageData.ts   # Snapshot loading and page-data orchestration
├── useUsageStatisticsPageState.ts  # Local persisted presentation state
└── usageStatisticsStorage.ts       # localStorage persistence helpers
```

## WHERE TO LOOK

- Dashboard analytics shell and top-level section orchestration: `../DashboardPage.tsx`
- Shell copy and presentation labels: `../DashboardPage.tsx`, `@/i18n/useLocale`, `@/i18n/AGENTS.md`
- Snapshot orchestration and persisted presentation state: `useUsageStatisticsPageData.ts`, `useUsageStatisticsPageState.ts`, `usageStatisticsStorage.ts`
- Usage-snapshot charts, sections, and tables: `charts/`, `sections/`, `tables/`
- Aggregate endpoint, model, and proxy-api-key tables: `tables/`
- Dashboard analytics loading shell: `UsageStatisticsPageSkeleton.tsx`
- Shared statistics cards and chart wrappers used by dashboard analytics: `../../components/AGENTS.md`
- Shared presentation helpers and timezone-aware formatting inputs: `@/hooks/useTimezone`, `@/components/ui/chart.tsx`
- Reporting-currency trust and cost formatting seams: `@/context/ReportingCurrencyContext`, `@/lib/reportingCurrency`, `@/lib/costing`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Treat local persisted presentation state as the source of truth for dashboard analytics preferences that should survive reloads. The current shared statistics surface does not expose a dedicated route-level query-param contract.
- Keep usage-snapshot orchestration in `useUsageStatisticsPageData.ts`, not in section or table components.
- Treat backend snapshot currency as the source of truth for rendered statistics money. The frontend reporting-currency layer controls selected-profile readiness and trust, but statistics components should not refetch costing settings or recompute backend stats.
- Keep this area aggregate-focused. Request-log investigation belongs on `/request-logs`, not inside the statistics tables.
- Keep analytics-shell and section copy on the shared locale boundary through `useLocale()`, and keep locale-aware formatting on the shared helpers rather than page-local string logic.
- The dense `charts/`, `sections/`, and `tables/` subfolders stay parent-covered. Do not add extra AGENTS files for them.
- Keep null-vs-zero rendering differences visible in helpers and copy, so missing data stays distinct from a true zero value.
- Keep selected-profile reporting-currency fallback or verified trust visible through the shared currency/cost helpers; do not invent page-local trust states.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS

- Do not recreate the tab/query-param model inside section or table components.
- Do not regress null-vs-zero rendering for usage or cost metrics. The shared statistics surface depends on that distinction for triage.
- Do not create standalone AGENTS files for `charts/`, `sections/`, or `tables/`. This parent doc owns those local clusters.
- Do not duplicate backend stats aggregation or spend math in the frontend.
- Do not fetch costing settings from statistics components when the stats snapshot and reporting-currency context already own the display contract.
