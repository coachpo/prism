# Product Requirements Document: LLM Proxy Gateway

## 1. Overview

LLM Proxy Gateway is a lightweight, self-hosted application that acts as a unified proxy for multiple LLM API providers. It allows a single user to configure, route, and load-balance requests across OpenAI, Anthropic, and Google Gemini APIs through a single endpoint with a web-based management UI.

## 2. Problem Statement

Developers and power users working with multiple LLM providers face:
- Managing multiple API keys and base URLs across different tools
- No unified endpoint for switching between providers
- No automatic failover when a provider is down or rate-limited
- Manual configuration changes when rotating keys or endpoints

## 3. Target User

Single user (developer/power user) running the application locally or on a local network. No multi-tenancy or authentication required.

## 4. Core Features

### 4.1 Multi-Provider Proxy
- Transparent proxy for OpenAI, Anthropic, and Google Gemini APIs
- Supports both streaming (SSE) and non-streaming responses
- Preserves original API request/response formats per provider
- Provider detection based on model ID configuration

### 4.2 Model Configuration
- Map any model ID to a specific provider type (openai/anthropic/gemini)
- Two model types:
  - **Native**: A real model with its own BaseURL + APIKey endpoint configurations
  - **Proxy**: An alias model that forwards all requests to a target native model (no own endpoints, no load balancing)
- Assign one or more BaseURL + APIKey combinations per native model
- Select which combination is actively used for each model
- CRUD operations for all configurations via REST API

### 4.3 Model Proxying (Alias)
- Proxy models resolve model ID suffix variations (e.g., `claude-sonnet-4-5` → `claude-sonnet-4-5-20250929`)
- Only same-provider proxying is allowed (e.g., OpenAI model → OpenAI model, not OpenAI → Anthropic)
- Proxy models cannot have their own endpoints — they use the target native model's endpoints
- A proxy model cannot point to another proxy model (must target a native model)
- Proxy models do not have load balancing — they always use the target native model's load balancing configuration
- All model IDs are globally unique regardless of model type
- Gateway transparently resolves proxy aliases: incoming request for proxy model → routed to target native model's endpoints
- For Gemini native API paths (e.g., `/v1beta/models/{model}:generateContent`), the proxy rewrites the model ID segment in the URL path to the resolved native model ID when a proxy alias is used

### 4.4 Load Balancing & Failover
- For models with multiple BaseURL/APIKey combinations:
  - **Round-robin** load balancing across active endpoints
  - **Automatic failover** on request failure (HTTP 5xx, timeout, rate limit)
- Configurable strategy per model (single, round-robin, failover)
- Proxy is fully transparent and read-only — no state mutations during request/response handling
- All failover attempts (including failed ones) are logged to `request_logs` for observability. When an endpoint returns a failover-triggering status code (429, 500, 502, 503, 529) or encounters a connection/timeout error, the failed attempt is logged before trying the next endpoint.

### 4.5 Endpoint Health Detection
- Manual health check for each endpoint, triggered by user action (no periodic checks)
- Health check sends a real chat completion request using the endpoint's configured model ID and a simple question ("hi") to validate the full request chain (URL routing, authentication, model availability)
- The request uses the same URL-building logic as the proxy engine to avoid path duplication issues
- Provider-specific request format:
  - **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `model`, `max_tokens: 1`, and a simple message
  - **Anthropic**: `POST {base_url}/messages` with `model`, `max_tokens: 1`, and a simple message
- Health status determination:
  - 2xx response → `healthy`
  - 401/403 → `unhealthy` (authentication failed)
  - 429 → `healthy` (endpoint works, just rate-limited)
  - Connection error / timeout → `unhealthy`
  - Other errors → `unhealthy`
- Health check available in:
  - Model Detail → Endpoints list → Actions menu ("Check Health")
  - Model Detail → Add/Edit Endpoint dialog ("Test Connection" button)

### 4.5.1 Endpoint Success Rate Badge
- Each endpoint displays a **success rate badge** computed from `request_logs` data
- Success rate = `COUNT(2xx status codes) / COUNT(total requests) * 100` for that endpoint
- Badge color thresholds:
  - **Green** (≥98%): Excellent health
  - **Yellow** (75%–97.99%): Degraded health
  - **Red** (<75%): Poor health
  - **Gray** (N/A): No request data available (0 total requests)
- The success rate badge replaces the previous binary health dot (green/yellow/red for healthy/unknown/unhealthy) in the endpoint list on the Model Detail page
- The manual health check still updates `health_status` and `health_detail` in the database, but the primary visual indicator is now the success rate badge
- Tooltip on hover shows: success rate percentage, total requests count, success/error counts, and last health check detail (if available)

### 4.5.2 Model Health Display
- Each model displays an aggregated health indicator on the Dashboard and Models pages
- Model health is computed by aggregating the success rates of all its active endpoints
- Model health = weighted average of endpoint success rates (weighted by request count per endpoint)
- If a model has no request data across any endpoint, it shows "N/A" (gray)
- Display format: A colored badge showing the aggregated success rate percentage
  - Same color thresholds as endpoint badges: ≥98% green, 75-98% yellow, <75% red, N/A gray
- Shown in:
  - **Dashboard** → Model Overview table → new "Health" column between "Endpoints" and "Status"
  - **Models** page → Model list table → new "Health" column between "Endpoints" and "Status"

### 4.6 Web UI (Management Dashboard)
- View all configured models and their endpoints
- Add/edit/delete model configurations (native and proxy types)
- Add/edit/delete endpoint (BaseURL + APIKey) combinations
- Toggle active/inactive endpoints per model
- Select load balancing strategy per model
- Manual health check for endpoints with visual status indicators

### 4.7 Configuration Persistence
- All configuration stored in SQLite database
- No config files to manage — everything through the UI/API
- Database auto-created on first run

### 4.8 Request Statistics & Analytics
- Automatic logging of all proxy requests with telemetry data
- Each request log captures: model ID, provider, endpoint used (ID, base URL, description), HTTP status, response time (ms), token usage (if available from upstream response), whether the request was streamed, and timestamp

#### 4.8.1 Token Usage Extraction
Token usage is extracted from upstream responses using provider-aware parsing:

- **OpenAI (non-streaming)**: Extracts from `response.usage` object (`prompt_tokens`, `completion_tokens`, `total_tokens`)
- **Anthropic Messages (non-streaming)**: Extracts from `response.usage` object (`input_tokens`, `output_tokens`); `total_tokens` is computed as `input_tokens + output_tokens`
- **Anthropic count_tokens (non-streaming)**: Extracts `input_tokens` from the top-level response (no `usage` wrapper); `output_tokens` and `total_tokens` are null
- **OpenAI (streaming)**: Accumulated from SSE events; the final chunk may contain a `usage` object if `stream_options.include_usage` was set. Otherwise, tokens are unavailable for streaming.
- **Anthropic (streaming)**: Accumulated from SSE events; `message_start` event contains `message.usage.input_tokens`, `message_delta` event contains `usage.output_tokens`. `total_tokens` is computed as their sum.
- **Fallback**: If token data cannot be extracted (unsupported format, parse error), all token fields are logged as `null`

SSE streaming responses require parsing `data: {...}` lines from the accumulated stream chunks to extract usage information from the appropriate events.
- Statistics dashboard in the Web UI with:
  - Overview cards: total requests, average response time, success rate, total tokens used
  - Filterable request log table with columns: timestamp, model, provider, endpoint (description), status, response time, tokens
  - Filters: date range, model, endpoint, time range presets (last 1h, 24h, 7d, all)
  - The "All" time range must query all historical data for both the summary cards and the request log table (no implicit 24h default)
  - Provider filter limited to supported providers only: OpenAI, Anthropic, Gemini
  - Summary statistics grouped by model and provider
- REST API for querying statistics:
  - List request logs with pagination and filters
  - Get aggregated statistics (counts, averages, totals) with grouping
- Statistics page accessible from sidebar navigation

### 4.9 Request Audit Logging

Full HTTP request/response recording for proxied requests, stored in the database for auditing and debugging.

#### 4.9.1 Per-Provider Audit Toggle
- Each provider (OpenAI, Anthropic, Gemini) has:
  - `audit_enabled` (default: off)
  - `audit_capture_bodies` (default: on)
- When enabled, every proxy request routed through that provider records the full HTTP request and response to the `audit_logs` table
- Toggle is accessible from the Settings page under "Audit Configuration"
- Toggling audit on/off takes effect immediately for new requests

#### 4.9.2 What Gets Recorded
For each audited upstream attempt (including failover attempts):
- **Request**: HTTP method, full upstream URL, all headers (redacted), request body
- **Response**: HTTP status code, response headers, response body (non-streaming only)
- **Metadata**: model ID, provider, endpoint identity (endpoint ID, base URL, description), duration, stream flag, timestamp, link to corresponding `request_log` entry

For streaming requests, the response body is not recorded (too large / unbounded). Response headers and status are still captured.

#### 4.9.3 Sensitive Data Redaction
All sensitive information is redacted before storage:
- `Authorization` header values → `Bearer [REDACTED]`
- `x-api-key` header values → `[REDACTED]`
- Any header containing `key`, `secret`, `token`, or `auth` in its name → value replaced with `[REDACTED]`
- Redaction happens at write time — sensitive data never reaches the database
- Request/response bodies are not header-redacted and may contain user-provided secrets or PII
- Body capture is configurable per provider via `audit_capture_bodies`; when disabled, `request_body` and `response_body` are stored as `NULL`

#### 4.9.4 Non-Interference
Audit logging must never affect proxy behavior:
- Recording uses a best-effort async write path (no client-visible failure propagation)
- Failures are logged to console but never propagated to the client
- No modification to the request or response pipeline
- Minimal overhead when audit is disabled for a provider (single flag check, no body/header serialization)

#### 4.9.5 Audit Page (Frontend)
- Dedicated page at `/audit` accessible from sidebar navigation
- Filterable list of audit records: provider, model, endpoint, status code, time range
- Paginated results with preview of request body (first 200 chars)
- Endpoint column showing endpoint description (or base URL fallback) for each audit record
- Detail view as a wide tabbed modal dialog with:
  - Summary strip: model, provider, endpoint (ID + description + base URL), status, duration, timestamp
  - Request tab: method, URL, headers (redacted, pretty-printed JSON), body (pretty-printed JSON)
  - Response tab: status, headers (pretty-printed JSON), body (pretty-printed JSON)
  - "Response body not recorded" notice for streaming requests
- Bulk deletion controls are provided in Settings under "Data Management"

#### 4.9.6 Body Size Limits
- Request and response bodies are truncated to 64KB before storage
- A `[TRUNCATED]` marker is appended when truncation occurs

### 4.10 Batch Data Deletion

Provide flexible bulk deletion of historical logs and statistics data to manage database growth.

#### 4.10.1 Supported Data Types
Both `request_logs` (telemetry/statistics) and `audit_logs` (request audit records) support batch deletion.

#### 4.10.2 Deletion Modes
Users can delete records using three modes:
- **Preset time ranges**: Delete records older than 7, 15, or 30 days
- **Custom day count**: Delete records older than any integer number of days (≥ 1)
- **Delete all**: Delete all records in a section

The cutoff is computed server-side from UTC app time as `current_utc - N days`. All records with `created_at` before the cutoff are deleted.

#### 4.10.3 Frontend UI
Batch deletion controls are placed on the Settings page under a "Data Management" section:
- Single action builder pattern: select data type (Request Logs / Audit Logs) → select action (preset: older than 7/15/30 days, custom days, or delete all) → click "Delete"
- Each action shows a confirmation dialog before executing with context-appropriate wording
- After deletion, a toast shows the count of deleted records
- All delete actions are disabled while a deletion is in progress
- The Statistics page and Audit page data reflect the deletion immediately on next load

#### 4.10.4 API
- `DELETE /api/stats/requests` with `older_than_days` (integer ≥ 1) or `delete_all=true` — exactly one mode required
- `DELETE /api/audit/logs` with `before` (datetime), `older_than_days` (integer ≥ 1), or `delete_all=true` — exactly one mode required

#### 4.10.5 Behavior
- Deletion is irreversible — no soft delete or recycle bin
- Deletion runs synchronously within a single transaction
- Deleting `request_logs` does NOT delete linked `audit_logs`; `audit_logs.request_log_id` is set to `NULL` via FK `ON DELETE SET NULL`
- Deleting `audit_logs` does NOT affect `request_logs`
- Optional maintenance: a manual `VACUUM` operation may be run to reclaim SQLite file space after large deletions

### 4.11 Custom HTTP Headers per Endpoint

Allow users to configure custom HTTP headers on individual endpoints. These headers are appended to upstream proxy requests, enabling per-endpoint customization (e.g., custom routing headers, organization IDs, feature flags).

#### 4.11.1 Configuration
- Each endpoint can optionally have a set of custom HTTP headers (key-value string pairs)
- Custom headers are configured during endpoint creation or editing via the existing endpoint CRUD UI
- Headers are stored as a JSON object (e.g., `{"X-Custom-Org": "org-123", "X-Priority": "high"}`)
- Empty or null custom_headers means no custom headers are applied

#### 4.11.2 Header Merge Order
When building upstream request headers, the merge order is:
1. **Client headers** (from the incoming request, minus hop-by-hop and client auth headers)
2. **Provider auth headers** (e.g., `Authorization: Bearer {key}`, `x-api-key: {key}`)
3. **Provider extra headers** (e.g., `anthropic-version: 2023-06-01`)
4. **Custom endpoint headers** (from `endpoints.custom_headers`) — applied LAST

Custom headers override any same-name header from earlier steps. This is intentional — the user's explicit configuration takes precedence.

#### 4.11.3 Validation
- Header names must be non-empty strings
- Header values must be strings
- No limit on the number of custom headers (practical limit: JSON column size)
- Reserved header names are NOT blocked — users can override `Authorization`, `Content-Type`, etc. at their own risk. This is a power-user feature for a trusted local deployment.

#### 4.11.4 UI
- The Add/Edit Endpoint dialog includes a "Custom Headers" section
- Key-value pair input with add/remove controls
- Empty by default (no custom headers)
- Headers are displayed in the endpoint detail view

### 4.12 Supported Providers
- The application exclusively supports three LLM providers: **OpenAI**, **Anthropic**, and **Google Gemini**
- All UI dropdowns, filters, and selectors only show these three providers
- No other providers (e.g., Ollama, vLLM) are available in any part of the application

## 5. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Deployment | Single binary/process, local or LAN |
| Authentication | None (single-user, trusted network) |
| Latency overhead | < 50ms added to proxy requests |
| Concurrent requests | Support 10+ simultaneous proxy requests |
| Database | SQLite (file-based, zero config) |
| API standard | OpenAPI 3.0 spec auto-generated |
| CORS | Wildcard allowed (`*`) |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Python 3.11+, FastAPI, SQLAlchemy (async), aiosqlite |
| HTTP Client | httpx (async, streaming) |
| Database | SQLite via aiosqlite |
| Frontend | React 18+, Vite, TypeScript, Tailwind CSS, shadcn/ui |
| API Contract | OpenAPI 3.0 (auto-generated by FastAPI) |
| Communication | REST API with JSON, SSE for streaming proxy |

## 7. Out of Scope (v1)

- User authentication / multi-tenancy
- Token usage tracking and billing
- Rate limiting on the proxy itself
- API key encryption at rest
- Docker packaging (future enhancement)
