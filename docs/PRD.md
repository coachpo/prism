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

### 4.4 Load Balancing & Failover
- For models with multiple BaseURL/APIKey combinations:
  - **Round-robin** load balancing across active endpoints
  - **Automatic failover** on request failure (HTTP 5xx, timeout, rate limit)
- Configurable strategy per model (single, round-robin, failover)
- Proxy is fully transparent and read-only — no state mutations during request/response handling

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
- Each request log captures: model ID, provider, endpoint used, HTTP status, response time (ms), token usage (if available from upstream response), whether the request was streamed, and timestamp

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
  - Filterable request log table with columns: timestamp, model, provider, endpoint, status, response time, tokens
  - Filters: date range, model, time range presets (last 1h, 24h, 7d, 30d)
  - Provider filter limited to supported providers only: OpenAI, Anthropic, Gemini
  - Summary statistics grouped by model and provider
- REST API for querying statistics:
  - List request logs with pagination and filters
  - Get aggregated statistics (counts, averages, totals) with grouping
- Statistics page accessible from sidebar navigation

### 4.9 Supported Providers
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
