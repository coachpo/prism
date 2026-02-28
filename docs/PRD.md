# Product Requirements Document: Prism

## 1. Product Overview

Prism is a lightweight, self-hosted application that acts as a unified proxy for multiple LLM API providers. It allows a single user to configure, route, and load-balance requests across OpenAI, Anthropic, and Gemini APIs through a single endpoint with a web-based management UI.

## 2. Problem Statement

Developers and power users working with multiple LLM providers face:
- Managing multiple API keys and base URLs across different tools
- No unified endpoint for switching between providers
- No automatic failover when a provider is down or rate-limited
- Manual configuration changes when rotating keys or endpoints

## 3. Target User

Single operator (developer/power user) running the application locally or on a local network. No authentication required. Prism supports profile-based configuration isolation for one operator (selected profile vs active profile); this is not auth multi-tenancy.

## 4. Core Features

### 4.1 Multi-Provider Proxy
- Transparent proxy for OpenAI, Anthropic, and Gemini APIs
- Supports both streaming (SSE) and non-streaming responses
- Preserves original API request/response formats per provider
- Provider detection based on model ID configuration

### 4.2 Model Configuration
- Map any model ID to a specific provider type (openai/anthropic/gemini)
- Two model types:
  - **Native**: A real model with its own routing and costing configurations (connections)
  - **Proxy**: An alias model that forwards all requests to a target native model (no own connections, no load balancing)
- Assign one or more connections per native model
- Select which connections are actively used for each model
- CRUD operations for all configurations via REST API

### 4.3 Model Proxying (Alias)
- Proxy models resolve model ID suffix variations (e.g., `claude-sonnet-4-5` → `claude-sonnet-4-5-20250929`)
- Only same-provider proxying is allowed (e.g., OpenAI model → OpenAI model, not OpenAI → Anthropic)
- Proxy models cannot have their own connections — they use the target native model's connections
- A proxy model cannot point to another proxy model (must target a native model)
- Proxy models do not have load balancing — they always use the target native model's load balancing configuration
- Model IDs are unique within a profile; the same model ID can exist in different profiles without collision
- Gateway transparently resolves proxy aliases: incoming request for proxy model → routed to target native model's connections
- For Gemini native API paths (e.g., `/v1beta/models/{model}:generateContent`), the proxy rewrites the model ID segment in the URL path to the resolved native model ID when a proxy alias is used

### 4.4 Load Balancing & Failover
- For models with multiple connections:
  - **Automatic failover** on request failure (HTTP 5xx, timeout, rate limit)
- Configurable strategy per model (single, failover)
- Proxy is fully transparent and read-only — no state mutations during request/response handling
- All failover attempts (including failed ones) are logged to `request_logs` for observability. When a connection returns a failover-triggering status code (429, 500, 502, 503, 529) or encounters a connection/timeout error, the failed attempt is logged before trying the next connection.

### 4.5 Profile-Scoped Endpoints & Model Connections
- **Providers** remain global seed records shared across profiles.
- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Connections** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile.
- Endpoints can be reused across multiple models within the same profile.
- Deleting an endpoint is blocked if any connections in that profile still reference it.

### 4.6 Connection Health Detection
- Manual health check for each connection, triggered by user action (no periodic checks)
- Health check sends a real chat completion request using the connection's configured model ID and a simple question ("hi") to validate the full request chain (URL routing, authentication, model availability)
- The request uses the same URL-building logic as the proxy engine to avoid path duplication issues
- Provider-specific request format:
  - **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `model`, `max_tokens: 1`, and a simple message
  - **Anthropic**: `POST {base_url}/messages` with `model`, `max_tokens: 1`, and a simple message
- Health status determination:
  - 2xx response → `healthy`
  - 401/403 → `unhealthy` (authentication failed)
  - 429 → `healthy` (connection works, just rate-limited)
  - Connection error / timeout → `unhealthy`
  - Other errors → `unhealthy`
- Health check available in:
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
- Select load balancing strategy per model
- Manual health check for connections with visual status indicators
- Global profile selector in the app shell controls the selected profile (management scope).
- Active profile indicator is shown globally; runtime activation is an explicit action.
- Profile create/edit/delete dialogs include active-profile delete guardrails and capacity guidance.

### 4.8 Configuration Persistence
- Configuration is stored in PostgreSQL with Alembic-managed schema migrations applied at startup
- No config files to manage — everything through the UI/API
- Existing installs are backfilled into a default profile during profile-isolation migration
- Config export uses version 7 (profile-aware, ID-agnostic logical references)
- Config import accepts v6 and v7; v6 numeric references are compatibility-remapped, and v7 is canonical
### 4.9 Request Statistics & Analytics
- Automatic logging of all proxy requests with telemetry data
- Each request log captures: profile ID attribution, model ID, provider, connection used (ID, endpoint base URL, description), HTTP status, response time (ms), token usage (if available from upstream response), whether the request was streamed, and timestamp

#### 4.9.1 Token Usage Extraction
Token usage is extracted from upstream responses using provider-aware parsing:
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
The gateway computes the cost of each request based on the extracted token usage and the connection's pricing configuration.
- **Pricing Fields**: Each connection can be configured with prices for input, output, cached input (read), cache creation (write), and reasoning tokens.
- **Fallback Policy**: The `missing_special_token_price_policy` determines how to handle costs when a specific special token price is missing.
  - `MAP_TO_OUTPUT`: Use the output token price as a fallback.
  - `ZERO_COST`: Treat missing special token prices as zero.
- **Semantic Note**: The fallback policy affects only the price used for cost calculation. It does not affect the token counts themselves.

- Statistics dashboard in the Web UI with:
  - Overview cards: total requests, average response time, success rate, total tokens used
  - Filterable request log table with columns: timestamp, model, provider, connection (description), status, response time, tokens
  - Filters: date range, model, connection, time range presets (last 1h, 24h, 7d, all)
  - Summary statistics grouped by model and provider
- REST API for querying statistics:
  - List request logs with pagination and filters
  - Get aggregated statistics (counts, averages, totals) with grouping

### 4.10 Request Audit Logging
Full HTTP request/response recording for proxied requests, stored in the database for auditing and debugging.

#### 4.10.1 Per-Provider Audit Toggle
- Each provider has `audit_enabled` and `audit_capture_bodies` flags
- Toggling audit on/off takes effect immediately for new requests

#### 4.10.2 What Gets Recorded
For each audited upstream attempt (including failover attempts):
- **Request**: HTTP method, full upstream URL, all headers (redacted), request body
- **Response**: HTTP status code, response headers, response body (non-streaming only)
- **Metadata**: model ID, provider, connection identity (connection ID, endpoint base URL, description), duration, stream flag, timestamp, link to corresponding `request_log` entry

#### 4.10.3 Sensitive Data Redaction
All sensitive information is redacted before storage:
- `Authorization`, `x-api-key`, `x-goog-api-key` header values → `[REDACTED]`
- Any header containing `key`, `secret`, `token`, or `auth` in its name → value replaced with `[REDACTED]`
- Redaction happens at write time — sensitive data never reaches the database

#### 4.10.4 Non-Interference
Audit logging must never affect proxy behavior:
- Recording uses a best-effort async write path
- Failures are logged to console but never propagated to the client

#### 4.10.5 Audit Page (Frontend)
- Dedicated page at `/audit`
- Filterable list of audit records: provider, model, connection, status code, time range
- Detail view as a wide tabbed modal dialog with summary strip and request/response tabs

#### 4.10.6 Body Size Limits
- Request and response bodies are truncated to 64KB before storage
- A `[TRUNCATED]` marker is appended when truncation occurs

### 4.11 Batch Data Deletion
Provide flexible bulk deletion of historical logs and statistics data to manage database growth.
- Supported Data Types: `request_logs` and `audit_logs`
- Deletion Modes: Preset time ranges, custom day count, or delete all
- Deleting `request_logs` does NOT delete linked `audit_logs`; `audit_logs.request_log_id` is set to `NULL`

### 4.12 Custom HTTP Headers per Connection
Allow users to configure custom HTTP headers on individual connections. These headers are appended to upstream proxy requests.
- Custom headers are configured during connection creation or editing
- Headers are stored as a JSON object
- Custom headers override any same-name header from earlier steps (client headers, provider auth headers)

### 4.13 Supported Providers
- The application exclusively supports three LLM providers: **OpenAI**, **Anthropic**, and **Gemini**
- No other providers are available in any part of the application

### 4.14 Configurable Header Blocklist
Database-backed header blocklist with CRUD API. Supports exact and prefix match types. System defaults for Cloudflare tunnel metadata, tracing headers, and standard proxy headers. Applied in `proxy_service.py` on every request.

### 4.15 Profile Isolation & Management
- Profiles are isolated configuration namespaces (for example A/B/C) with one globally active profile for runtime routing at any time
- Selected profile controls management/API scope; active profile controls `/v1/*` and `/v1beta/*` runtime traffic
- Management APIs support optional `X-Profile-Id`; if omitted, scope defaults to the active profile
- Profile lifecycle supports create/list/update/activate/delete where delete is soft-delete for inactive profiles (`deleted_at`)
- Active profile deletion is rejected; activation uses optimistic CAS guard (`expected_active_profile_id`, `expected_active_profile_version`) and returns `409` on conflict
- Capacity is capped at 10 non-deleted profiles; creating an 11th profile is rejected until one profile is deleted
- Observability rows (`request_logs`, `audit_logs`) carry immutable `profile_id` attribution for historical correctness


## 5. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Deployment | Single binary/process, local or LAN |
| Authentication | None (single-user, trusted network) |
| Latency overhead | < 50ms added to proxy requests |
| Concurrent requests | Support 10+ simultaneous proxy requests |
| Database | PostgreSQL (Alembic-managed schema, startup migrations) |
| API standard | OpenAPI 3.0 spec auto-generated |
| CORS | Wildcard allowed (`*`) |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Python 3.11+, FastAPI, SQLAlchemy (async), asyncpg |
| HTTP Client | httpx (async, streaming) |
| Database | PostgreSQL via asyncpg |
| Frontend | React 19, Vite, TypeScript, Tailwind CSS, shadcn/ui |
| API Contract | OpenAPI 3.0 (auto-generated by FastAPI) |
| Communication | REST API with JSON, SSE for streaming proxy |

## 7. Out of Scope (v1)

- User authentication / auth-based multi-tenancy (profile namespace isolation for one operator is in scope)
- Token usage tracking and billing
- Rate limiting on the proxy itself
- API key encryption at rest


## 8. Revision Traceability (Profile Isolation, 2026-02-28)

Source inputs: `docs/PROFILE_ISOLATION_REQUIREMENTS.md`, `docs/PROFILE_ISOLATION_UPGRADE_PLAN.md`, `docs/PROFILE_ISOLATION_FRONTEND_ITERATION_PLAN.md`, `docs/PROFILE_ISOLATION_RESEARCH_REFERENCES.md`, and `docs/PROFILE_ISOLATION_SUPPORTING_EVIDENCE.md`.


This appendix records how the profile-isolation requirement package is represented in implemented behavior and product documentation updates.

- Backend reference (`c0f2daa`, `feat: add profile-scoped routing and config isolation`): runtime routing is active-profile-only; management scope is effective profile (`X-Profile-Id` or active fallback); profile lifecycle includes CAS activation and inactive-only soft delete; config import/export supports v7 logical references with v6 compatibility remap.
- Frontend reference (`02c70ce`, `feat: add profile context and profile-aware dashboard flows`): selected profile drives management scope, active profile remains explicit runtime state, global shell exposes selector plus activation affordance, and profile revision triggers scoped page refetch.
- Root/docs reference (`f6f0106`, `docs: update architecture docs and bootstrap script`): documentation set and startup/bootstrap narrative were aligned to profile-isolation architecture and migration-aware initialization.

Requirement coverage anchors:

- `FR-001` / `FR-004`: profile lifecycle, single active profile, CAS-safe activation, and active-delete rejection.
- `FR-002` / `FR-003`: profile-scoped config entities and active-profile runtime routing isolation.
- `FR-006`: management API effective-scope semantics with explicit override header.
- `FR-007`: profile-targeted config replace behavior and v7 canonical format with logical refs.
- `FR-008` / `FR-009`: profile-scoped costing/settings and immutable profile attribution in observability.
- `FR-010`: selected-profile versus active-profile UX with explicit activation action.

These revisions preserve single-operator product intent and do not introduce authentication multi-tenancy.