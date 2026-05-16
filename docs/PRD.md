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
- Transparent proxy for OpenAI, Anthropic, and Gemini path families
- Supports both streaming (SSE) and non-streaming responses
- Preserves native request/response formats per API family
- Runtime compatibility is fixed by `api_family`

### 4.2 Model Configuration
- Map each model to optional `vendor_id` metadata plus a fixed runtime `api_family`
- Two model types:
  - **Native**: A real model with its own routing and costing configurations (connections)
  - **Proxy**: A selector-driven routing model that selects one native target per request (no own connections, no own loadbalance strategy)
- Assign one or more connections per native model
- Select which connections are actively used for each model
- CRUD operations for all configurations via REST API

### 4.3 Proxy Model Routing
- Proxy models own `proxy_selection_strategy` plus a `proxy_targets` list instead of a singular redirect target
- Supported selectors are `ordered_fallback`, `weighted_static`, and `priority_static`
- Each proxy target carries `target_model_id`, contiguous zero-based `position`, `weight >= 1`, and `target_priority >= 0`
- Only same-`api_family` proxying is allowed
- Proxy models cannot have their own connections; they use the chosen native target model's connections
- A proxy model cannot target another proxy model (must target a native model)
- Proxy models do not have their own load balancing strategy; they always use the chosen native target model's load balancing configuration
- Model IDs are unique within a profile; the same model ID can exist in different profiles without collision
- Gateway resolves the selector before native connection planning: incoming request for proxy model -> selected native target -> chosen native model's connections handle the request
- For Gemini native API paths (e.g., `/v1beta/models/{model}:generateContent`), the proxy rewrites the model ID segment in the URL path to the resolved native target model ID when proxy routing selects a different upstream model ID
- Gemini streaming is path-native: `/v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`
- Once one target is selected, retries stay inside that target model's native connection plan and do not jump to another proxy target in the same request.

### 4.4 Load Balancing & Failover
- For models with multiple connections:
  - **Automatic failover** when an attempt returns a failover-triggering status (`403`, `429`, `500`, `502`, `503`, `529`) or raises connection/timeout errors
  - Native models attach one reusable loadbalance strategy chosen from two first-class families: `legacy` or `adaptive`
  - Upstream request timing uses shared backend timeout settings; legacy strategies use `legacy_strategy_type` (`single`, `fill-first`, or `round-robin`) plus `auto_recovery`, while adaptive strategies use `routing_policy`
- Failover-worthy HTTP responses are governed by the attached strategy's configured recovery policy: legacy strategies use `auto_recovery.status_codes`, while adaptive strategies use `routing_policy.circuit_breaker.failure_status_codes`
  - Non-failover client errors (for example `400`, `404`, `422`) do not force-clear existing recovery state
  - Each connection can optionally define `qps_limit`, `max_in_flight_non_stream`, and `max_in_flight_stream`; `null` means unlimited
  - Limiter state is persisted in PostgreSQL `UNLOGGED` tables and is intentionally ephemeral after crash or unclean shutdown
- Proxy request forwarding may apply compatibility normalizations while preserving API-family-native response formats
- All failover attempts (including failed ones) are logged to `request_logs` for observability. When a connection returns a failover-triggering status code (`403`, `429`, `500`, `502`, `503`, `529`) or encounters a connection/timeout error, the failed attempt is logged before trying the next connection.
- Failover, recovery, and probe-eligibility transitions are persisted as `loadbalance_events` for audit and observability.
### 4.5 Profile-Scoped Endpoints & Model Connections
- **Vendors** remain global publisher metadata shared across profiles.
- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Models** carry optional `vendor_id` metadata plus fixed `api_family` metadata.
- **Connections** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile.
- Endpoints can be reused across multiple models within the same profile.
- Deleting an endpoint is blocked if any connections in that profile still reference it.

### 4.6 Connection Health Detection
- Manual health check remains available for each connection from the management UI.
- Health probes send a minimal request using the connection's configured model ID and the same URL-building logic as the proxy engine to validate URL routing, authentication, and model availability end to end.
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
  - Model Detail → Connections list → Actions menu ("Check Health")
  - Model Detail → Add/Edit Connection dialog ("Test Connection" button)

### 4.6.1 Connection Success Rate Badge
- Each connection displays a **success rate badge** computed from `request_logs` data
- Success rate = `COUNT(2xx status codes) / COUNT(total requests) * 100` for that connection
- Badge color thresholds:
  - **Green** (≥98%): Excellent health
  - **Yellow** (75%–97.99%): Degraded health
  - **Red** (<75%): Poor health
  - **Gray** (N/A): No request data available (0 total requests)
- The success rate badge is the primary visual indicator in the connection list on the Model Detail page
- The manual health check still updates `health_status` and `health_detail` in the database
- Tooltip on hover shows: success rate percentage, total requests count, success/error counts, and last health check detail (if available)

### 4.6.2 Model Health Display
- Each model displays an aggregated health indicator on the Dashboard and Models pages
- Model health is computed by aggregating the success rates of all its active connections
- Model health = weighted average of connection success rates (weighted by request count per connection)
- If a model has no request data across any connection, it shows "N/A" (gray)
- Display format: A colored badge showing the aggregated success rate percentage
  - Same color thresholds as connection badges: ≥98% green, 75-98% yellow, <75% red, N/A gray
- Shown in:
  - **Dashboard** → Model Overview table → "Success Rate" column
  - **Models** page → Model list table → "Success Rate" column

### 4.7 Web UI (Management Dashboard)
- View all configured models and their connections
- Add/edit/delete model configurations (native and proxy types)
- Add/edit/delete profile-scoped endpoints
- Add/edit/delete model connections
- Toggle active/inactive connections per model
- Select either a legacy or adaptive routing strategy per native model
- Manual health check for connections with visual status indicators
- Dedicated model-detail routes (`/models/:id` and `/models/:id/proxy`) with manual health checks, connection KPIs, current loadbalance state, and loadbalance event history
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
- Config export/import uses the Go-era split-bundle contract: profile bundles are `version: 1` with `bundle_kind: profile_config`, and vendor catalog bundles are `version: 1` with `bundle_kind: vendor_catalog`
- Profile bundles carry `vendor_refs`, `profile_settings`, nullable `api_key_secret_ref`, encrypted `secret_payload`, top-level `loadbalance_strategies`, proxy `proxy_selection_strategy`, explicit `proxy_targets`, nullable `vendor_key`, and `api_family`
- Profile import preview validates bundle kind, version, secret decryption, and vendor resolution before replace-mode import; unsupported versions are rejected
- Database setup is managed by the Go backend runtime and applies the checked-in migration chain on startup
### 4.9 Request Statistics & Analytics
- Automatic logging of all proxy requests with telemetry data
- Each request log captures: profile ID attribution, requested `model_id`, `resolved_target_model_id` (when proxy routing selected a native target), `api_family`, connection used (ID, endpoint base URL, description), Prism `ingress_request_id`, per-request `attempt_number`, best-effort `provider_correlation_id`, HTTP status, response time (ms), token usage (if available from upstream response), whether the request was streamed, and timestamp
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
- **Pricing behavior**: Pricing must be explicit for each token type.
- **Semantic Note**: Costing uses only the explicit prices stored on the template; it does not invent a missing-token path.

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

### 4.12 Custom HTTP Headers per Connection
Allow users to configure custom HTTP headers on individual connections. These headers are appended to upstream proxy requests.
- Custom headers are configured during connection creation or editing
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
| API standard | OpenAPI 3.1 contract served from the checked-in artifact |
| CORS | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Go 1.26.2, chi, pgx, gorilla/websocket |
| HTTP Client | Go `net/http` streaming transport |
| Database | PostgreSQL via pgx |
| Frontend | React 19, Vite 8, TypeScript, Tailwind CSS 4, shadcn/ui, React Router 7 |
| API Contract | OpenAPI 3.1 served from checked-in `docs/openapi.json` |
| Communication | REST API with JSON, SSE for streaming proxy, WebSocket for realtime updates |

## 7. Out of Scope (v1)

- User authentication / auth-based multi-tenancy (profile namespace isolation for one operator is in scope)
- Usage-based billing/accounting integrations beyond Prism's built-in telemetry and costing reports
- Rate limiting on the proxy itself
- External secret-manager integrations beyond Prism's built-in endpoint-secret encryption at rest
