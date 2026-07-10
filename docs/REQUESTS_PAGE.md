# Requests Page Specification

**Scope:** Frontend route `/observe/requests` and its request-investigation helper cluster

## 1. Overview

The Requests page is Prism's dedicated request-browser and investigation surface for proxied traffic. It is mounted at `/observe/requests`. It provides a Default-profile-pinned view for browsing request history through a slim retained filter set and inspecting request-level overview details, with full audit payloads on a dedicated audit page.

The backend request-log and audit APIs remain the source of truth. The frontend route is responsible for presenting that data in an operator-friendly investigation workflow without changing runtime proxy semantics. The canonical URL filter set keeps `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `priced`, `unpriced_reason`, and `time_range`, while exact single-request investigation uses `request_id`.

The request-log route now uses split HTTP contracts: a slim list payload for browsing and a dedicated grouped detail payload for the sheet. Caller client filtering is server-backed through `client_rule_id` and matches `caller_user_agent` only. Upstream user-agent display stays informational.

## 2. Goals

- Provide a dedicated request-history route at `/observe/requests`.
- Support deep investigation of a single request through URL-addressable state.
- Keep the retained browse filters server-backed and URL-addressable.
- Expose linked audit payloads only when needed.
- Support implemented drill-down entry points from dashboard overview and dashboard recent activity.
- Show requested model identity separately from the final target model chosen by unified access-target resolution.
- Show requested model identity separately from final target, selected Terminal Target, and endpoint.

## 3. Non-Goals

- Replace dashboard or statistics summaries.
- Change backend request-log, audit-log, or costing contracts.
- Change frozen Default-profile runtime routing behavior for `/v1/*` and `/v1beta/*`.

## 4. Route Responsibilities

The page route should act as a thin orchestration shell with four primary responsibilities:

1. Render page chrome through `PageHeader`.
2. Own URL-backed state through `useRequestLogPageState()`.
3. Load request data and filter options through `useRequestLogsPageData()`.
4. Compose the investigation UI through `RequestFocusBanner`, `FiltersBar`, `RequestLogsTable`, and `RequestLogDetailSheet`.

The route should also integrate shared application services:

- Default profile id `1` is frozen for management reads; `X-Profile-Id` may still be sent by shared API code but is ignored.
- `useTimezone()` plus the shared frontend locale boundary for locale-aware timestamp formatting.
- `useLocale()` for route-shell, filter, empty-state, and detail-sheet copy.
- `TooltipProvider` for table and filter affordances.

## 5. URL State Contract

`useRequestLogPageState()` should own the complete search-parameter contract and update query state with `replace: true` semantics so frequent filter changes do not spam browser history.

Supported canonical query parameters:

- Browse filters: `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `priced`, `unpriced_reason`, `time_range`
- Pagination: `limit`, `cursor`
- Exact-investigation flow: `request_id`
- Row selection without exact mode: `selected_request_id`

Accepted legacy aliases are parsed and canonicalized away: `model_id`, `endpoint_id`, `status_family`, and `offset`. `status=client_error` maps to backend `status_family=4xx`; `status=error` maps to backend `status_family=5xx`.

Behavioral requirements:

- Default values should be omitted from the URL.
- Any filter mutation that changes the result set must reset canonical `cursor` to `0` and therefore send backend `offset=0`.
- `request_id` must switch the page into exact-request investigation mode.
- `ingress_request_id` must support grouped investigation of all per-attempt rows created by one incoming runtime request.
- Stale `detail_tab` parameters must be ignored and canonicalized away.

## 6. Data And API Requirements

### 6.1 Request Log Fetch

Primary APIs:

- `api.stats.requests()` -> `/api/stats/requests`
- dedicated detail fetch -> `/api/stats/requests/{request_id}`

Required behavior:

- Debounce fetches by 300 ms.
- Send server-supported browse filters for model, ingress request grouping, endpoint, caller client rule, final target model, status family, exact status code, error text, priced state, unpriced reason, and time window.
- Translate canonical URL state to backend request parameters: `model` -> `model_id`, `endpoint` -> `endpoint_id`, `status` -> `status_family`, and `cursor` -> `offset`.
- Send `unpriced_reason` only when `priced=false`; other priced states omit it from backend params.
- Send `ingress_request_id` as an exact server-backed grouping filter when present.
- Keep list browsing on the slim list schema and fetch exact-request sheet data from the dedicated detail endpoint.
- Track fetch ordering so stale responses cannot overwrite newer state.

### 6.2 Filter Option Bootstrap

The page derives model, endpoint, caller client, and final-target filter options from the paginated `/api/stats/requests` response: `filter_options.models`, `filter_options.endpoints`, `filter_options.clients`, and `filter_options.resolved_target_models`.

Response-owned filter options should become ready when the current list response arrives. `filter_options.clients` entries use `{ client_rule_id, client_label }` and represent enabled User-Agent Client Rules. Selecting one sends `client_rule_id` back to the backend, where matching is caller-only against `caller_user_agent`.

### 6.3 Dedicated Audit Resolution

Detailed audit payloads load only on `/observe/requests/:requestId/audit`. The audit route is request-focused: it first loads `/api/stats/requests/{request_id}`. If that request detail is missing or invalid, the page stops and does not issue audit-list or audit-detail calls. The current UI therefore has no standalone orphan-audit browser even though backend audit rows can retain `request_log_missing` metadata after request-log deletion.

Audit APIs:

- request detail: `api.stats.requestDetail()` -> `/api/stats/requests/{request_id}`
- request-scoped audit list: `api.audit.listForRequestLog()` -> `/api/audit/logs?request_log_id=...`
- selected audit detail: `api.audit.get()` -> `/api/audit/logs/{id}`

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

The page should use only the retained browse filters in URL state and send them directly to the backend list route. The current canonical URL contract keeps `request_id`, `selected_request_id`, `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `priced`, `unpriced_reason`, and `time_range`, and removes the old client-side search, token, latency, stream, outcome, and triage refinement layer. The Client dropdown must not expose regex, `client_scope`, or upstream matching language.

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
- The overview sheet should surface `ingress_request_id`, `attempt_number`, `provider_correlation_id`, requested model, final target model, selected Terminal Target, and endpoint so operators can distinguish Prism grouping from upstream correlation and final response ownership.

### 7.3 Table Workflow

`RequestLogsTable` should support dense browsing at high row counts.

Required behavior:

- Virtualized rows with `45px` row height.
- `10` rows of overscan.
- One fixed component-owned scroll viewport height for the table body.
- Sticky headers in all views.
- Page-size controls limited to `100`, `300`, and `500`, with `100` as the route default.
- Footer controls for page size plus previous and next pagination.
- Show `api_family`, requested model, final target model, endpoint, and caller/upstream client display fields without adding browser-side post-filtering.
- Export CSV from the currently loaded `items` only. The export never fetches all filtered rows and is therefore capped by the selected page size (`100`, `300`, or `500`).

### 7.4 Detail Sheet Workflow

`RequestLogDetailSheet` exposes an overview-only inspection sheet with request metadata, requested model vs final target model identity, token and cost breakdowns, and routing context.

Every successfully loaded request detail provides a link to the dedicated full audit page. The sheet does not conditionally hide that entry and does not fetch audit payloads. The target page then renders one of three request-time states: disabled, metadata-only, or full capture.

Dense overview requirements:

- Keep the same logical groups: `Request details`, `Routing context`, `Token usage`, and `Cost breakdown`.
- Render a compact summary strip for latency, token, cost, and timestamp context above the grouped sections.
- Cost breakdown includes priced/billable state, unpriced reason, report currency, original/source currency, FX rate and source, pricing unit, pricing configuration version, and all five pricing snapshot values.
- Operation name, upstream operation name, translation mode, and upstream path are returned by the backend detail API, but the current frontend detail type/sheet displays the request path and does not render those operation/translation fields.
- Keep audit payload loading out of the sheet and scoped to the dedicated full audit page.

### 7.5 Payload Views And Copy

The dedicated audit page renders request and response headers plus request and response bodies. For each non-empty payload block:

- `Rendered` shows the structured document view when Prism recognizes the payload. Header rendering additionally masks `authorization`, `proxy-authorization`, `cookie`, `set-cookie`, and header names containing `api-key`, `token`, `secret`, or `credential` (case-insensitive).
- `Raw JSON` pretty-prints stored body payloads. For header blocks, it shows a browser-normalized header representation with the same additional masking rather than the unmodified stored text.
- Copying in raw mode copies the transformed text currently shown. Copying in rendered mode copies the underlying stored text, not the browser-masked header display; the three request auth-header values redacted by the backend at write time remain redacted because the persisted values are `[REDACTED]`.
- Empty bodies disable the copy control. Clipboard API failure or absence falls back to a temporary local textarea mounted under the page or sheet's `[data-clipboard-fallback-root]`.

## 8. Module Boundaries

The `frontend/src/pages/request-logs/` helper cluster should remain page-specific and own the following responsibilities:

- query-parameter definitions and parsers
- retained browse-filter state and exact-request mode orchestration
- sticky filter-bar UI groups
- column definitions and row renderers, including requested model vs final target model identity rendering and caller/upstream client display
- overview-only detail sheet and shared panels over the dedicated request-detail payload
- dedicated full audit page loading hook
- URL/filter and audit-state seam contracts plus the dedicated request-log/audit Playwright journey

## 9. Cross-Route Integrations

Other frontend surfaces should be able to deep-link into `/observe/requests` with scoped context.

### 9.1 Dashboard

Dashboard should support request-log drill-down entry points for:

- quick action button: `Review Requests`
- recent activity row drill-downs by `request_id`

## 10. Required Contracts

The Requests page must remain compatible with the following backend-facing and shared frontend contracts:

- `RequestLogListItem` for the browse table and related list consumers
- `RequestLogDetail` for the detail sheet only
- `api.stats.requests()` for browsing slices and `/api/stats/requests/{request_id}` for exact detail
- audit API client methods
- dashboard flows that consume request-derived backend responses
- caller-client and final-target observability fields such as `client_rule_id`, `filter_options.clients`, and `resolved_target_model_id`

## 11. Acceptance Criteria

1. Visiting `/observe/requests` loads a paginated request list plus filter-reference data for Default profile id `1`.
2. Server-backed filter changes update URL state with `replace: true` semantics and reset pagination to the first page.
3. The retained browse filters update URL state with `replace: true` semantics and drive refreshed list requests directly, without a client-side search or triage refinement layer. `client_rule_id` filters caller user agents only, and `resolved_target_model_id` filters final target models.
4. Visiting `/observe/requests?request_id=<id>` opens exact-request investigation mode with the focus banner and detail-sheet support.
5. Visiting `/observe/requests?ingress_request_id=<id>` filters the request list to all per-attempt rows for that incoming runtime request without breaking numeric `request_id` deep links.
6. Opening the dedicated full audit page loads request detail first, then queries `/api/audit/logs` with `request_log_id`, ±12-hour bounds, `limit=20`, and optional `cursor`; disabled audit makes no audit API call.
7. The table remains usable at large result counts through virtualization, sticky headers, and explicit pagination controls.
8. The list view stays on the slim list payload, while exact-request investigation uses the dedicated detail payload without re-expanding the table schema.
9. Dashboard overview and recent activity can emit deep links into `/observe/requests` without inventing route-local state outside the documented query contract.
10. The overview sheet renders `ingress_request_id`, `attempt_number`, and `provider_correlation_id` when present so operators can distinguish incoming request grouping from per-attempt row identity.
11. The request-log table and detail sheet render requested model vs final target model separately, falling back to the requested model when `resolved_target_model_id` matches `model_id`.
12. CSV export contains only the currently loaded page (up to the selected 500-row page size), not the full filtered result set.
13. Route-shell, filter, empty-state, and detail-sheet labels follow the active frontend locale while timestamp rendering stays aligned to the selected timezone and locale-aware formatting helpers.
