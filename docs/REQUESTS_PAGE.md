# Requests Page Specification

**Scope:** Frontend route `/request-logs` and its request-investigation helper cluster

## 1. Overview

The Requests page is Prism's dedicated request-browser and investigation surface for proxied traffic. It is a mounted route at `/request-logs` that provides a profile-scoped view for browsing request history through a slim retained filter set and inspecting request-level details including linked audit payloads.

The backend request-log and audit APIs remain the source of truth. The frontend route is responsible for presenting that data in an operator-friendly investigation workflow without changing runtime proxy semantics. The current browse filter set keeps `ingress_request_id`, `model_id`, `endpoint_id`, `status_family`, and `time_range`, while exact single-request investigation uses `request_id`.

The request-log route now uses split HTTP contracts: a slim list payload for browsing and a dedicated grouped detail payload for the sheet. The table renders vendor as display-only metadata next to `api_family`, but vendor never becomes a server-backed request-log filter.

## 2. Goals

- Provide a dedicated request-history route at `/request-logs`.
- Support deep investigation of a single request through URL-addressable state.
- Keep the retained browse filters server-backed and URL-addressable.
- Expose linked audit payloads only when needed.
- Support drill-down entry points from dashboard and model-detail views.
- Show requested model identity separately from the final target model chosen by unified access-target resolution.

## 3. Non-Goals

- Replace dashboard or statistics summaries.
- Change backend request-log, audit-log, or costing contracts.
- Change active-profile runtime routing behavior for `/v1/*` and `/v1beta/*`.

## 4. Route Responsibilities

The page route should act as a thin orchestration shell with four primary responsibilities:

1. Render page chrome through `PageHeader`.
2. Own URL-backed state through `useRequestLogPageState()`.
3. Load request data and filter options through `useRequestLogsPageData()`.
4. Compose the investigation UI through `RequestFocusBanner`, `FiltersBar`, `RequestLogsTable`, and `RequestLogDetailSheet`.

The route should also integrate shared application services:

- `useProfileContext()` for selected-profile and profile-revision refresh behavior.
- `useTimezone()` plus the shared frontend locale boundary for locale-aware timestamp formatting.
- `useLocale()` for route-shell, filter, empty-state, and detail-drawer copy.
- `request-logs/connectionNavigation.ts` for connection-centric drill-down flows.
- `TooltipProvider` for table and filter affordances.

## 5. URL State Contract

`useRequestLogPageState()` should own the complete search-parameter contract and update query state with `replace: true` semantics so frequent filter changes do not spam browser history.

Supported query parameters:

- Browse filters: `ingress_request_id`, `model_id`, `endpoint_id`, `status_family`, `time_range`
- Pagination: `limit`, `offset`
- Exact-investigation flow: `request_id`, `detail_tab`

Behavioral requirements:

- Default values should be omitted from the URL.
- Any filter mutation that changes the result set must reset `offset` to `0`.
- `request_id` must switch the page into exact-request investigation mode.
- `ingress_request_id` must support grouped investigation of all per-attempt rows created by one incoming runtime request.
- `detail_tab` must preserve whether the detail drawer opens on `overview` or `audit`.

## 6. Data And API Requirements

### 6.1 Request Log Fetch

Primary APIs:

- `api.stats.requests()` -> `/api/stats/requests`
- dedicated detail fetch -> `/api/stats/requests/{request_id}`

Required behavior:

- Debounce fetches by 300 ms.
- Send server-supported browse filters for model, ingress request grouping, endpoint, status family, and time window.
- Send `ingress_request_id` as an exact server-backed grouping filter when present.
- Keep list browsing on the slim list schema and fetch exact-request sheet data from the dedicated detail endpoint.
- Track fetch ordering so stale responses cannot overwrite newer state.

### 6.2 Filter Option Bootstrap

The page bootstraps model reference data separately through `api.models.list()` and derives endpoint filter options from the paginated `/api/stats/requests` response (`filter_options.endpoints`).

Partial failure in model bootstrap must not block request browsing, and endpoint filter options should become ready when the current list response arrives.

### 6.3 Linked Audit Resolution

Audit detail should load lazily only when the request detail drawer opens the `audit` tab.

Audit APIs:

- `api.audit.list()` -> `/api/audit/logs`
- `api.audit.get()` -> `/api/audit/logs/{id}`

Required behavior:

- Avoid audit fetches during normal table browsing.
- Skip linked-audit fetches entirely when `audit_enabled_at_request` is `false`.
- Treat `audit_capture_bodies_at_request` as the request-time provenance flag for metadata-only vs full capture instead of inferring from body presence.
- Retry linked-audit lookup up to five times with a one-second delay when the linked audit list is still empty or audit fetches fail transiently.
- Keep orphaned audit rows visible when linked request logs were deleted, while still treating `request_log_id` as nullable provenance rather than a browse filter.
- Resolve audit detail rows with `Promise.allSettled()` so one failed detail fetch does not hide other captured rows.
- Keep audit loading isolated from the main request-list fetch lifecycle.

## 7. UX Workflow Requirements

### 7.1 Filter And Triage Workflow

The page should use only the retained browse filters in URL state and send them directly to the backend list route. The current contract keeps `request_id`, `ingress_request_id`, `model_id`, `endpoint_id`, `status_family`, and `time_range`, and removes the old client-side search, token, latency, stream, outcome, and triage refinement layer.

### 7.2 Exact-Request Investigation Workflow

When the route opens with `request_id`, it should stop behaving like a normal paginated browser.

Required behavior:

- Fetch only the targeted request.
- Show `RequestFocusBanner` with an exit action.
- Render a dedicated empty state with a return action when the request is missing.
- Preserve `detail_tab` so links can open directly to `overview` or `audit`.

Grouped request-tracking workflow:

- `request_id` remains a one-row deep link for exact attempt investigation.
- `ingress_request_id` groups multiple attempt rows from one incoming runtime request without changing `request_id` semantics.
- The overview tab should surface `ingress_request_id`, `attempt_number`, and `provider_correlation_id` so operators can distinguish Prism grouping from upstream correlation.

### 7.3 Table Workflow

`RequestLogsTable` should support dense browsing at high row counts.

Required behavior:

- Virtualized rows with `45px` row height.
- `10` rows of overscan.
- One fixed component-owned scroll viewport height for the table body.
- Sticky headers in all views.
- Page-size controls limited to `100`, `300`, and `500`, with `100` as the route default.
- Footer controls for page size plus previous and next pagination.
- Show one combined `Vendor / API` column that renders `vendor_name` (or `—`) on the first line and the formatted `api_family` with icon on the second line.

### 7.4 Detail Drawer Workflow

`RequestLogDetailSheet` should expose two tabs:

- `overview`: request metadata, requested model vs final target model identity, token and cost breakdowns, routing context, and connection drill-down
- `audit`: lazily resolved request and response payload capture

The drawer should also support direct navigation to the owning connection record.

Dense overview requirements:

- Keep the same logical groups: `Request details`, `Routing context`, `Token usage`, and `Cost breakdown`.
- Render a compact summary strip for latency, token, cost, and timestamp context above the grouped sections.
- Keep audit loading lazy and scoped to the `audit` tab only.

## 8. Module Boundaries

The `frontend/src/pages/request-logs/` helper cluster should remain page-specific and own the following responsibilities:

- query-parameter definitions and parsers
- retained browse-filter state and exact-request mode orchestration
- sticky filter-bar UI groups
- column definitions and row renderers, including requested model vs final target model identity rendering and the display-only vendor column
- detail-sheet tabs and shared panels over the dedicated request-detail payload
- audit loading hook
- dedicated tests for page state, filter options, page data, and audit detail loading

## 9. Cross-Route Integrations

Other frontend surfaces should be able to deep-link into `/request-logs` with scoped context.

### 9.1 Dashboard

Dashboard should support request-log drill-down entry points for:

- quick action button: `Review Requests`
- routing diagram endpoint drill-downs
- routing diagram route drill-downs

### 9.2 Model Detail

Model Detail should support request-log drill-down from the 24-hour connection metrics card.

## 10. Required Contracts

The Requests page must remain compatible with the following backend-facing and shared frontend contracts:

- `RequestLogListItem` for the browse table and related list consumers
- `RequestLogDetail` for the detail sheet only
- `api.stats.requests()` for browsing slices and `/api/stats/requests/{request_id}` for exact detail
- audit API client methods
- dashboard flows that consume request-derived backend responses
- final-target observability fields such as `resolved_target_model_id`

## 11. Acceptance Criteria

1. Visiting `/request-logs` loads a paginated request list plus filter-reference data for the selected profile.
2. Server-backed filter changes update URL state with `replace: true` semantics and reset pagination to the first page.
3. The retained browse filters update URL state with `replace: true` semantics and drive refreshed list requests directly, without a client-side search or triage refinement layer.
4. Visiting `/request-logs?request_id=<id>` opens exact-request investigation mode with the focus banner and detail-drawer support.
5. Visiting `/request-logs?ingress_request_id=<id>` filters the request list to all per-attempt rows for that incoming runtime request without breaking numeric `request_id` deep links.
6. Opening the `audit` tab triggers lazy audit resolution, skips fetches when audit capture was disabled for that request, and supports retry behavior for temporarily missing or transiently failing linked-audit lookups.
7. The table remains usable at large result counts through virtualization, sticky headers, and explicit pagination controls.
8. The list view stays on the slim list payload, while exact-request investigation uses the dedicated detail payload without re-expanding the table schema.
9. Dashboard and Model Detail can emit deep links into `/request-logs` without inventing route-local state outside the documented query contract.
10. The overview tab renders `ingress_request_id`, `attempt_number`, and `provider_correlation_id` when present so operators can distinguish incoming request grouping from per-attempt row identity.
11. The request-log table and detail drawer render requested model vs final target model separately, falling back to the requested model when `resolved_target_model_id` matches `model_id`.
12. Route-shell, filter, empty-state, and detail-drawer labels follow the active frontend locale while timestamp rendering stays aligned to the selected timezone and locale-aware formatting helpers.
