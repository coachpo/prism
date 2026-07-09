# FRONTEND REQUEST LOGS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/request-logs/` owns the investigation flow for runtime traffic: retained browse filtering, exact-request focus mode, requested-model and final-target observability, request-time audit provenance, Default-profile spend rendering, stream telemetry, and detailed payload inspection. This parent also covers the local `detail/` cluster, while URL-state, exact-request behavior, and sheet-scoped clipboard fallback stay local here.

## STRUCTURE
```
request-logs/
├── queryParams.ts               # URL-state contract for retained browse filters and pagination
├── useRequestLogPageState.ts    # Search-param orchestration and exact-request mode
├── useRequestLogsPageData.ts    # Server fetches and retained filter-option bootstrap
├── useRequestLogDetail.ts       # Exact-request detail fetch, not-found handling, and refresh
├── useDedicatedRequestLogAudit.ts # Dedicated full audit page detail lookup
├── requestLogAuditWindow.ts     # Dedicated audit lookup window helper
├── requestLogAuditState.ts      # Audit capture mode and request-detail audit state helpers
├── streamTelemetry.ts           # Stream-outcome, TTFT, and rate helpers for request-log views
├── columns.tsx                  # Table column definitions and detail entry affordances
├── FiltersBar.tsx               # UI shell for retained browse filters plus refresh/clear actions
├── FiltersBar.constants.ts      # Filter option constants and shared filter presentation helpers
├── FiltersBarPrimaryFilters.tsx # Retained filter row composition
├── RequestLogsTable.tsx         # Paginated and virtualized log list
├── requestLogsCsv.ts            # Current-page CSV export helper
├── RequestLogDetailSheet.tsx    # Overview-only request inspection drawer and clipboard fallback root
├── RequestFocusBanner.tsx       # Exact-request mode banner and exit action
└── detail/                      # Parent-covered overview, payload, and shared detail helpers
```

## WHERE TO LOOK
- Investigation flow and state, including URL-state and exact-request mode: `useRequestLogsPageData.ts`, `useRequestLogPageState.ts`
- Route-shell copy, empty-state messaging, and locale-aware detail labels: `../RequestLogsPage.tsx`, `@/i18n/useLocale`, `@/i18n/AGENTS.md`
- Retained browse-filter contract and defaults: `queryParams.ts`
- Table columns, row actions, and detail-entry affordances: `columns.tsx`, `RequestLogsTable.tsx`
- Filter-bar composition and shared filter constants: `FiltersBar.constants.ts`, `FiltersBarPrimaryFilters.tsx`, `FiltersBar.tsx`
- Detail sheet, exact-request fetch, audit capture state, and sheet-scoped clipboard fallback: `RequestLogDetailSheet.tsx`, `useRequestLogDetail.ts`, `requestLogAuditState.ts`
- Stream telemetry helpers and TTFT/rate display logic: `streamTelemetry.ts`, `detail/RequestLogOverviewTab.tsx`
- E2E seam for exact-request mode and dedicated audit-page states: `../../../tests/e2e/request-log-dedicated-audit-page.spec.ts`; shared request-log fixtures live in `../../../tests/e2e/request-log-dedicated-audit-fixtures.ts`.
- Parent-covered detail cluster helpers: `detail/RequestLogOverviewTab.tsx`, `detail/RequestLogPayloadBlock.tsx`, `detail/requestLogDetailShared.tsx`, `detail/requestLogDetailUtils.ts`
- Reporting-currency trust and spend display coupling: `../../context/ReportingCurrencyContext.tsx`, `../../lib/reportingCurrency.ts`, `detail/RequestLogOverviewTab.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing request-log surface is itself a shipped contract or guardrail.
- Treat URL as the source of truth for the retained browse filters to support deep-linking.
- Keep request-log browse defaults on the URL contract: `time_range=24h` is the page default and is omitted from generated default URLs.
- Keep status URL aliases and backend filters aligned: `status=success` round-trips to `status_family=2xx`, while `status=client_error` and `status=error` map to `4xx` and `5xx`.
- Keep `status_code` as an exact numeric status filter and `error_text` as the backend `error_detail ILIKE` substring filter.
- Keep `priced` as the request-log pricing-state filter (`all|true|false`) and `unpriced_reason` aligned with backend reason codes: `PRICING_DISABLED`, `MISSING_TOKEN_USAGE`, `STREAM_USAGE_UNAVAILABLE`, and `MISSING_PRICE_DATA`.
- Keep CSV export client-only and scoped to currently loaded table rows; page sizes are capped by `PAGE_SIZE_OPTIONS`, so full-range export needs a backend endpoint first.
- Keep audit payload fetching isolated to the dedicated full audit page. The overview drawer must not fetch audit payloads.
- Use exact-request mode (`request_id`) to switch from paginated browsing to a single-request investigation workflow, and keep that mode local to the request-logs page.
- Keep retained browse filtering on `ingress_request_id`, `model_id`, `endpoint_id`, `client_rule_id`, `resolved_target_model_id`, `status_family`, `status_code`, `error_text`, `priced`, `unpriced_reason`, and `time_range`.
- Keep user-facing copy on the shared locale boundary through `useLocale()`, while timestamp formatting continues to flow through `useTimezone()`.
- Keep audit capture mode and detail-state helpers in `requestLogAuditState.ts` instead of re-deriving them inside detail tabs or fetch hooks.
- Derive audit visibility from request-time provenance: disabled audit means no linked-audit fetch; enabled without body capture is metadata-only; body presence alone is not the contract.
- Keep stream telemetry in `streamTelemetry.ts` and parent detail helpers instead of recomputing TTFT or request-rate state in shared widgets.
- Keep copy actions on shared clipboard helpers. `RequestLogDetailSheet.tsx` intentionally provides `[data-clipboard-fallback-root]` so browser fallback UI stays inside the sheet instead of triggering downloads.
- Keep request-log cost labels tied to `useReportingCurrencyContext()` so fallback or verified Default-profile reporting-currency trust is visible in detail views.
- Keep `detail/` parent-covered here. Those helpers support the request-log sheet only and should not get a separate AGENTS file.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not stale-claim that request logs are missing from the route map.
- Do not duplicate filter parsing outside `queryParams.ts`.
- Do not fetch audit payloads during normal table browsing or from the overview drawer.
- Do not split `request-logs/detail/` into a separate AGENTS file while this parent already owns that cluster.
- Do not replace the sheet-scoped clipboard fallback with a global DOM fallback or a download-based workaround.
- Do not render request-log spend independently from the Default-profile reporting-currency state.
