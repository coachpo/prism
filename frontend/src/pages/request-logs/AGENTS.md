# FRONTEND REQUEST LOGS DOMAIN KNOWLEDGE BASE

## OVERVIEW

`pages/request-logs/` owns the investigation flow for runtime traffic: retained browse filtering, exact-request focus mode, requested-model and final-target observability, request-time audit provenance, Default-profile spend rendering, stream telemetry, and detailed payload inspection. This parent also covers the local `detail/` cluster, while URL-state, exact-request behavior, and sheet-scoped clipboard fallback stay local here.

## STRUCTURE

```
request-logs/
├── queryParams.ts               # URL-state contract for retained browse filters, view, sort, and pagination
├── useRequestLogPageState.ts    # Search-param orchestration and exact-request mode
├── useRequestLogsPageData.ts    # Server fetches, chain-view flattening, and retained filter-option bootstrap
├── useRequestLogDetail.ts       # Exact-request detail fetch, not-found handling, and refresh
├── useRequestLogChain.ts        # Retained ingress chain fetch for the detail sheet
├── requestLogSavedViews.ts      # Versioned saved canonical views (localStorage)
├── requestLogColumnPreferences.ts # Versioned column-visibility preferences (localStorage)
├── RequestLogAuditPage.tsx      # Dedicated full audit page
├── useDedicatedRequestLogAudit.ts # Dedicated full audit page detail lookup
├── requestLogAuditRoute.ts      # Audit-page id parsing and path building
├── requestLogAuditWindow.ts     # Dedicated audit lookup window helper
├── RequestLogAuditWindowBar.tsx # Permanent disclosure of the frontend-chosen audit query bound
├── AuditCaptureLedger.tsx       # Bytes seen, kept, and dropped with the reason capture stopped
├── requestLogAuditState.ts      # Audit capture mode and request-detail audit state helpers
├── streamTelemetry.ts           # Stream-outcome, TTFT, and rate helpers for request-log views
├── columns.tsx                  # Table column definitions (nine core + pricing state) and scoped status/duration helpers
├── pricingExplanation.ts        # Unpriced-cause, token-component, and typed selection-state/card-role classification for rows and detail
├── RequestLogsViewToolbar.tsx   # View switcher plus the controls both views share (columns, page size, export)
├── FiltersBar.tsx               # UI shell for retained browse filters plus refresh/clear actions
├── FiltersBar.constants.ts      # Filter option constants and shared filter presentation helpers
├── FiltersBarPrimaryFilters.tsx # Retained filter row composition (pricing_status four-state)
├── ActiveFilterChips.tsx        # Every filter actually in effect, as closable chips
├── RequestLogsTable.tsx         # Adaptive-height virtualized attempt list with server chain cursors
├── IngressChainsTable.tsx       # Default server-side retained ingress-chain view
├── requestLogsCsv.ts            # Server-side full filtered CSV download helper
├── RequestLogDetailSheet.tsx    # Overview-only request inspection drawer, retained-chain section, clipboard fallback root
├── RequestFocusBanner.tsx       # Exact-request mode banner and exit action
├── detail/                      # Parent-covered overview, payload, and shared detail helpers
│   ├── RequestLogOverviewTab.tsx   # Overview tab: routing, timing, usage, spend
│   ├── RequestLogPricingEvidence.tsx # Typed kind/state/role/resolution and schedule evidence
│   ├── RequestLogPayloadBlock.tsx  # Payload viewer block with the content-aware view switch
│   ├── requestLogPayloadDocuments.ts # Payload document model shared by the viewer
│   ├── payloadDocumentViewModel.ts # Content-aware payload views (消息/JSON 事件/原始 SSE/JSON/原始文本/不可解析)
│   ├── payloadViewLabels.ts     # View-label mapping
│   ├── sseFraming.ts            # SSE framing (LF/CRLF/CR-only, BOM, multi-line data, [DONE], incomplete tails)
│   ├── streamTranscript.ts      # Operation-aware stream accumulation with tool calls/results
│   ├── requestLogDetailShared.tsx  # Shared detail rows, stats, section cards, API-family pill
│   ├── requestLogStatus.ts       # Status intent/tone presentation
│   └── requestLogClipboard.ts    # Detail clipboard action and localized toast side effect
└── *.test.ts(x)                 # Saved views, preferences, audit route, and audit lookup coverage
```

## WHERE TO LOOK

- Investigation flow and state, including URL-state and exact-request mode: `useRequestLogsPageData.ts`, `useRequestLogPageState.ts`
- Route-shell copy, empty-state messaging, and locale-aware detail labels: `../RequestLogsPage.tsx`, `@/i18n/useLocale`, `@/i18n/AGENTS.md`
- Retained browse-filter contract and defaults: `queryParams.ts`
- Table columns, row actions, and detail-entry affordances: `columns.tsx`, `RequestLogsTable.tsx`
- Filter-bar composition and shared filter constants: `FiltersBar.constants.ts`, `FiltersBarPrimaryFilters.tsx`, `FiltersBar.tsx`
- Detail sheet, exact-request fetch, audit capture state, and sheet-scoped clipboard fallback: `RequestLogDetailSheet.tsx`, `useRequestLogDetail.ts`, `requestLogAuditState.ts`
- Stream telemetry helpers and TTFT/rate display logic: `streamTelemetry.ts`, `detail/RequestLogOverviewTab.tsx`
- Cache-read share on the request detail row and unpriced-cause explanations: `detail/RequestLogOverviewTab.tsx`, `../../features/observe/cacheReadShare.ts`, `pricingExplanation.ts`
- E2E seam for exact-request mode and dedicated audit-page states: `../../../tests/e2e/request-log-dedicated-audit-page.spec.ts`; shared request-log fixtures live in `../../../tests/e2e/request-log-dedicated-audit-fixtures.ts`.
- Parent-covered detail cluster helpers: `detail/RequestLogOverviewTab.tsx`, `detail/RequestLogPayloadBlock.tsx`, `detail/requestLogDetailShared.tsx`, `detail/requestLogStatus.ts`, `detail/requestLogClipboard.ts`
- Reporting-currency trust and spend display coupling: `../../context/ReportingCurrencyContext.tsx`, `../../lib/reportingCurrency.ts`, `detail/RequestLogOverviewTab.tsx`

## CONVENTIONS

- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing request-log surface is itself a shipped contract or guardrail.
- Treat URL as the source of truth for the retained browse filters to support deep-linking.
- Keep request-log browse defaults on the URL contract: `time_range=24h` is the page default and is omitted from generated default URLs.
- Send the selected `time_range` (or `custom` with explicit bounds) to the backend for attempts, ingress chains, and CSV export; the Requests owner resolves the effective window from actual coverage, so a browser-generated `from_time` is not the coverage authority.
- Keep status URL aliases and backend filters aligned: `status=success` round-trips to `status_family=2xx`, while `status=client_error` and `status=error` map to `4xx` and `5xx`.
- Keep `status_code` as an exact numeric status filter and `error_text` as the backend `error_detail ILIKE` substring filter.
- Keep `pricing_status` as the four-state request-log pricing filter (`all|priced|unpriced|ineligible|unknown`); never generate or accept the retired `priced` boolean alias. `unpriced_reason` stays aligned with backend reason codes: `PRICING_DISABLED`, `MISSING_TOKEN_USAGE`, `STREAM_USAGE_UNAVAILABLE`, and `MISSING_PRICE_DATA`.
- Keep CSV export server-side: `api.stats.exportCsv()` downloads the full filtered file from `/api/stats/requests/export`; never assemble CSV from currently loaded table rows.
- Keep the default view as the server-side retained ingress chain (`view=ingress_chains`) with signed chain cursors; the table paginates by `chain_cursor`, not by `offset`.
- Keep row scoping strict: render `upstream_status_code`/`gateway_status_code`/`legacy_status_code` by `row_kind` and never COALESCE across scopes; the `pricing_state` column is first-class in the default column set.
- Keep BIGINT request-log IDs as decimal strings end-to-end; never convert them to JS numbers.
- Keep the payload viewer content-aware: streaming SSE offers 消息/JSON 事件/原始 SSE with real per-view content, non-stream JSON offers 消息/JSON, and binary/invalid-UTF-8 bodies are unparseable; never render the same stored text as two modes.
- Keep saved views and column preferences versioned in localStorage with a schema version; saved views omit transient pagination/selection anchors.
- Keep the filter bar compact: request-ID search + time + triage chips + More Filters toggle visible; the remaining controls collapse.
- Keep the sheet's retained-chain section server-owned; never reconstruct the chain client-side from the current page.
- Keep audit payload fetching isolated to the dedicated full audit page. The overview drawer must not fetch audit payloads.
- Use exact-request mode (`request_id`) to switch from paginated browsing to a single-request investigation workflow, and keep that mode local to the request-logs page.
- Keep retained browse filtering on `ingress_request_id`, `model_id`, `endpoint_id`, `client_rule_id`, `resolved_target_model_id`, `status_family`, `status_code`, `error_text`, `pricing_status`, `unpriced_reason`, `time_range`, `view`, `sort_by`, `sort_order`, and `chain_cursor`; URL stays the source of truth for deep links.
- Keep `pricing_card_role` and `pricing_selection_state` as independent retained-row filters, with typed options round-tripped through URL state and server-side CSV export.
- Keep user-facing copy on the shared locale boundary through `useLocale()`, while timestamp formatting continues to flow through `useTimezone()`.
- Keep audit capture mode and detail-state helpers in `requestLogAuditState.ts` instead of re-deriving them inside detail tabs or fetch hooks.
- Derive audit visibility from request-time provenance: disabled audit means no linked-audit fetch; enabled without body capture is metadata-only; body presence alone is not the contract.
- Keep stream telemetry in `streamTelemetry.ts` and parent detail helpers instead of recomputing TTFT or request-rate state in shared widgets.
- Keep copy actions on shared clipboard helpers. `RequestLogDetailSheet.tsx` intentionally provides `[data-clipboard-fallback-root]` so browser fallback UI stays inside the sheet instead of triggering downloads.
- Keep request-log cost labels tied to `useReportingCurrencyContext()` so fallback or verified Default-profile reporting-currency trust is visible in detail views.
- Render pricing evidence through `pricing_selection_state` and `pricing_card_role` independently. `unresolved` is a failure surface with `pricing_resolution_kind`; missing evidence is not a base-card decision. Peak/valley detail may show timezone, frozen decision time, local weekday/minute, and digest only when present. CSV column order must match the backend generic evidence columns.
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
