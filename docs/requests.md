# Requests Workflow Contract

This specification covers retained ingress and attempt investigation. Use the [Observability API](observability-api.md) for wire fields and the [design system](../frontend/DESIGN.md) for UI patterns.

**Scope:** Frontend route `/observe/requests` and its request-investigation helper cluster

## 1. Overview

The Requests page is Prism's dedicated request-browser and investigation surface for proxied traffic. It is mounted at `/observe/requests`. It provides a Default-profile-pinned view for browsing request history through server-backed filters, a retained-ingress chain view as the default investigation unit, and an overview detail sheet with a unified failure projection. Readable saved request/response content lives on a dedicated request-content page, with explicit capture and retention limits.

The backend request-log and audit APIs remain the source of truth. The frontend route is responsible for presenting that data in an operator-friendly investigation workflow without changing runtime proxy semantics. Canonical browse identity uses `ingress_model_id` for the requested model and `attempt_target_model_id` for a retained attempt target; ambiguous model aliases are not restored. Ordinary URL state also carries endpoint, Terminal Target, caller client rule, status/error/pricing, triage, time, view, sort and pagination. Observe drill-down adds signed repeated `final_*`/attempt selectors, while exact single-request investigation uses a positive int64 decimal `request_id` without conversion to a JavaScript number.

The request-log route uses split HTTP contracts: a slim v2 row payload for attempt browsing, a chain envelope for the default ingress-chain view, and a v2 detail payload for the sheet. The `priced` boolean alias is not accepted anywhere; pricing filtering uses the four-state `pricing_status` (`priced|unpriced|ineligible|unknown`) and the strict backend rejects unknown query keys with `422 unknown_query_key`.

## 2. Goals

- Provide a dedicated request-history route at `/observe/requests`.
- Support deep investigation of a single request through URL-addressable state.
- Keep the retained browse filters server-backed and URL-addressable.
- Default to the server-side retained ingress chain as the investigation unit.
- Load saved conversation content only on explicit inspection; explain missing, unsupported, or truncated capture without displaying raw diagnostic data.
- Support implemented drill-down entry points from dashboard overview and dashboard recent activity.
- Show requested model identity separately from final target, selected Terminal Target, and endpoint.

## 3. Non-Goals

- Replace dashboard or statistics summaries.
- Re-derive retained chains client-side from the current page; the chain grouping is server-owned and paged by ingress with signed cursors.
- Change frozen Default-profile runtime routing behavior for `/v1/*` and `/v1beta/*`.

## 4. Route Responsibilities

The page route should act as a thin orchestration shell with four primary responsibilities:

1. Render page chrome through `OperatorPageShell` and `OperatorPageHeader`.
2. Own URL-backed state through `useRequestLogPageState()`.
3. Load request data and filter options through `useRequestLogsPageData()`.
4. Compose exact-request focus, filters, the shared toolbar, the selected ingress-chain or attempt table, and the overview detail sheet.

The route should also integrate shared application services:

- Default profile id `1` is frozen for management reads; `X-Profile-Id` may still be sent by shared API code but is ignored.
- `useTimezone()` plus the shared frontend locale boundary for locale-aware timestamp formatting.
- `useLocale()` for route-shell, filter, empty-state, and detail-sheet copy.
- `TooltipProvider` for table and filter affordances.

## 5. URL State Contract

`useRequestLogPageState()` should own the complete search-parameter contract and update query state with `replace: true` semantics so frequent filter changes do not spam browser history.

Supported canonical query parameters:

- Browse filters: `ingress_request_id`, `ingress_model_id`, `endpoint`, `terminal_target_id`, `client_rule_id`, `proxy_api_key_id`, `attempt_target_model_id`, `api_family`, `row_kind`, `status`, `status_code`, `stream_outcome`, `stream_error_kind`, `error_text`, `pricing_status`, `unpriced_reason`, `pricing_card_role`, `pricing_selection_state`, `ingress_final_result`, `confirmed_failover`, `time_range`, paired `from_time`/`to_time`; ingress chains additionally support `cost_segment_key`
- Signed Observe filters: `query_context` plus repeated/comma-equivalent `final_result`, `outcome_detail`, `final_status_code`, `final_stream_outcome`, `final_stream_error_kind`, `final_target_model_id`, `final_endpoint_id`, `final_terminal_target_id`, `final_pricing_status`, `final_unpriced_reason`, `reporting_currency_epoch`, `attempt_trigger`, and `attempt_result`; server-generated Other links additionally use the bounded, typed `final_exclude` complement selector
- View: `view` (`ingress_chains` default | `attempts`), `chain_cursor` for chain outer-page continuation
- Sorting: `sort_by`, `sort_order` (accepted values depend on the view; see the [request query contract](observability-api.md#42-list-request-logs--ingress-chains))
- Pagination: `limit`, `cursor` (attempt view)
- Exact-investigation flow: `request_id`
- Row selection without exact mode: `selected_request_id`

Accepted URL aliases are canonicalized only where the route still owns them (`endpoint_id`, `status_family`, and attempt-page `offset`). `status=client_error` maps to backend `status_family=4xx`; `status=error` maps to backend `status_family=5xx`. Retired ambiguous model aliases and `priced=true|false` are rejected by the backend.

Behavioral requirements:

- Default values should be omitted from the URL.
- Any filter mutation that changes the result set must reset attempt offset and both signed chain cursors to the first page. Cursors bind the normalized cohort/window as well as profile, sort, page size and retention generation, so a manually reused cursor fails closed.
- `request_id` must switch the page into exact-request investigation mode.
- `ingress_request_id` must support grouped investigation of all per-attempt rows created by one incoming runtime request.
- Stale `detail_tab` parameters must be ignored and canonicalized away.
- Chain continuation must never split an ingress identity across pages; the server returns a signed opaque chain cursor.

## 6. Data And API Requirements

### 6.1 Request Log Fetch

Primary APIs:

- `api.stats.requests()` -> `/api/stats/requests` (attempt view)
- `api.stats.chains()` -> `/api/stats/requests?view=ingress_chains` (default)
- dedicated detail fetch -> `/api/stats/requests/{request_id}` (v2 detail with scoped statuses and failure projection)
- full filtered export -> `/api/stats/requests/export` (server-side CSV)

Required behavior:

- Debounce fetches by 300 ms.
- Send the server-supported scope-specific identity, grouping, caller-client, status/error, pricing, triage and time filters without browser-side cohort refinement.
- Keep `ingress_model_id` and `attempt_target_model_id` unchanged on the wire; translate only `endpoint` to `endpoint_id`, status aliases to `status_family`, and attempt `cursor` to `offset`.
- Send `unpriced_reason` only when `pricing_status=unpriced`; other pricing states omit it from backend params.
- Send `ingress_request_id` as an exact server-backed grouping filter when present.
- Keep list browsing on the slim v2 row schema and fetch exact-request sheet data from the dedicated v2 detail endpoint.
- Track fetch ordering so stale responses cannot overwrite newer state.
- CSV export downloads the server-produced full filtered file; the frontend never assembles CSV from the current page.

### 6.2 Filter Option Bootstrap

Both attempts and ingress-chain envelopes require non-null `filter_options.ingress_models`, `filter_options.endpoints`, `filter_options.clients`, and `filter_options.attempt_target_models`; empty sets are `[]`, never omitted. The default chain response therefore bootstraps filters without a hidden attempts request.

Response-owned filter options should become ready when the current list response arrives. `filter_options.clients` entries use `{ client_rule_id, client_label }` and represent enabled User-Agent Client Rules. Selecting one sends `client_rule_id` back to the backend, where matching is caller-only against `caller_user_agent`.

### 6.3 Dedicated Audit Resolution

Detailed audit payloads load only on `/observe/requests/:requestId/audit`. The audit route is request-focused: it first loads `/api/stats/requests/{request_id}`. If that request detail is missing or invalid, the page stops and does not issue audit-list or audit-detail calls. The current UI therefore has no standalone orphan-audit browser even though backend audit rows can retain `request_log_missing` metadata after request-log deletion.

Audit APIs:

- request detail: `api.stats.requestDetail()` -> `/api/stats/requests/{request_id}`
- request-scoped audit list: `api.audit.listForRequestLog()` -> `/api/audit/logs?request_log_id=...`
- selected audit detail: `api.audit.get()` -> `/api/audit/logs/{id}`
- The byte-exact raw-body download endpoints remain available to API clients; the request-content page does not invoke them.

Required behavior:

- Avoid audit fetches during normal table browsing and the overview sheet.
- Skip the audit-list and audit-detail calls when request-time `audit_enabled_at_request` is `false`.
- Treat `audit_capture_bodies_at_request` as the request-time provenance flag: enabled plus false means metadata-only; enabled plus true means full capture. Do not infer capture mode from whether a body happens to be present.
- Derive `from` and `to` as a UTC window of 12 hours before through 12 hours after the request's `created_at`.
- Request at most 20 audit rows per page. `audit_id` selects a row from the current page; when it is absent, the first returned row is selected. An unknown `audit_id` shows a missing-audit state without fetching a detail row.
- Preserve `cursor` in the audit-page URL. Next uses `next_cursor`; Previous clears `cursor` and returns to the first page.
- Keep audit loading isolated from the request-list and sheet detail-fetch lifecycle.

## 7. UX Workflow Requirements

### 7.1 Filter And Triage Workflow

The page sends retained browse filters directly to the owning backend read model. Ordinary triage (`ingress_final_result`, `confirmed_failover`) is tokenless; Observe analysis links carry a signed context and explicit final/attempt selectors. The Client dropdown exposes only enabled rule identity and never regex, `client_scope`, or upstream matching language.

Triage chips are canonical-query shortcuts only:

- **全部**: clears `ingress_final_result`/`confirmed_failover`/`pricing_status` shortcut-owned conditions.
- **最终失败**: writes `ingress_final_result=failed` (finalized ingress cohort).
- **确认故障转移**: writes `confirmed_failover=true`, matched against persisted `attempt_trigger=failover` evidence only.
- **未定价**: writes `pricing_status=unpriced`; `ineligible` and `unknown` never match this chip.

Chip changes must not delete unrelated More Filters conditions. Contradictory conditions return typed `422`; a legal filter with no rows returns a true empty state.

### 7.2 Exact-Request Investigation Workflow

When the route opens with `request_id`, it should stop behaving like a normal paginated browser.

Required behavior:

- Fetch only the targeted request.
- Show `RequestFocusBanner` with an exit action.
- Render a dedicated empty state with a return action when the request is missing.
- Ignore stale `detail_tab` parameters and keep exact-request investigation on the overview-only sheet.

Grouped request-tracking workflow:

- `request_id` remains a one-row deep link for exact attempt investigation.
- `ingress_request_id` groups multiple attempt rows from one incoming runtime request without changing `request_id` semantics.
- Grouped rows show all attempts for one incoming runtime request together.
- The overview sheet explains the requested and actual models, service, attempt sequence, and observed results. Internal grouping and provider-correlation identifiers remain available to the route/API contracts rather than becoming diagnostic fields in the product UI.

### 7.3 Table Workflow

`RequestLogsTable` should support dense browsing at high row counts.

Required behavior:

- Virtualized rows with `45px` row height.
- `10` rows of overscan.
- Adaptive table height: the body fills the remaining shell viewport and is the single scroll container (no fixed component-owned pixel height).
- Sticky headers in all views.
- Default attempt columns: time, status, TTFT, output rate, requested model, attempt target model, Terminal Target, total tokens, known cost, and pricing state. Optional columns include latency, client, reasoning effort, API family, endpoint, stream, and the persisted upstream model ID; column visibility is configurable.
- The ingress-chain view and the Observe Activity feed have no separate pricing-state column: a priced request shows only its known cost, and any other pricing state sits beside the amount.
- Scoped row statuses: upstream rows show `upstream_status_code`, planning/admission rows show `gateway_status_code`, legacy rows show `legacy_status_code`. No COALESCE across scopes.
- Page-size controls limited to `100`, `300`, and `500`, with `100` as the route default.
- Footer controls for page size plus previous and next pagination (attempt view); chain view paginates by signed chain cursor.
- Show `api_family`, requested model, final target model, endpoint, and caller/upstream client display fields without adding browser-side post-filtering.
- CSV export is server-side over the full filtered result set; the frontend downloads the produced file and never assembles CSV from the current page. `GET /api/stats/requests/export` uses RFC 4180 quoting with formula-neutralizing cells, `X-Prism-Export-Row-Count` + `Digest` + exact `Content-Length`, bounded by 100k rows / 128 MiB / 31 days, and `attachment`/`private, no-store`.
- The successful list response carries a non-null `coverage` projection (`known|legacy_unknown`) from the owning actual-coverage snapshot; Requests attempts/chains and CSV export resolve `time_range=1h|6h|24h|7d|30d|all|custom` on the server. When the requested window reaches before the retention floor or actual retained intersection, the page shows an explicit gap instead of pretending the result is complete, and `legacy_unknown` never enters true-empty. A page-size count is not coverage precision.

### 7.4 Detail Sheet Workflow

`RequestLogDetailSheet` exposes an overview-only inspection sheet with request metadata, requested model vs final target model identity, a unified failure projection, token and cost breakdowns, and routing context.

Every successfully loaded request detail provides a link to the dedicated full audit page. The sheet does not conditionally hide that entry and does not fetch audit payloads. The target page then renders one of three request-time states: disabled, metadata-only, or full capture.

Failed rows lead with what is known from the scoped status and stream outcome, what remains uncertain, and an actionable next step. The shared recovery section links to the relevant model and service when those identities can be resolved; missing historical references fall back to an exact model-name search. Raw error text, failure codes, internal paths, and implementation-stage fields do not appear in the sheet or request-content page.

Dense overview requirements:

- Keep the same logical groups: `Request details`, `Routing context`, `Token usage`, and `Cost breakdown`.
- Render a compact summary strip for latency, token, cost, and timestamp context above the grouped sections.
- Pricing presents known cost and currency, its component breakdown, and plain-language reasons for missing or untrusted amounts. Historical limits remain explicit; raw pricing enums, currency-segment keys, and legacy diagnostic values are not displayed or promoted into canonical cost.
- Operation names, translation mode, request paths, and provider diagnostics remain backend detail fields; the product sheet presents user-relevant model, service, outcome, and usage information.
- Keep audit payload loading out of the sheet and scoped to the dedicated full audit page.

### 7.5 Payload Views And Copy

The dedicated request-content page uses the existing audit detail API to read saved request and response bodies on demand. Stored bytes still arrive as Base64 with encoding, capture, and observed/stored-byte metadata; the wire and raw-download contracts remain in [Audit Log Detail](observability-api.md#52-get-audit-log-detail).

- Recognized text formats render user, assistant, and system messages, with natural-language indications of tool activity. Tool arguments/results, headers, raw JSON, JSON events, and raw SSE are not product views or copy targets.
- Streaming content is reassembled through the operation-aware parser. An incomplete or truncated capture keeps a visible limitation; missing or unsupported content never becomes an empty successful conversation.
- Copy operates on the readable extracted text and role labels. It is unavailable when no readable text exists, and clipboard failure retains the existing local fallback.
- Capture-disabled, metadata-only, missing-body, unsupported/binary, and truncated states remain distinct. The page reports the applicable stored-byte limits, ±12-hour query window, and retention gaps without exposing diagnostic payloads.
- Returning to the request list restores the original filters, paging, and selected request through a validated local `return_to` target. Request comparison uses the same readable-content boundary and states that tool parameters, images, and attachments are outside the comparison.

## 8. Module Boundaries

- `queryParams.ts` owns the canonical URL filter/view/sort contract, the `pricing_status` enum, and the triage cohort filters (`ingress_final_result`, `confirmed_failover`).
- `useRequestLogAttempts.ts` owns the attempt-list read/replace lifecycle; `useRequestLogIngressChains.ts` owns ingress-chain cursor, replacement, and nested row append state; `useRequestLogsPageData()` only composes the active view.
- `columns.tsx` owns available columns, the `pricing_state` cell, and scoped status/duration helpers; `requestLogColumnPreferences.ts` selects the default visible set.
- `requestLogColumnPreferences.ts` owns versioned column-visibility preferences (localStorage).
- `requestLogSavedViews.ts` owns versioned saved canonical queries (localStorage); `FiltersBar.tsx` renders the triage chips and the saved-views dropdown.
- `requestLogsCsv.ts` only downloads the server-produced export file with the current filters.
- `detail/RequestLogOverviewTab.tsx` renders the v2 detail as request outcome, model/service context, usage, and pricing. `detail/RequestFailureRecovery.tsx` shares failure explanation and recovery actions with the request-content page; `requestFailurePresentation.ts` maps observed status/stream facts, and `useRequestRecoveryModel.ts` resolves historical model names against the shared model catalog.
- `useRequestLogChain.ts` loads the retained ingress chain for the detail sheet; `RequestLogDetailSheet.tsx` renders the chain section (attempt order, triggers, winner/current markers, completeness).
- `useDedicatedRequestLogAudit.ts` composes the separate request, audit-list, and selected audit-detail read lanes; request-time capture evidence gates the latter two.
- `detail/sseFraming.ts` owns SSE framing; `detail/streamTranscript.ts` owns operation-aware accumulation; `detail/payloadDocumentViewModel.ts` owns the content-aware projection. `detail/RequestLogPayloadBlock.tsx` renders only readable messages and tool-activity notices. Parser-level protocol data remains internal.

## 9. Cross-Route Integrations

- Dashboard overview and recent activity deep-link into `/observe/requests` with `request_id` or `ingress_request_id`.
- The detail sheet links to `/observe/requests/:requestId/audit` with a validated `/observe/requests` return target that preserves the original list state.
- Routing Health Events and Requests can cross-link with absolute ±15-minute windows and verified objects; neither direction claims a unique trigger relationship, and retention cropping is surfaced.
- Pricing Templates / Model detail CTAs come only from the pricing action matrix; usage-only unpriced reasons never show a Pricing CTA.

## 10. Required Contracts

The Requests page must remain compatible with the following backend-facing and shared frontend contracts:

- `RequestLogListItem` (v2 slim row: `request_log_id` string, `row_kind`, scoped statuses, attempt facts, failure preview) for the browse table
- `ChainResponse`/`ChainIngressItem`/`FinalizedSummary` for the default chain view
- `RequestLogDetail` for the detail sheet only
- `api.stats.requests()`/`api.stats.chains()` for browsing and `/api/stats/requests/{request_id}` for exact detail
- `api.stats.exportCsv()` for the server-side full filtered export
- audit list/detail API client methods for explicit content reads; raw-body download helpers remain separate API-client capabilities
- dashboard flows that consume request-derived backend responses
- caller-client and attempt/final-target observability fields such as `client_rule_id`, `filter_options.clients`, `filter_options.attempt_target_models`, and retained `resolved_target_model_id` execution attribution

## 11. Acceptance Criteria

1. Visiting `/observe/requests` loads the ingress-chain view plus filter-reference data for Default profile id `1`.
2. Server-backed filter changes update URL state with `replace: true` semantics and reset pagination to the first page.
3. Retained browse filters update URL state with `replace: true`, clear every incompatible pagination anchor, and drive refreshed list/CSV cohorts directly without browser-side search or triage refinement. `client_rule_id` filters caller user agents only, `ingress_model_id` selects the requested model, `attempt_target_model_id` selects retained attempt targets, and signed `final_target_model_id` selects the finalized owner. `pricing_status` is the only pricing-state filter; `priced` is never generated or accepted.
4. Visiting `/observe/requests?request_id=<id>` opens exact-request investigation mode with the focus banner and detail-sheet support.
5. Visiting `/observe/requests?ingress_request_id=<id>` filters the request list to all per-attempt rows for that incoming runtime request without breaking numeric `request_id` deep links.
6. Opening the dedicated full audit page loads request detail first, then queries `/api/audit/logs` with `request_log_id`, ±12-hour bounds, `limit=20`, and optional `cursor`; disabled audit makes no audit API call.
7. The table remains usable at large result counts through virtualization, sticky headers, adaptive height, and explicit pagination controls; the body is the single scroll container.
8. The list view stays on the slim v2 row payload, the chain view on the chain envelope, and exact-request investigation uses the dedicated v2 detail payload without re-expanding the table schema.
9. Dashboard overview and recent activity can emit deep links into `/observe/requests` without inventing route-local state outside the documented query contract.
10. The overview sheet distinguishes requested model, actual model/service, attempt order, and results while keeping internal correlation identities out of its product presentation.
11. The request-log table and detail sheet render requested model vs final target model separately, falling back to the requested model when `resolved_target_model_id` matches `model_id`.
12. CSV export contains the full filtered result set as produced by the server (`/api/stats/requests/export`), not the currently loaded page.
13. Failed rows open a plain-language explanation grounded in status and stream evidence, with uncertainty and model/service recovery actions; untrusted diagnostic text is never rendered.
14. Route-shell, filter, empty-state, and detail-sheet labels follow the active frontend locale while timestamp rendering stays aligned to the selected timezone and locale-aware formatting helpers.
15. TTFT is the primary latency with neutral coloring; total duration stays neutral and is never painted as an error state.
16. The detail sheet renders each piece of primary information once and preserves pricing limitations in natural language; internal diagnostics do not reappear in disclosures or tooltips.
17. A selected audit record shows a compact content summary, readable conversation, and capture limitations; header, protocol, and raw-download controls are absent.
18. Request-log IDs stay decimal strings end-to-end (URL, JSON, TS); the frontend never converts BIGINT row IDs to JS numbers.
19. The filter bar shows only search (request ID), time range, the triage chips, and a More Filters toggle at 1200px; the remaining filters collapse under More Filters.
20. Operators can save, apply, and delete named views of the canonical query state (versioned localStorage); column visibility is adjustable through the table's Columns menu with a reset-to-defaults action.
21. The detail sheet renders the server-owned retained ingress chain for the selected row: attempt order, triggers, winner/current markers, retained counts, and chain completeness.
22. Streaming bodies display recognized readable messages and tool-activity notices. Unsupported, incomplete, and truncated content remains explicit; comparison and copy exclude raw protocol and tool arguments.
