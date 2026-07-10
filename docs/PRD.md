# Product Requirements Document: Prism

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
- `GET /v1/models` is local: requests with `client_version` use the embedded Codex catalog shape with a deterministic weak ETag/`304` path; requests without it use the local OpenAI `object`/`data` list

### 4.2 Model Configuration
- Map each model to a fixed runtime `api_family`
- Models expose one ordered `access_targets` list whose public entries point to same-family models
- Terminal Targets carry endpoint, costing, health, admission-limit, and auth metadata as model-private endpoint bindings owned by one model
- Select which access targets are enabled for each model; enabled models require at least one enabled target
- CRUD operations for all configurations are available via REST API

### 4.3 Unified Model Access Routing
- Ordered same-family model targets are evaluated as an aggregate; direct Terminal Targets are the fallback when no model-target candidate is eligible
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
- Request-log materialization distinguishes actual upstream attempts from synthetic failures. Telemetry-eligible target-resolution/translation errors carrying `PlanningFailure`, plus `admission_exhausted`, can produce synthetic rows without endpoint/connection. Malformed bodies, unknown models, API-family mismatches, registry-rejected requests, and the terminal all-transport `502` path do not retain request history.
- Request-log detail preserves requested public model identity, final target identity, selected Terminal Target, endpoint, operation names, and translation mode through flat fields.
- Retry scheduling, retry exhaustion, bans, unbans, recovery, and admission-rejection transitions are persisted as `loadbalance_events` for audit and observability.

### 4.5 Default-Profile Endpoints & Terminal Targets
- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Models** carry fixed `api_family` metadata.
- **Terminal Targets** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile.
- Endpoints can be reused across multiple models within the same profile.
- Deleting an endpoint is blocked if any Terminal Targets in that profile still reference it.

### 4.6 Terminal Target Request Health
- Manual Terminal Target test actions are removed from the management API and UI.
- Backend request-derived stats can expose Terminal Target success-rate and routing-health read models for real runtime traffic. These are API/read-model capabilities, not a current per-target health-badge workflow.
- The Models page renders plain telemetry text for each model: 24-hour success rate, P95 latency, and 24-hour request count, plus a 30-day spend value. Missing success data is shown as `- Success`; there are no colored success-rate thresholds or health badges.
- The current model-detail UI does not render Terminal Target success-rate indicators, and the dashboard does not render the backend `routing_health_map` response field.

### 4.7 Web UI (Management Dashboard)
- View all configured models and their reachable Terminal Targets
- Add/edit/delete model configurations with ordered access targets
- Add/edit/delete profile-scoped endpoints
- Add/edit/delete Terminal Targets from model detail
- Toggle enabled/disabled access targets per model
- Select an explicit load-balance strategy with Ban Policy settings per model
- Dedicated model-detail route (`/models/:id`) for ordered access-target and Terminal Target configuration; current loadbalance state and loadbalance event history live under Ban Policies
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
- **OpenAI (streaming)**: Accumulated from terminal SSE usage events; translated Chat streams inject `stream_options.include_usage=true` upstream when needed
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
Audit policy does not change model selection, Terminal Target selection, or client-facing response translation:
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
| API standard | Markdown API contract maintained in `docs/API_SPEC.md` |
| CORS | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Go 1.26.5, chi, pgx |
| HTTP Client | Go `net/http` streaming transport |
| Database | PostgreSQL via pgx |
| Frontend | React 19, Vite 8, TypeScript, Tailwind CSS 4, shadcn/ui, TanStack Router |
| API Contract | `docs/API_SPEC.md` markdown reference |
| Communication | REST API with JSON and SSE for streaming proxy responses |

## 7. Out of Scope (v1)

- Auth-based multi-tenancy, multi-operator RBAC, and per-user data isolation beyond the single-operator auth surface and profile namespace isolation
- Usage-based billing/accounting integrations beyond Prism's built-in telemetry and costing reports
- Global runtime rate limiting outside per-Terminal Target `qps_limit` and in-flight limits
- External secret-manager integrations beyond Prism's built-in endpoint-secret encryption at rest
