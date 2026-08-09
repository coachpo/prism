# Product Specification: Prism

## 1. Product Overview

Prism is a lightweight, self-hosted application that acts as a unified proxy for multiple LLM API families. It lets a single user configure, route, and load-balance requests through a web-based management UI.

## 2. Problem Statement

Developers and power users working with multiple LLM API families face:
- Managing multiple API keys and base URLs across different tools
- No unified endpoint for switching between API families
- No automatic failover when an API family is down or rate-limited
- Manual configuration changes when rotating keys or endpoints

## 3. Target User

Single operator (developer/power user) running the application locally or on a local network. Prism supports optional operator authentication for management APIs and proxy API keys for runtime traffic. Management configuration is pinned to frozen Default profile id `1` while runtime traffic always resolves to frozen Default profile id `1`; `X-Profile-Id` and profile fields are compatibility/storage attribution only. This is not auth multi-tenancy.

## 4. Core Features

### 4.1 Multi-Family Proxy
- Operation-registered proxy support for explicit OpenAI Chat Completions, Responses, Anthropic Messages/count-token, and Gemini generate/stream/count-token operations
- Supports both streaming (SSE) and non-streaming responses
- Preserves native request/response formats per API family
- Runtime compatibility is fixed by `api_family`
- `GET /v1/models` is local: requests with `client_version` use the current Codex catalog, seeded from the embedded copy and refreshed asynchronously from OpenAI at startup and every 24 hours, with a deterministic weak ETag/`304` path; requests without it use the local OpenAI `object`/`data` list

### 4.2 Model Configuration
- Map each model to a fixed runtime `api_family`
- Models expose one ordered `access_targets` list whose public entries point to same-family models
- Terminal Targets carry endpoint, costing, admission-limit, and auth metadata as model-private endpoint bindings owned by one model
- Select which access targets are enabled for each model; enabled models require at least one enabled target
- CRUD operations for all configurations are available via REST API
- Composite create (`initial_terminal_target`) creates a model, an optional inline endpoint, the first Terminal Target and its owner edge in one atomic transaction: the normal path produces a working enabled model in a single submit, any failure rolls back completely, and "configure later" creates a disabled model without a target
- New Terminal Target capability defaults follow the owner model accepted format; `None` coverage is not selectable in first-party UI, `Partial` coverage saves with a visible warning, and the create dialog shows capability coverage previews next to the picker

### 4.3 Unified Model Access Routing
- Ordered same-family model targets are evaluated as an aggregate; direct Terminal Targets are the fallback when no model-target candidate is eligible
- The UI presents the two stages explicitly: "模型目标（先尝试）" and "终端目标（无模型候选时回落）", each with stage-local numbering; `single` truncation is shown per stage and per truncated row, and never mislabeled across stages
- Routing diagnostics (per-operation capability coverage, static eligibility, resolved stage) come from a backend static analyzer; the frontend never re-derives coverage or eligibility, and operation coverage warnings (`完整覆盖 / 部分覆盖 / 不兼容`) are authoritative
- Model-target entries must stay within the same `api_family`, cannot target themselves, and cannot introduce cycles
- Internal connection-target entries are terminal ownership and routing edges for Terminal Targets; model-target entries can compose chains while preserving deterministic order
- Each model owns its reusable load-balance strategy, so nested model targets evaluate strategy and Ban Policy at their own graph level
- Model IDs are unique within a profile; the same model ID can exist in different profiles without collision
- Gateway resolves the access graph before Terminal Target planning: incoming request for a public model -> final target model and Terminal Target -> upstream request
- For Gemini API paths (e.g., `/v1beta/models/{model}:generateContent`), the proxy rewrites the model ID segment in the URL path to the resolved final target model ID when needed
- Gemini streaming is path-native: `/v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`
- Once one final target path is selected for an attempt, retries stay inside that target path's Terminal Target plan for that attempt.

### 4.4 Load Balancing & Failover
- For models with multiple reachable Terminal Targets:
  - **Automatic failover** when an attempt returns a failover-triggering status (`403`, `422`, `429`, `500`, `502`, `503`, `504`, `529` by default) or raises connection/timeout errors
  - Models attach one reusable explicit Ban Policy strategy using `single`, `fill-first`, or `round-robin` routing
  - Upstream request timing uses shared backend timeout settings, while Ban Policy owns retry windows, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, `temporary` or `until_reset` bans, and failover status codes
  - Ban Policy thresholds are inclusive: retry-cycle exhaustion uses `cycle_retry_attempts >= cycle_retry_attempt_limit`, and bans use `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`
- Failover-worthy HTTP responses are governed by the attached strategy's configured failure status codes and retry-window settings
  - Non-failover client errors outside the configured failure status set (for example `400` or `404` with default policies) do not force-clear existing Ban Policy state
  - Each Terminal Target can optionally define `qps_limit`, `max_in_flight_non_stream`, and `max_in_flight_stream`; `null` means unlimited
  - Runtime admission, Ban Policy, lease, and round-robin state is process-local and is intentionally ephemeral across process restarts; retained SQL hot-state tables are compatibility schema, not the production hot path
- Proxy request forwarding may apply compatibility normalizations while preserving API-family-native response formats
- Model-owned overflow replay and exact facade routing have been removed. After headers, flush, SSE, or any client-visible bytes commit downstream output, Prism does not switch upstreams for that stream.
- Request-log materialization distinguishes actual upstream attempts from synthetic failures. Telemetry-eligible target-resolution or native-compatibility errors carrying `PlanningFailure`, plus `admission_exhausted`, can produce synthetic rows without endpoint/connection. Malformed bodies, unknown models, API-family mismatches, registry-rejected requests, and the terminal all-transport `502` path do not retain request history.
- Request-log detail preserves requested public model identity, final target identity, selected Terminal Target, endpoint, operation names, and translation mode through flat fields.
- Retry scheduling, retry exhaustion, bans, unbans, recovery, and admission-rejection transitions are persisted as `loadbalance_events` for audit and observability.

### 4.5 Default-Profile Endpoints & Terminal Targets
- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Models** carry fixed `api_family` metadata.
- **Terminal Targets** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile.
- Endpoints can be reused across multiple models within the same profile.
- Deleting an endpoint is blocked with a typed `409 endpoint_in_use` that lists the same direct Terminal Target references shown in the Endpoint page disclosure (`模型 → Terminal Target → capability → 价格模板 → 启停/激活状态`); recursive reachable models are labeled separately and never block deletion
- Endpoint cards support "附加到模型": pick a destination model and create a new private Terminal Target with the endpoint preselected and locked
- A Terminal Target can be copied to multiple same-family destination models in one transactional batch; copies are independent private connections, default to not participating in routing, and never carry runtime state or secrets

### 4.6 Terminal Target Request Health
- Manual Terminal Target test actions are removed from the management API and UI.
- Backend request-derived stats can expose Terminal Target success-rate and routing-health read models for real runtime traffic. These are API/read-model capabilities, not a current per-target health-badge workflow.
- The Models page renders plain telemetry text for each model: 24-hour success rate, P95 latency, and 24-hour request count, plus a 30-day spend value. Missing success data is shown as `- Success`; there are no colored success-rate thresholds or health badges.
- The current model-detail UI does not render Terminal Target success-rate indicators, and the dashboard does not render the backend `routing_health_map` response field.
- Terminal Target cards render the process-local Ban Policy observation: `本进程尚未观测`, `当前无冷却限制`, retry-wait/banned with until-time and a narrow "重置冷却" action, last success time, `最近成功响应头延迟` and in-flight counts. `available` never implies upstream health; stale snapshots are labeled `状态可能已过期`

### 4.7 Web UI (Management Dashboard)
- View all configured models and their reachable Terminal Targets
- Add/edit/delete model configurations with ordered access targets
- Add/edit/delete profile-scoped endpoints
- Add/edit/delete Terminal Targets from model detail
- Toggle enabled/disabled access targets per model
- Select an explicit load-balance strategy with Ban Policy settings per model
- Dedicated model-detail route (`/models/:id`) for two-stage access-target and Terminal Target configuration, operation coverage summary and runtime state; dead `?tab=` state is normalized away, `action=create-terminal-target` (with optional `endpoint_id`) and `focus_connection_id` are one-shot URL parameters consumed exactly once; current loadbalance state and loadbalance event history live under Ban Policies
- Model list and detail share the `N 启用 / M 总计` target count vocabulary; the API column shows family plus OpenAI accepted format; Partial/None/uncovered and `single` truncation warnings appear on list rows
- Dedicated request-log browsing and investigation at `/observe/requests`, separate from dashboard analytics
- Dedicated routes for pricing templates and proxy API key lifecycle management
- Dashboard analytics lives under `/observe?tab=analytics` and replaces the old standalone statistics route
- The protected shell renders sidebar navigation and breadcrumbs from local route metadata.
- Settings visibly uses **全局** and **实例** tabs. The internal query values remain `profile` and `global`: `全局` contains billing/currency, timezone, audit/privacy, and config rules; `实例` contains authentication, global retention policies, and retention/deletion.

### 4.8 Configuration Persistence
- Runtime and management configuration is stored in PostgreSQL with Go-backend-managed schema migrations applied at startup
- Startup/bootstrap process settings are owned by the plaintext `config.json` bootstrap file; external edits require a Prism restart after R2
- The default profile exists from the first startup and remains editable after initialization
- Database setup is managed by the Go backend runtime and applies the checked-in fresh-install baseline on startup
### 4.9 Request Statistics & Analytics
- Local `GET /v1/models` produces no runtime telemetry. Provider-forwarded operations create retained history only when activity reaches a telemetry handoff: successful `2xx` responses use the durable response-path handoff, captured non-`2xx` responses use scheduled activity handoff, and the narrow `PlanningFailure`/`admission_exhausted` classes use synthetic failure handoff. Registry rejections and the earlier planning errors listed above do not create request history.
- Each retained request-log detail can capture: profile ID attribution, requested `model_id`, final `resolved_target_model_id` when an access-target path is selected, `api_family`, Terminal Target used through compatibility connection attribution (ID, endpoint base URL, description), Prism `ingress_request_id`, per-request `attempt_number`, best-effort `provider_correlation_id`, caller and upstream client display, HTTP status, response time (ms), token usage when available from upstream response, whether the request was streamed, selected Terminal Target, operation names, translation mode, and timestamp.
- A normal upstream attempt is represented by one request-log row; telemetry-eligible `PlanningFailure` or `admission_exhausted` activity may add a synthetic row without endpoint/connection. `ingress_request_id` groups retained attempt rows from one incoming runtime request. The current detail sheet shows the request path and routing fields, while operation names, translation mode, and upstream path are persisted and returned by the detail API but are not rendered in that sheet.

#### 4.9.1 Token Usage Extraction
Token usage is extracted from upstream responses using api-family-aware parsing:
- **OpenAI Chat Completions (non-streaming)**: Extracts from `usage.prompt_tokens`, `usage.completion_tokens`, and detail objects
- **OpenAI Responses and Responses compact (non-streaming)**: Extracts from `usage.input_tokens`, `usage.output_tokens`, `usage.total_tokens`, and detail objects
- **OpenAI Responses input_tokens (non-streaming)**: Extracts `input_tokens` and `total_tokens` from top-level token-count payloads
- **Anthropic Messages (non-streaming)**: Extracts from `usage` object
- **Anthropic count_tokens (non-streaming)**: Extracts `input_tokens` from top-level
- **OpenAI (streaming)**: Accumulated from terminal SSE usage events for native Chat Completions or Responses streams
- **Anthropic (streaming)**: Accumulated from SSE events (`message_start` and `message_delta`)
- **Fallback**: If token data cannot be extracted, all token fields are logged as `null`
- **Null vs zero token semantics**:
  - No upstream usage block: token fields remain `null`
  - Usage block present but special fields absent: special fields logged as `0`

#### 4.9.2 Token Costing
The gateway computes the cost of each request based on the extracted token usage and the connection's assigned pricing template.
- **Pricing Templates**: Pricing is profile-scoped and reusable. Connections reference templates via `pricing_template_id` instead of storing inline price fields.
- **Pricing behavior**: Pricing templates use five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Management writes normalize missing/null/blank pricing inputs to `"0"` before validation.
- **Semantic Note**: Explicit `"0"` means configured free pricing. `MISSING_PRICE_DATA` is reserved for absent, unusable, or invalid pricing snapshots, or missing FX data. Token costing uses canonical disjoint components: base input, cache-read input, cache-creation input, base output, and reasoning output; aggregate `cached_tokens` is derived-only for presentation.
- **Historical costing provenance**: Request-log details retain and display report currency, original/source currency, FX rate and source, pricing unit, pricing configuration version, and all five pricing snapshot components. Lists and CSV use each row's stored report-currency symbol rather than recomputing historical requests from the current settings.

- Statistics dashboard in the Web UI with:
  - Overview KPI cards for Active Models, Requests 24h, Spending 30d, and Average RPM
  - Overview supporting tiles for average latency, P95 latency, error rate, and streaming share, plus API-family mix, recent activity, quick actions, and top-spending models
  - Dashboard incidents for active bans and recent loadbalance events; the backend `routing_health_map` is returned in the snapshot but is not rendered by the current UI
  - Aggregate endpoint, model, and proxy-key usage views sourced from the unified usage snapshot with endpoint labels read from stored `endpoint_label_snapshot` values
  - Separate recent activity feed that links into request-log investigation without being embedded in the dashboard snapshot
  - Analytics controls: time presets (`1h`, `6h`, `24h`, `7d`, `30d`, `all`) plus model-line selection for usage trend comparison
  - Analytics KPI cards for requests/success rate, total tokens with component breakdown, RPM, TPM, and total spend, followed by usage trends and aggregate endpoint/model/proxy-key tables
  - Summary statistics grouped by model and API family
- Dedicated request investigation UI at `/observe/requests` with server-backed coarse filters, caller-only `client_rule_id` filtering, final-target `resolved_target_model_id` filtering, grouped `ingress_request_id` tracking, an overview detail sheet, and a dedicated full audit page
- REST API for querying statistics remains available for API callers and debugging:
  - List request logs with pagination and filters
  - Get the stats-only dashboard snapshot and separate dashboard recent activity feed
  - Get aggregated statistics (counts, averages, totals) with grouping
  - Get the usage snapshot and endpoint model statistics directly when needed
- Dashboard overview polls REST stats for the aggregate snapshot and separate recent activity feed.
- Dashboard Analytics polls the REST usage snapshot for the selected preset and treats each accepted snapshot as a full replacement; endpoint model statistics load through REST drilldown endpoints.

### 4.10 Request Audit Logging
Full HTTP request/response recording for proxied requests, stored in the database for auditing and debugging.

#### 4.10.1 Request-Time Audit Flags
- Audit rows store `audit_enabled_at_request` and `audit_capture_bodies_at_request` as request-time provenance
- Audit behavior does not derive runtime compatibility from catalog metadata
- Toggling audit settings affects new requests only

#### 4.10.2 What Gets Recorded
For each audited upstream attempt that is materialized:
- **Request**: HTTP method, full upstream URL, request-header snapshot, request body. Only the three configured auth-header values are replaced before the request-header snapshot is stored.
- **Response**: HTTP status code and response-header snapshot; captured response body bytes when body capture is enabled and bytes were captured. Response headers are stored as captured. For a multi-attempt request, response body capture is associated with the final attempt rather than every failed attempt.
- **Metadata**: model ID, api family, connection identity (connection ID, endpoint base URL, description), duration, stream flag, timestamp, link to corresponding `request_log` entry

#### 4.10.3 Sensitive Data Redaction
Audit redaction is intentionally limited:
- Upstream request-header values for `Authorization`, `X-API-Key`, and `X-Goog-Api-Key` are stored as `[REDACTED]`.
- Other upstream request headers, all upstream response headers, and captured request or response bodies are stored as captured and can contain sensitive information.
- Redaction happens at write time only for those three request-header names; audit access must therefore be restricted accordingly.

#### 4.10.4 Routing And Delivery Boundaries
Audit policy does not change model selection, Terminal Target selection, or client-facing response handling:
- A successful provider-forwarded `2xx` response requires the applicable durable telemetry handoff before Prism commits or first flushes the response. If that handoff fails before client-visible output, Prism returns a runtime observability error instead of the successful response.
- After handoff, background materialization and non-required side-effect failures are isolated from the proxied response path.
- Unsupported or wrong-method runtime registry rejections do not enter telemetry or audit handling.

#### 4.10.5 Audit Inspection (Frontend)
- Audit detail is opened from the request investigation flow on `/observe/requests/:requestId/audit` rather than a standalone `/audit` page
- Every successfully loaded request detail sheet provides an entry point to the dedicated audit page; the sheet itself remains overview-only and does not fetch audit payloads
- The dedicated page first loads the request detail, then shows request-time audit state as disabled, metadata-only, or full capture and resolves audit rows with `request_log_id`

#### 4.10.6 Body Size Limits
- When body capture is enabled, Prism can store captured request bodies for audited attempts and the captured response body for the final attempt only
- Current storage has no documented truncation marker

### 4.11 Batch Data Deletion
Provide flexible bulk deletion of historical logs and statistics data to manage database growth.
- Supported Data Types: `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`
- The Settings UI offers only `1`, `7`, `30`, `90` days, or all data. The management API accepts an explicit cutoff timestamp for callers that need a custom range.
- Deletion requests create durable management jobs; list/get/cancel job endpoints remain API-only in the current UI.
- Deleting `request_logs` does NOT delete linked `audit_logs`; audit rows retain weak request-log metadata, and audit reads expose `request_log_missing=true` only when both `request_log_id` and `request_log_created_at` are present but their profile-scoped tuple no longer resolves

### 4.12 Custom HTTP Headers per Terminal Target
Allow users to configure custom HTTP headers on individual Terminal Targets. These headers are appended to upstream proxy requests.
- Custom headers are configured during Terminal Target creation or editing
- Headers are stored as a JSON object
- Custom headers can override ordinary forwarded headers, but they cannot override Prism-controlled authentication or provider-version headers and cannot bypass the final Header Blocklist

### 4.13 Supported API Families
- The application exclusively supports the shipped OpenAI, Anthropic, and Gemini `api_family` values

### 4.14 Configurable Header Blocklist
Database-backed header blocklist with CRUD API. Supports exact and prefix match types. System defaults for Cloudflare tunnel metadata, tracing headers, and standard proxy headers. Applied by the Go runtime on every request.

### 4.15 Frozen Profile Scope
- Prism preserves the `profiles` table and all `profile_id` storage columns for historical attribution and a future unfreeze path.
- Profile-scoped management APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.
- Global management routes stay outside profile scoping. Global routes include auth, auth-setting flows, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Profile lifecycle APIs are not exposed in the current management surface.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and always resolves against frozen Default profile id `1`; `X-Profile-Id` and profile fields remain compatibility/storage attribution only.
- Observability rows (`request_logs`, `audit_logs`) carry immutable `profile_id` attribution for historical correctness.


## 5. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Deployment | Root Compose self-hosted bundle uses one Prism app image plus PostgreSQL; the app image runs the Go backend behind Nginx, and the local launcher runs PostgreSQL, backend, and optional Vite frontend |
| Authentication | Optional operator auth for `/api/*`; optional proxy API keys for `/v1/*` and `/v1beta/*` |
| Latency overhead | < 50ms added to proxy requests |
| Concurrent requests | Support 10+ simultaneous proxy requests |
| Database | PostgreSQL (Go-managed startup migrations) |
| API standard | Markdown API contract maintained in `docs/architecture.md` (section 14, API Reference) |
| CORS | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Go 1.26.5, chi, pgx |
| HTTP Client | Go `net/http` streaming transport |
| Database | PostgreSQL via pgx |
| Frontend | React 19, Vite 8, TypeScript, Tailwind CSS 4, shadcn/ui, TanStack Router |
| API Contract | Markdown reference in `docs/architecture.md` (section 14, API Reference) |
| Communication | REST API with JSON and SSE for streaming proxy responses |

## 7. Out of Scope (v1)

- Auth-based multi-tenancy, multi-operator RBAC, and per-user data isolation beyond the single-operator auth surface and profile namespace isolation
- Usage-based billing/accounting integrations beyond Prism's built-in telemetry and costing reports
- Global runtime rate limiting outside per-Terminal Target `qps_limit` and in-flight limits
- External secret-manager integrations beyond Prism's built-in endpoint-secret encryption at rest

## 8. Requests Page Specification


**Scope:** Frontend route `/observe/requests` and its request-investigation helper cluster

### 1. Overview

The Requests page is Prism's dedicated request-browser and investigation surface for proxied traffic. It is mounted at `/observe/requests`. It provides a Default-profile-pinned view for browsing request history through a slim retained filter set and inspecting request-level overview details, with full audit payloads on a dedicated audit page.

The backend request-log and audit APIs remain the source of truth. The frontend route is responsible for presenting that data in an operator-friendly investigation workflow without changing runtime proxy semantics. The canonical URL filter set keeps `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `priced`, `unpriced_reason`, and `time_range`, while exact single-request investigation uses `request_id`.

The request-log route now uses split HTTP contracts: a slim list payload for browsing and a dedicated grouped detail payload for the sheet. Caller client filtering is server-backed through `client_rule_id` and matches `caller_user_agent` only. Upstream user-agent display stays informational.

### 2. Goals

- Provide a dedicated request-history route at `/observe/requests`.
- Support deep investigation of a single request through URL-addressable state.
- Keep the retained browse filters server-backed and URL-addressable.
- Expose linked audit payloads only when needed.
- Support implemented drill-down entry points from dashboard overview and dashboard recent activity.
- Show requested model identity separately from the final target model chosen by unified access-target resolution.
- Show requested model identity separately from final target, selected Terminal Target, and endpoint.

### 3. Non-Goals

- Replace dashboard or statistics summaries.
- Change backend request-log, audit-log, or costing contracts.
- Change frozen Default-profile runtime routing behavior for `/v1/*` and `/v1beta/*`.

### 4. Route Responsibilities

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

### 5. URL State Contract

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

### 6. Data And API Requirements

#### 6.1 Request Log Fetch

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

#### 6.2 Filter Option Bootstrap

The page derives model, endpoint, caller client, and final-target filter options from the paginated `/api/stats/requests` response: `filter_options.models`, `filter_options.endpoints`, `filter_options.clients`, and `filter_options.resolved_target_models`.

Response-owned filter options should become ready when the current list response arrives. `filter_options.clients` entries use `{ client_rule_id, client_label }` and represent enabled User-Agent Client Rules. Selecting one sends `client_rule_id` back to the backend, where matching is caller-only against `caller_user_agent`.

#### 6.3 Dedicated Audit Resolution

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

### 7. UX Workflow Requirements

#### 7.1 Filter And Triage Workflow

The page should use only the retained browse filters in URL state and send them directly to the backend list route. The current canonical URL contract keeps `request_id`, `selected_request_id`, `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `priced`, `unpriced_reason`, and `time_range`, and removes the old client-side search, token, latency, stream, outcome, and triage refinement layer. The Client dropdown must not expose regex, `client_scope`, or upstream matching language.

#### 7.2 Exact-Request Investigation Workflow

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

#### 7.3 Table Workflow

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

#### 7.4 Detail Sheet Workflow

`RequestLogDetailSheet` exposes an overview-only inspection sheet with request metadata, requested model vs final target model identity, token and cost breakdowns, and routing context.

Every successfully loaded request detail provides a link to the dedicated full audit page. The sheet does not conditionally hide that entry and does not fetch audit payloads. The target page then renders one of three request-time states: disabled, metadata-only, or full capture.

Dense overview requirements:

- Keep the same logical groups: `Request details`, `Routing context`, `Token usage`, and `Cost breakdown`.
- Render a compact summary strip for latency, token, cost, and timestamp context above the grouped sections.
- Cost breakdown includes priced/billable state, unpriced reason, report currency, original/source currency, FX rate and source, pricing unit, pricing configuration version, and all five pricing snapshot values.
- Operation name, upstream operation name, translation mode, and upstream path are returned by the backend detail API, but the current frontend detail type/sheet displays the request path and does not render those operation/translation fields.
- Keep audit payload loading out of the sheet and scoped to the dedicated full audit page.

#### 7.5 Payload Views And Copy

The dedicated audit page renders request and response headers plus request and response bodies. For each non-empty payload block:

- `Rendered` shows the structured document view when Prism recognizes the payload. Header rendering additionally masks `authorization`, `proxy-authorization`, `cookie`, `set-cookie`, and header names containing `api-key`, `token`, `secret`, or `credential` (case-insensitive).
- `Raw JSON` pretty-prints stored body payloads. For header blocks, it shows a browser-normalized header representation with the same additional masking rather than the unmodified stored text.
- Copying in raw mode copies the transformed text currently shown. Copying in rendered mode copies the underlying stored text, not the browser-masked header display; the three request auth-header values redacted by the backend at write time remain redacted because the persisted values are `[REDACTED]`.
- Empty bodies disable the copy control. Clipboard API failure or absence falls back to a temporary local textarea mounted under the page or sheet's `[data-clipboard-fallback-root]`.

### 8. Module Boundaries

The `frontend/src/pages/request-logs/` helper cluster should remain page-specific and own the following responsibilities:

- query-parameter definitions and parsers
- retained browse-filter state and exact-request mode orchestration
- sticky filter-bar UI groups
- column definitions and row renderers, including requested model vs final target model identity rendering and caller/upstream client display
- overview-only detail sheet and shared panels over the dedicated request-detail payload
- dedicated full audit page loading hook
- URL/filter and audit-state seam contracts plus the dedicated request-log/audit Playwright journey

### 9. Cross-Route Integrations

Other frontend surfaces should be able to deep-link into `/observe/requests` with scoped context.

#### 9.1 Dashboard

Dashboard should support request-log drill-down entry points for:

- quick action button: `Review Requests`
- recent activity row drill-downs by `request_id`

### 10. Required Contracts

The Requests page must remain compatible with the following backend-facing and shared frontend contracts:

- `RequestLogListItem` for the browse table and related list consumers
- `RequestLogDetail` for the detail sheet only
- `api.stats.requests()` for browsing slices and `/api/stats/requests/{request_id}` for exact detail
- audit API client methods
- dashboard flows that consume request-derived backend responses
- caller-client and final-target observability fields such as `client_rule_id`, `filter_options.clients`, and `resolved_target_model_id`

### 11. Acceptance Criteria

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

## 9. Workflows Reference


This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in `frontend/src/app/router/appRouter.tsx`, `frontend/src/app/router/rewriteRoutes.ts`, the live Go backend API surface, and the markdown API reference.

Validated again against current repo surfaces on 2026-07-10:
- `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` are all `1.0.4`, which is the current backend/frontend version surface.
- The protected frontend route shell mounts observe, request-log, model, route, settings, proxy-key, and pricing workflows; analytics lives under `/observe`.

### Evidence Sources

- Frontend route surface: `frontend/src/app/router/appRouter.tsx` and `frontend/src/app/router/rewriteRoutes.ts`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/useShellNavigation.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Default-profile scoping: `frontend/src/lib/api/core.ts`, `frontend/src/lib/api/profileScope.ts`
- Backend router assembly: `backend/internal/httpapi/management/`, `backend/internal/httpapi/runtime/`, and `backend/internal/platform/http/server.go`
- Backend API reference: `docs/architecture.md` (section 14, API Reference)
- Request-log details: `docs/product.md` (section 8, Requests Page Specification)

### Runtime URLs

- Frontend: `http://localhost:5173`
- Backend: `http://localhost:8000` for a fresh repo-local bootstrap seed; existing selected bootstrap files can choose another backend port
- Health: `http://localhost:8000/health` for that fresh seed

### Shared Scope Rules

- Public auth routes are `/auth/login`.
- Protected shell routes cover `/observe`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/models`, `/models/:id`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, and `/control/proxy-keys`; analytics is under `/observe?tab=analytics`.
- Profile-scoped management requests are pinned to Default profile id `1`. `X-Profile-Id` is still accepted for compatibility, but the backend ignores its value.
- Global management routes omit `X-Profile-Id` and include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and resolves against frozen Default profile id `1`.

### 1. Sign In And Session Bootstrap

**User entrypoints**

- `/auth/login`

**Frontend flow**

1. `AuthProvider` chooses public bootstrap mode for auth-only routes.
2. The login page loads auth state before showing the form.
3. The login form offers `session`, `7_days` (the backend default), and `30_days` session durations.
4. Successful login returns to the safe local `redirect` path captured from the full protected pathname, query string, and hash; without a valid redirect it falls back to `/observe`.
5. Passive and proactive refresh keep the operator session alive while the tab stays active.

**UI-driven backend touchpoints**

- `GET /api/auth/public-bootstrap`
- `GET /api/auth/status`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `POST /api/auth/refresh`
- `GET /api/auth/session`

### 2. Shell Bootstrap

**User entrypoints**

- Any protected route

**Frontend flow**

1. `AuthProvider` confirms the operator session.
2. The shell renders sidebar groups, breadcrumbs, language/theme controls, and the version label.
3. Default-profile pages send the pinned compatibility `X-Profile-Id: 1` header from the shared API client.

**UI-driven backend touchpoints**

- `GET /api/auth/status`
- Default-profile-scoped `/api/*` routes with accepted-but-ignored `X-Profile-Id`

### 3. Dashboard And Statistics

**User entrypoints**

- `/observe`
- `/observe?tab=analytics`

**Frontend flow**

1. Dashboard overview bootstrap loads KPI cards, spending summaries, and average RPM/metric snapshots from the stats-only aggregate snapshot.
2. Dashboard overview bootstrap loads recent activity from the separate recent-activity feed.
3. The dashboard also loads current loadbalance incident state for active-ban and recent-event alerts. The backend snapshot's `routing_health_map` is a response field only and is not rendered by the current dashboard.
4. The dashboard polls REST stats endpoints every 30 seconds for aggregate and recent-activity reconciliation.
5. Quick actions send operators into the analytics tab or `/observe/requests` for deeper analysis.
6. The analytics tab stays aggregate-focused and uses its own snapshot presets rather than request-level drill-down.

**Backend touchpoints**

- `GET /api/stats/dashboard` for the overview aggregate snapshot; its backend-computed `routing_health_map` response field is not rendered by the current dashboard
- `GET /api/stats/dashboard/recent-activity` for bounded request-history-backed dashboard activity
- `GET /api/loadbalance/incidents` for active bans and recent loadbalance incidents
- `GET /api/stats/usage-snapshot` for API callers, debugging, analytics polling, and manual refresh
- `GET /api/stats/endpoints/{endpoint_id}/models` for analytics endpoint drilldown rows

### 4. Model Management And Model Detail

**User entrypoints**

- `/models`
- `/models/:id`

**Frontend flow**

1. Operators list, search, create, edit, and delete model configs.
2. Model create and edit dialogs manage model metadata, OpenAI accepted format, loadbalance strategy, and enabled state.
3. Model detail owns ordered same-family access-target authoring and Terminal Target management for the model's private endpoint bindings.
4. Request logs preserve the requested model while final-target fields show the terminal model reached through the access graph.

**UI-driven backend touchpoints**

- `GET /api/models`
- `POST /api/models`
- `GET /api/models/{model_config_id}`
- `PUT /api/models/{model_config_id}`
- `DELETE /api/models/{model_config_id}`
- `POST /api/models/by-endpoints`
- `GET /api/models/{model_config_id}/targets`
- `POST /api/models/{model_config_id}/targets`
- `PATCH /api/models/{model_config_id}/targets/{target_id}`
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position`
- `DELETE /api/models/{model_config_id}/targets/{target_id}`
- `GET /api/models/{model_config_id}/connections`
- `POST /api/models/{model_config_id}/connections`
- `PATCH /api/models/{model_config_id}/connections/{connection_id}`
- `DELETE /api/models/{model_config_id}/connections/{connection_id}`

`POST /api/models/by-endpoints` is used by the Endpoints page to hydrate model references. `GET /api/models/by-endpoint/{endpoint_id}` and `POST /api/models/connections/batch` remain backend/API-client surfaces without a current production frontend caller.

### 5. Endpoints, Loadbalance Strategies, And Pricing Templates

**User entrypoints**

- `/route/endpoints`
- `/route/ban-policies`
- `/route/pricing`

**Frontend flow**

1. Endpoints define reusable upstream credentials and base URLs that Terminal Targets can share.
2. The Ban Policies page exposes only `Strategies`, `Current State`, and `Events`; it does not render incidents. The dashboard consumes `/api/loadbalance/incidents` for its incident banner and recent-event alerts.
3. Pricing templates define reusable cost models attached to Terminal Targets with five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`.
4. Pricing-template management saves explicit strings for every component. Missing/null/blank inputs normalize to `"0"`; explicit `"0"` is configured free pricing, not missing pricing data.
5. Pricing supports JSON file or pasted-text import with `upsert_by_name` or `create_only`, a connection-usage lookup, and delete protection when Terminal Targets still depend on a template.
6. Request logs and cost math consume canonical disjoint token components: base input, cache-read input, cache-creation input, base output, and reasoning output. Aggregate `cached_tokens` is derived-only for presentation.
7. These resources are Default-profile-scoped and are usually managed before or alongside model-detail work.
8. The defaults action creates the canonical loadbalance strategy rows for Default profile id `1`.

**UI-driven backend touchpoints**

- `GET /api/endpoints`
- `POST /api/endpoints`
- `PUT /api/endpoints/{endpoint_id}`
- `DELETE /api/endpoints/{endpoint_id}`
- `PATCH /api/endpoints/{endpoint_id}/position`
- `POST /api/endpoints/{endpoint_id}/duplicate`
- `GET /api/loadbalance/strategies`
- `POST /api/loadbalance/strategies/defaults`
- `POST /api/loadbalance/strategies`
- `GET /api/loadbalance/strategies/{strategy_id}`
- `PUT /api/loadbalance/strategies/{strategy_id}`
- `DELETE /api/loadbalance/strategies/{strategy_id}`
- `GET /api/loadbalance/current-state`
- `POST /api/loadbalance/current-state/{connection_id}/reset`
- `GET /api/loadbalance/events`
- `GET /api/loadbalance/events/{event_id}`

`GET /api/endpoints/connections` remains a shared API-client/reference-data catalog surface without a current production frontend consumer.

The loadbalance strategy routes are pinned to Default profile id `1`, and the defaults action is a no-body POST that returns the created/current canonical rows plus creation metadata.
- `GET /api/pricing-templates`
- `POST /api/pricing-templates`
- `POST /api/pricing-templates/import`
- `GET /api/pricing-templates/{template_id}`
- `PUT /api/pricing-templates/{template_id}`
- `DELETE /api/pricing-templates/{template_id}`
- `GET /api/pricing-templates/{template_id}/connections`

### 6. Request Investigation

**User entrypoints**

- `/observe/requests`
- Dashboard deep links into `/observe/requests`

**Frontend flow**

1. Operators browse request history with server-backed filters.
2. Exact request investigation opens the overview detail sheet through `request_id`.
3. `ingress_request_id` groups all upstream attempts for one incoming proxy request.
4. The request-log UI keeps the requested `model_id` separate from the final `resolved_target_model_id` so operators can see authoring intent and execution target at the same time.
5. The Client filter sends `client_rule_id` to the backend and matches caller User-Agent Client Rules against `caller_user_agent` only.
6. Audit payloads load only on the dedicated `/observe/requests/:requestId/audit` page; the detail sheet remains overview-only.

**Backend touchpoints**

- `GET /api/stats/requests`
- `GET /api/stats/requests/{request_id}`
- `GET /api/audit/logs`
- `GET /api/audit/logs/{log_id}`
- `GET /api/settings/timezone`

For the page-specific query contract and UI behavior, see section 8 (Requests Page Specification).

### 7. Settings And Access Control

**User entrypoints**

- `/system/settings`
- `/control/proxy-keys`

**Frontend flow**

1. Settings visibly splits into `全局` and `实例` tabs. Their internal query values remain `profile` and `global`.
2. The `全局` tab covers reporting currency and FX mappings, timezone, audit/privacy defaults, and config rules. Rows with missing FX data remain pricing failures; explicit `"0"` component prices are configured free pricing and do not become `MISSING_PRICE_DATA`.
3. The `实例` tab covers operator authentication, log-retention policies, and deletion requests.
4. Startup settings remain in the plaintext bootstrap file selected by `PRISM_CONFIG_PATH`; edit `config.json` directly and restart Prism to apply changes.
5. Saving a transition from disabled to enabled authentication broadcasts a localStorage auth-state update, refreshes local auth state, and redirects the current tab to `/auth/login`; other open tabs re-bootstrap from the broadcast.
6. Proxy API keys are managed on their own route and stay global rather than profile-scoped. They can be issued while auth is disabled, but runtime proxy-key authentication is enforced only after operator auth is enabled.

Mail bootstrap fields remain parse-compatible for existing `config.json` files, but Prism no longer sends mail. Fresh bootstrap seeds use backend `8000`, frontend `5173`, and PostgreSQL `15432`, but `./start.sh` follows the existing bootstrap file's configured `server.port` when one already exists. `runtime.transport.requestTimeout` is seeded as `"300s"`, and `runtime.sideEffects.attemptTimeout` is seeded as `"10s"`. Direct external `config.json` edits are not watched automatically, and existing valid files are not rewritten by the launcher. To reset startup defaults, stop Prism, remove or relocate the bootstrap file, and restart.

OpenAI text routing is native-only. Operators set runtime support on each Terminal Target through `openai_text_capability`, using `responses_only`, `chat_completions_only`, or `dual_native`; incompatible Chat Completions/Responses attempts are skipped rather than translated.

**Backend touchpoints**

- `GET /api/settings/costing`
- `PUT /api/settings/costing`
- `GET /api/settings/timezone`
- `PUT /api/settings/timezone`
- `GET /api/settings/audit`
- `PUT /api/settings/audit`
- `GET /api/config/header-blocklist-rules`
- `GET /api/config/header-blocklist-rules/{rule_id}`
- `PATCH /api/config/header-blocklist-rules/{rule_id}`
- `DELETE /api/config/header-blocklist-rules/{rule_id}`
- `POST /api/config/header-blocklist-rules`
- `GET /api/config/user-agent-client-rules`
- `GET /api/config/user-agent-client-rules/{rule_id}`
- `POST /api/config/user-agent-client-rules`
- `PATCH /api/config/user-agent-client-rules/{rule_id}`
- `DELETE /api/config/user-agent-client-rules/{rule_id}`
- `GET /api/settings/auth`
- `PUT /api/settings/auth`
- `GET /api/settings/auth/proxy-keys`
- `POST /api/settings/auth/proxy-keys`
- `PATCH /api/settings/auth/proxy-keys/{key_id}`
- `POST /api/settings/auth/proxy-keys/{key_id}/rotate`
- `DELETE /api/settings/auth/proxy-keys/{key_id}`
- `GET /api/settings/log-retention`
- `PUT /api/settings/log-retention`
- `POST /api/maintenance/log-retention/jobs`

Global log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.

The Settings UI exposes fixed deletion presets of `1`, `7`, `30`, `90` days, or all data, then creates a durable retention job and shows its `job_id`/`status_url` in a toast. The retention API accepts explicit cutoff timestamps, while `GET /api/management/jobs`, `GET /api/management/jobs/{job_id}`, and `POST /api/management/jobs/{job_id}/cancel` are API-only follow-up endpoints; the current UI does not list, inspect, or cancel jobs.

### 8. Runtime Proxy Traffic

Runtime auth follows the latest proxy-key snapshot immediately after auth and proxy-key management writes: rotated, disabled, or expired keys stop authorizing new supported `/v1` and `/v1beta` runtime operations. Create and rotate return a secret shown once in a copyable dialog; the ledger keeps key metadata and rotation lineage, while DELETE removes the row.

**User entrypoints**

- External clients calling one of the operation-registered runtime routes listed below

**Runtime flow**

1. Global CORS runs first. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the exact operation registry in that order. Once inside the registry, unsupported routes and wrong methods reject before body reads, provider transport, telemetry, audit, feedback, or runtime side effects.
2. Provider adapters parse provider-specific payloads, build upstream requests, adapt responses, classify streams, extract usage, and own pure OpenAI Chat/Responses conversion.
3. Planning evaluates all ordered same-family model targets into an eligible Terminal Target aggregate; direct private Terminal Targets are used only when no model-target candidate is eligible.
4. Connection planning applies the attached explicit Ban Policy strategy and per-connection limits.
5. The shared runtime/gateway owns operation registration, admission, routing, SSE lifecycle, accounting, pricing, request-log metadata, and durable handoff. Telemetry/audit rows are materialized by background workers from the runtime outbox; non-accepted side effects use their own in-memory or worker queues.
6. After the first downstream byte or event on a stream, no retry, redirect, or hedge replay can start.
7. Missing pricing stays visibly degraded or unpriced, it never silently looks complete.

**Backend touchpoints**

- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/responses/input_tokens`
- `POST /v1/responses/compact`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1beta/models/{model}:streamGenerateContent`
- `POST /v1beta/models/{model}:countTokens`

These 10 allowlisted runtime routes are defined in `backend/internal/httpapi/runtime/operations.go` and are intentionally separate from `/api/*` management routes. Prism does not treat `/v1` or `/v1beta` as catch-all prefixes.
`GET /v1/models` remains local: without `client_version` it returns the OpenAI-shaped list; with `client_version` it returns the current Codex catalog, seeded from the embedded copy and refreshed asynchronously from OpenAI at startup and every 24 hours, plus a content-derived weak `ETag` and `304 Not Modified` on an exact `If-None-Match`.

### 9. Priority Operations Runbook

Before shipping priority-sensitive backend changes, run the standard priority regression tests from the backend tree:

```bash
cd backend && go test ./tests/priority/...
```

The expected pass signal is exit code `0`. Failures should be treated as regressions in the priority classification, admission, scheduler, or lane-isolation behavior covered by the checked-in backend suite.

Operational triage by symptom:

- Lane budget pressure: identify the labeled DB lane first. `runtime_execution` protects proxy work; `management`, `cache_refresh`, `runtime_telemetry`, `runtime_feedback`, and `background_jobs` have separate budgets and should be remediated at their owning workload instead of increasing unrelated pools.
- Overload or `Retry-After`: honor the retry delay and reduce client concurrency. M3 reporting and maintenance routes are expected to shed before M2/M1 management work, and management/background pressure should not affect proxy execution capacity.
- Scheduler lag: expect delayed, coalesced, retried, or dropped background work according to worker policy. Do not add ad hoc goroutines or timers; register new recurring, retrying, or delayed work with the scheduler.
- Outbox failures: inspect the relevant durable store state. Management side-effect outbox rows retry or become permanent failures without rolling back committed primary state.
- Runtime telemetry loss: accepted runtime activity intents should drain to the telemetry outbox unless terminal validation or forced shutdown prevents completion. Treat lost accepted telemetry as a durability incident.
- Runtime feedback loss: feedback is best effort and may drop on queue full, invalid event, closed pipeline, or store failure. Drops should be accounted for, but they must not delay or fail proxy responses.
- Audit or stat lag: raw audit reads remain bounded by time window and keyset cursor. Dashboard overview reads come from the canonical `/api/stats/dashboard` aggregate snapshot; it returns backend-computed Routing Health Map data, but the current dashboard does not render that field. Broad deletes run as durable management jobs.
- Cache generation lag: management mutations advance durable runtime-cache generations before commit. Cache warming may lag, but runtime reads compare generation vectors and refresh or fail closed for stale, missing, or unverifiable auth-sensitive snapshots.

### Cross-References

- Product scope: `docs/product.md`
- API contracts: `docs/architecture.md` (section 14, API Reference)
- Request investigation details: `docs/product.md` (section 8, Requests Page Specification)
