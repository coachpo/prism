# Product Requirements Document: Prism

## 1. Product Overview

Prism is a lightweight, self-hosted application that acts as a unified proxy for multiple LLM API families. It lets a single user configure, route, and load-balance requests through a web-based management UI.

## 2. Problem Statement

Developers and power users working with multiple LLM API families face:
- Managing multiple API keys and base URLs across different tools
- No unified endpoint for switching between API families
- No automatic failover when an API family is down or rate-limited
- Manual configuration changes when rotating keys or endpoints
- Limited visibility into external CLIProxyAPI auth/provider inventories when those sidecars are part of the local toolchain

## 3. Target User

Single operator (developer/power user) running the application locally or on a local network. Prism supports optional operator authentication for management APIs and proxy API keys for runtime traffic. It also supports profile-based configuration isolation for one operator (selected profile vs active profile); this is not auth multi-tenancy.

## 4. Core Features

### 4.1 Multi-Family Proxy
- Operation-registered proxy support for explicit OpenAI Chat Completions, Responses, Images, Anthropic Messages/count-token, and Gemini generate/stream/count-token operations
- Supports both streaming (SSE) and non-streaming responses
- Preserves native request/response formats per API family
- Runtime compatibility is fixed by `api_family`

### 4.2 Model Configuration
- Map each model to optional `vendor_id` metadata plus a fixed runtime `api_family`
- Models expose one ordered `access_targets` list whose public entries point to same-family models
- Terminal Targets carry endpoint, costing, health, admission-limit, and auth metadata as model-private endpoint bindings owned by one model
- Select which access targets are enabled for each model; enabled models require at least one enabled target
- CRUD operations for all configurations are available via REST API

### 4.3 Unified Model Access Routing
- Ordered access targets resolve recursively to Terminal Targets before runtime execution
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
  - **Automatic failover** when an attempt returns a failover-triggering status (`403`, `429`, `500`, `502`, `503`, `529`) or raises connection/timeout errors
  - Models attach one reusable explicit Ban Policy strategy using `single`, `fill-first`, or `round-robin` routing
  - Upstream request timing uses shared backend timeout settings, while Ban Policy owns retry windows, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, `temporary` or `until_reset` bans, and failover status codes
  - Ban Policy thresholds are inclusive: retry-cycle exhaustion uses `cycle_retry_attempts >= cycle_retry_attempt_limit`, and bans use `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`
- Failover-worthy HTTP responses are governed by the attached strategy's configured failure status codes and retry-window settings
  - Non-failover client errors (for example `400`, `404`, `422`) do not force-clear existing Ban Policy state
  - Each Terminal Target can optionally define `qps_limit`, `max_in_flight_non_stream`, and `max_in_flight_stream`; `null` means unlimited
  - Limiter state is persisted in PostgreSQL `UNLOGGED` tables and is intentionally ephemeral after crash or unclean shutdown
- Proxy request forwarding may apply compatibility normalizations while preserving API-family-native response formats
- CLIProxyAPI context overflow promotion is available only for non-stream OpenAI Chat Completions and Responses attempts, only before downstream commit, and only once to a model's explicit `context_overflow_promotion_target_id`. The known upstream for this path is CLIProxyAPI, not official OpenAI, so Prism requires body-confirmed overflow evidence; plain `429` never promotes. Native paths can use OpenAI-style or unambiguous flat gateway JSON bodies, while translated paths reject flat gateway JSON for promotion in v1. Exact facades keep their selected child restriction and never reopen siblings.
- All failover attempts (including failed ones) are logged to `request_logs` for observability. When a Terminal Target returns a failover-triggering status code (`403`, `429`, `500`, `502`, `503`, `529`) or encounters a connection/timeout error, the failed attempt is logged before trying the next Terminal Target.
- Failed source attempts from context overflow promotion are logged as attempt-level observability, while final usage, status, and pricing ownership come from the final returned response.
- Failover, recovery, and probe-eligibility transitions are persisted as `loadbalance_events` for audit and observability.

### 4.5 Profile-Scoped Endpoints & Terminal Targets
- **Vendors** remain global publisher metadata shared across profiles.
- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Models** carry optional `vendor_id` metadata plus fixed `api_family` metadata.
- **Terminal Targets** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile.
- Endpoints can be reused across multiple models within the same profile.
- Deleting an endpoint is blocked if any Terminal Targets in that profile still reference it.

### 4.6 Terminal Target Health Detection
- Manual health check remains available for each Terminal Target from the management UI.
- Health probes send a minimal request using the Terminal Target's configured model ID and the same URL-building logic as the proxy engine to validate URL routing, authentication, and model availability end to end.
- API-family-specific request format:
  - **OpenAI**: `POST {base_url}/v1/responses` or `POST {base_url}/v1/chat/completions` based on `openai_probe_endpoint_variant`; the current OpenAI variants are `responses_minimal`, `responses_reasoning_none`, `chat_completions_minimal`, and `chat_completions_reasoning_none`
  - **Anthropic**: `POST {base_url}/v1/messages` with `model`, `max_tokens: 1`, and a simple message
  - **Gemini**: `POST {base_url}/v1beta/models/{model}:generateContent` with minimal content payload
  - 2xx response → `healthy`
  - 401/403 → `unhealthy` (authentication failed)
  - 429 → `healthy` (connection works, just rate-limited)
  - Connection error / timeout → `unhealthy`
  - Other errors → `unhealthy`
- Health checks are available in:
  - Model Detail -> Terminal Targets list -> Actions menu ("Check Health")
  - Model Detail -> Add/Edit Terminal Target dialog ("Test Terminal Target" button)

### 4.6.1 Terminal Target Success Rate Badge
- Each Terminal Target displays a **success rate badge** computed from `request_logs` data
- Success rate = `COUNT(2xx status codes) / COUNT(total requests) * 100` for that Terminal Target
- Badge color thresholds:
  - **Green** (≥98%): Excellent health
  - **Yellow** (75%–97.99%): Degraded health
  - **Red** (<75%): Poor health
  - **Gray** (N/A): No request data available (0 total requests)
- The success rate badge is the primary visual indicator in the Terminal Targets list on the Model Detail page
- The manual health check still updates `health_status` and `health_detail` in the database
- Tooltip on hover shows: success rate percentage, total requests count, success/error counts, and last health check detail (if available)

### 4.6.2 Model Health Display
- Each model displays an aggregated health indicator on the Dashboard and Models pages
- Model health is computed by aggregating the success rates of all its active Terminal Targets
- Model health = weighted average of Terminal Target success rates (weighted by request count per Terminal Target)
- If a model has no request data across any Terminal Target, it shows "N/A" (gray)
- Display format: A colored badge showing the aggregated success rate percentage
  - Same color thresholds as Terminal Target badges: ≥98% green, 75-98% yellow, <75% red, N/A gray
- Shown in:
  - **Dashboard** -> Model Overview table -> "Success Rate" column
  - **Models** page -> Model list table -> "Success Rate" column

### 4.7 Web UI (Management Dashboard)
- View all configured models and their reachable Terminal Targets
- Add/edit/delete model configurations with ordered access targets
- Add/edit/delete profile-scoped endpoints
- Add/edit/delete Terminal Targets from model detail
- Toggle enabled/disabled access targets per model
- Select an explicit load-balance strategy with Ban Policy settings per model
- Manual health check for Terminal Targets with visual status indicators
- Dedicated model-detail route (`/models/:id`) with manual health checks, Terminal Target KPIs, current loadbalance state, and loadbalance event history
- Dedicated request-log browsing and investigation at `/request-logs`, separate from dashboard analytics
- Dedicated routes for pricing templates and proxy API key lifecycle management
- Dedicated `/sidecars` route for global CLIProxyAPI sidecar registration, sync, auth/provider inventory, and direct auth-file mutation
- Dashboard analytics lives under `/dashboard?tab=analytics` and replaces the old standalone statistics route
- Global profile selector in the app shell controls the selected profile (management scope).
- Active profile indicator is shown globally; runtime activation is an explicit action.
- The protected shell bootstraps profile state from one profile-bootstrap response, while sidebar navigation and breadcrumbs are derived from local route metadata.
- Profile create/edit/delete dialogs include active-profile delete guardrails and capacity guidance.
- Settings is split between Profile-scoped sections (backup, billing/currency, timezone, audit/privacy, retention/deletion, and config rules) and a Global tab for instance auth plus shared vendor management.

### 4.8 Configuration Persistence
- Runtime and management configuration is stored in PostgreSQL with Go-backend-managed schema migrations applied at startup
- Startup/bootstrap process settings are owned by the plaintext `config.json` bootstrap file and managed through `/settings#startup`
- The default profile exists from the first startup and remains editable after initialization
- Config export/import uses the split-bundle contract: profile bundles are `version: 3` with `bundle_kind: profile_config`, and vendor catalog bundles are `version: 1` with `bundle_kind: vendor_catalog`
- Profile bundles carry `vendor_refs`, `profile_settings`, encrypted `secret_payload`, top-level `loadbalance_strategies`, top-level `connections` for Terminal Targets, model `access_targets`, nullable `vendor_key`, and `api_family`
- Profile import preview validates bundle kind, version, secret decryption, and vendor resolution before replace-mode import; unsupported versions are rejected
- Database setup is managed by the Go backend runtime and applies the checked-in fresh-install baseline on startup
### 4.9 Request Statistics & Analytics
- Automatic logging of all proxy requests with telemetry data
- Each request log captures: profile ID attribution, requested `model_id`, final `resolved_target_model_id` when an access-target path is selected, `api_family`, Terminal Target used through compatibility connection attribution (ID, endpoint base URL, description), Prism `ingress_request_id`, per-request `attempt_number`, best-effort `provider_correlation_id`, HTTP status, response time (ms), token usage (if available from upstream response), whether the request was streamed, and timestamp
- Request logs remain one row per upstream attempt; `ingress_request_id` groups all attempts from one incoming runtime request

#### 4.9.1 Token Usage Extraction
Token usage is extracted from upstream responses using api-family-aware parsing:
- **OpenAI (non-streaming)**: Extracts from `usage` object
- **Anthropic Messages (non-streaming)**: Extracts from `usage` object
- **Anthropic count_tokens (non-streaming)**: Extracts `input_tokens` from top-level
- **OpenAI (streaming)**: Accumulated from SSE events (requires `include_usage=true`)
- **Anthropic (streaming)**: Accumulated from SSE events (`message_start` and `message_delta`)
- **Fallback**: If token data cannot be extracted, all token fields are logged as `null`
- **Null vs zero token semantics**:
  - No upstream usage block: token fields remain `null`
  - Usage block present but special fields absent: special fields logged as `0`

#### 4.9.2 Token Costing
The gateway computes the cost of each request based on the extracted token usage and the connection's assigned pricing template.
- **Pricing Templates**: Pricing is profile-scoped and reusable. Connections reference templates via `pricing_template_id` instead of storing inline price fields.
- **Pricing behavior**: Pricing templates use five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Management writes and bundle v1 import normalize missing/null/blank pricing inputs to `"0"` before validation.
- **Semantic Note**: Explicit `"0"` means configured free pricing. `MISSING_PRICE_DATA` is reserved for absent, unusable, or invalid pricing snapshots, or missing FX data. Token costing uses canonical disjoint components: base input, cache-read input, cache-creation input, base output, and reasoning output; aggregate `cached_tokens` is derived-only for presentation.

- Statistics dashboard in the Web UI with:
  - Overview cards: total requests, average response time, success rate, total tokens used
  - Aggregate endpoint, model, and proxy-key usage views sourced from the unified usage snapshot
  - Filters: date range, model, connection, time range presets (last 1h, 24h, 7d, all)
  - Summary statistics grouped by model and api family
- Dedicated request investigation UI at `/request-logs` with server-backed coarse filters, grouped `ingress_request_id` tracking, and lazy audit lookup in a detail drawer
- REST API for querying statistics remains available for API callers and debugging:
  - List request logs with pagination and filters
  - Get aggregated statistics (counts, averages, totals) with grouping
  - Get the usage snapshot and endpoint model statistics directly when needed
- Dashboard realtime streams overview `dashboard.update` payloads over WebSocket with recent request, 24h summary/api family, 30d spending, 24h throughput, and routing snapshot data
- Dashboard Analytics uses websocket-native `analytics.snapshot` payloads scoped by `{profile_id,preset}`. Each snapshot is a full replacement that includes the usage snapshot plus endpoint model statistics keyed by endpoint ID string.

### 4.10 Request Audit Logging
Full HTTP request/response recording for proxied requests, stored in the database for auditing and debugging.

#### 4.10.1 Per-Vendor Audit Toggle
- Each vendor has `audit_enabled` and `audit_capture_bodies` flags
- Vendorless models do not synthesize audit defaults from `api_family`; they simply skip vendor-scoped audit logging
- Toggling audit on/off takes effect immediately for new requests

#### 4.10.2 What Gets Recorded
For each audited upstream attempt (including failover attempts):
- **Request**: HTTP method, full upstream URL, all headers (redacted), request body
- **Response**: HTTP status code, response headers, captured response body bytes when body capture is enabled and bytes were captured
- **Metadata**: model ID, api family, connection identity (connection ID, endpoint base URL, description), duration, stream flag, timestamp, link to corresponding `request_log` entry

#### 4.10.3 Sensitive Data Redaction
All sensitive information is redacted before storage:
- `Authorization` keeps the scheme and stores `Bearer [REDACTED]`; `x-api-key` and `x-goog-api-key` are stored as `[REDACTED]`
- Any header containing `key`, `secret`, `token`, or `auth` in its name → value replaced with `[REDACTED]`
- Redaction happens at write time — sensitive data never reaches the database

#### 4.10.4 Non-Interference
Audit logging must never affect proxy behavior:
- Recording uses a best-effort async write path
- Failures are logged to console but never propagated to the client

#### 4.10.5 Audit Inspection (Frontend)
- Audit detail is opened from the request investigation flow rather than a standalone `/audit` page
- Request-focused inspection surfaces the linked audit payload when available
- Detail view is presented in the request-log side drawer with summary and payload context

#### 4.10.6 Body Size Limits
- Request and response bodies are truncated to 64KB before storage
- A `[TRUNCATED]` marker is appended when truncation occurs

### 4.11 Batch Data Deletion
Provide flexible bulk deletion of historical logs and statistics data to manage database growth.
- Supported Data Types: `request_logs`, `audit_logs`, and `loadbalance_events`
- Deletion Modes: Preset time ranges, custom day count, or delete all
- Deleting `request_logs` does NOT delete linked `audit_logs`; `audit_logs.request_log_id` is set to `NULL`

### 4.12 Custom HTTP Headers per Terminal Target
Allow users to configure custom HTTP headers on individual Terminal Targets. These headers are appended to upstream proxy requests.
- Custom headers are configured during Terminal Target creation or editing
- Headers are stored as a JSON object
- Custom headers override any same-name header from earlier steps (client headers, upstream auth headers)

### 4.13 Supported API Families
- The application exclusively supports the shipped OpenAI, Anthropic, and Gemini `api_family` values
- Vendor records are publisher metadata, not runtime compatibility switches

### 4.14 Configurable Header Blocklist
Database-backed header blocklist with CRUD API. Supports exact and prefix match types. System defaults for Cloudflare tunnel metadata, tracing headers, and standard proxy headers. Applied by the Go runtime on every request.

### 4.15 Profile Isolation & Management
- Profiles are isolated configuration namespaces (for example A/B/C) with one globally active profile for runtime routing at any time
- Selected profile controls management/API scope; active profile controls `/v1/*` and `/v1beta/*` runtime traffic
- Management APIs require `X-Profile-Id` for profile-scoped `/api/*` routes, while global management routes (profiles, vendors, auth, realtime, auth-setting flows, and vendor-catalog config flows) stay outside selected-profile scoping; `POST /api/config/profile/import/preview` is profile-scoped and requires `X-Profile-Id`
- Profile lifecycle supports create/list/update/activate/delete where delete is soft-delete for inactive profiles (`deleted_at`)
- Active profile deletion is rejected; activation uses an optimistic CAS guard (`expected_active_profile_id`) and returns `409` on conflict
- Capacity is capped at 10 non-deleted profiles; creating an 11th profile is rejected until one profile is deleted
- Observability rows (`request_logs`, `audit_logs`) carry immutable `profile_id` attribution for historical correctness


## 5. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Deployment | Single binary/process, local or LAN |
| Authentication | Optional operator auth for `/api/*`; optional proxy API keys for `/v1/*` |
| Latency overhead | < 50ms added to proxy requests |
| Concurrent requests | Support 10+ simultaneous proxy requests |
| Database | PostgreSQL (Go-managed startup migrations) |
| API standard | Markdown API contract maintained in `docs/API_SPEC.md` |
| CORS | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Go 1.26.2, chi, pgx, gorilla/websocket |
| HTTP Client | Go `net/http` streaming transport |
| Database | PostgreSQL via pgx |
| Frontend | React 19, Vite 8, TypeScript, Tailwind CSS 4, shadcn/ui, React Router 7 |
| API Contract | `docs/API_SPEC.md` markdown reference |
| Communication | REST API with JSON, SSE for streaming proxy, WebSocket for realtime updates |

## 7. Out of Scope (v1)

- User authentication / auth-based multi-tenancy (profile namespace isolation for one operator is in scope)
- Usage-based billing/accounting integrations beyond Prism's built-in telemetry and costing reports
- Rate limiting on the proxy itself
- External secret-manager integrations beyond Prism's built-in endpoint-secret encryption at rest
