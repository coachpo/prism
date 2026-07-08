# FRONTEND STATISTICS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/statistics/` owns the shared usage-statistics implementation consumed by `../DashboardPage.tsx`. The dashboard analytics surface is backend-snapshot driven, with local persisted presentation state in `useUsageStatisticsPageState.ts`, orchestration in `useUsageStatisticsPageData.ts`, realtime analytics updates in `useUsageStatisticsRealtimeData.ts`, and shared rendering in `UsageStatisticsContent.tsx`.

## STRUCTURE
```
statistics/
├── charts/                          # Usage-snapshot charts and local chart helpers
├── sections/                        # Page sections and snapshot summaries
├── tables/                          # Aggregate endpoint, model, and proxy-api-key tables
├── UsageStatisticsContent.tsx       # Shared analytics content consumed by the dashboard shell
├── UsageStatisticsPageSkeleton.tsx  # Page-level loading shell for the dashboard analytics surface
├── useUsageStatisticsPageData.ts    # Snapshot loading, websocket merge, and page-data orchestration
├── useUsageStatisticsPageState.ts   # Local persisted presentation state
├── useUsageStatisticsRealtimeData.ts # Analytics websocket subscription and refresh helpers
└── usageStatisticsStorage.ts        # localStorage persistence helpers
```

## WHERE TO LOOK
- Dashboard analytics shell and top-level section orchestration, with no standalone `/statistics` route in `App.tsx`: `../DashboardPage.tsx`, `../dashboard/DashboardAnalyticsContent.tsx`
- Shared analytics content surface: `UsageStatisticsContent.tsx`
- Snapshot orchestration, realtime merge, and persisted presentation state: `useUsageStatisticsPageData.ts`, `useUsageStatisticsRealtimeData.ts`, `useUsageStatisticsPageState.ts`, `usageStatisticsStorage.ts`
- Usage-snapshot charts, sections, and tables: `charts/`, `sections/`, `tables/`
- Dashboard analytics loading shell: `UsageStatisticsPageSkeleton.tsx`
- Shared statistics cards and chart wrappers used by dashboard analytics: `../../components/AGENTS.md`
- Shared presentation helpers and timezone-aware formatting inputs: `@/hooks/useTimezone`, `@/components/ui/chart.tsx`
- Reporting-currency trust and cost-formatting seams: `@/context/ReportingCurrencyContext`, `@/lib/reportingCurrency`, `@/lib/costing`
- E2E seams for shared chart statistics, TTFT percentiles, output-rate columns, selected-model totals, proxy-key labels, and native analytics websocket updates: `../../../tests/e2e/shared-chart-statistics.spec.ts`, `../../../tests/e2e/statistics-ttft.spec.ts`, `../../../tests/e2e/statistics-token-rate.spec.ts`, `../../../tests/e2e/statistics-filtered-totals.spec.ts`, `../../../tests/e2e/statistics-proxy-api-key-label.spec.ts`, `../../../tests/e2e/analytics-websocket-native.spec.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Treat local persisted presentation state as the source of truth for dashboard analytics preferences that should survive reloads. The shared statistics surface does not expose a dedicated route-level query-param contract.
- Keep usage-snapshot orchestration in `useUsageStatisticsPageData.ts`, not in section or table components.
- Keep analytics websocket subscription and stale-sequence handling in `useUsageStatisticsRealtimeData.ts`, not in the dashboard shell.
- Treat backend snapshot currency as the source of truth for rendered statistics money. The frontend reporting-currency layer controls Default-profile readiness and trust, but statistics components should not refetch costing settings or recompute backend stats.
- Keep this area aggregate-focused. Request-log investigation belongs on `/observe/requests`, not inside the statistics tables.
- Keep analytics-shell and section copy on the shared locale boundary through `useLocale()`, and keep locale-aware formatting on the shared helpers rather than page-local string logic.
- The dense `charts/`, `sections/`, and `tables/` subfolders stay parent-covered. Do not add extra AGENTS files for them.
- Keep null-vs-zero rendering differences visible in helpers and copy, so missing data stays distinct from a true zero value.
- Keep Default-profile reporting-currency fallback or verified trust visible through the shared currency/cost helpers; do not invent page-local trust states.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not recreate the tab or query-param model inside section or table components.
- Do not regress null-vs-zero rendering for usage or cost metrics. The shared statistics surface depends on that distinction for triage.
- Do not create standalone AGENTS files for `charts/`, `sections/`, or `tables/`. This parent doc owns those local clusters.
- Do not duplicate backend stats aggregation or spend math in the frontend.
- Do not fetch costing settings from statistics components when the stats snapshot and reporting-currency context already own the display contract.
