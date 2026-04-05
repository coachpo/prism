# FRONTEND STATISTICS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/statistics/` owns the unified `/statistics` route behind `../StatisticsPage.tsx`. The live page is snapshot-driven, not tab-driven, and it is coordinated by `useUsageStatisticsPageState.ts` for local persisted presentation state plus `useUsageStatisticsPageData.ts` for snapshot orchestration. Keep this route focused on aggregate snapshot views split across `charts/`, `sections/`, and `tables/`.

## STRUCTURE
```
statistics/
├── charts/                         # Usage-snapshot charts and local chart helpers
├── sections/                       # Page sections and snapshot summaries
├── tables/                         # Aggregate endpoint, model, and proxy-api-key tables
├── UsageStatisticsPageSkeleton.tsx # Page-level loading shell for the unified route
├── StatisticsPageSkeleton.tsx      # Secondary skeleton kept for local fallback/test coverage
├── useUsageStatisticsPageData.ts   # Snapshot loading and page-data orchestration
├── useUsageStatisticsPageState.ts  # Local persisted presentation state
├── usageStatisticsStorage.ts       # localStorage persistence helpers
└── __tests__/                      # State, data, refresh, and table coverage
```

## WHERE TO LOOK

- Thin route shell and top-level section orchestration: `../StatisticsPage.tsx`
- Route-shell copy and presentation labels: `../StatisticsPage.tsx`, `@/i18n/useLocale`, `@/i18n/AGENTS.md`
- Snapshot orchestration and persisted presentation state: `useUsageStatisticsPageData.ts`, `useUsageStatisticsPageState.ts`, `usageStatisticsStorage.ts`
- Usage-snapshot charts, sections, and tables: `charts/`, `sections/`, `tables/`
- Aggregate endpoint, model, and proxy-api-key tables: `tables/`
- Unified route loading shell: `UsageStatisticsPageSkeleton.tsx`
- Shared statistics cards and chart wrappers: `../../components/AGENTS.md`
- Shared presentation helpers and timezone-aware formatting inputs: `@/hooks/useTimezone`, `@/components/ui/chart.tsx`

## CONVENTIONS

- Treat local persisted presentation state as the source of truth for page preferences that should survive reloads. The current unified statistics page does not expose a dedicated route-level query-param contract.
- Keep unified usage-snapshot orchestration in `useUsageStatisticsPageData.ts`, not in section or table components.
- Keep this route aggregate-focused. Request-log investigation belongs on `/request-logs`, not inside the statistics tables.
- Keep route-shell and section copy on the shared locale boundary through `useLocale()`, and keep locale-aware formatting on the shared helpers rather than page-local string logic.
- The dense `charts/`, `sections/`, and `tables/` subfolders stay parent-covered. Do not add extra AGENTS files for them.
- Keep null-vs-zero rendering differences visible in helpers and copy, so missing data stays distinct from a true zero value.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not recreate the tab/query-param model inside section or table components.
- Do not regress null-vs-zero rendering for usage or cost metrics. Statistics depends on that distinction for triage.
- Do not create standalone AGENTS files for `charts/`, `sections/`, or `tables/`. This parent doc owns those local clusters.
