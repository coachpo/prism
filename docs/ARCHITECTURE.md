# Architecture Document: Prism

## 1. System Overview

```
┌─────────────┐     ┌──────────────────────────────────────────┐     ┌──────────────┐
│   Client    │     │                 Prism                    │     │   Providers  │
│             │     │  ┌──────────┐  ┌──────────┐             │     │              │
│  Port 5173  │◀────│  │ Config  │  │  Proxy   │          │◀────│  Gemini API  │
│             │     │  │  API    │  │  Engine  │          │     │              │
└─────────────┘     │  └────┬────┘  └────┬─────┘          │     └──────────────┘
                    │       │            │                 │
                    │  ┌────▼────────────▼─────┐          │
                    │  │     SQLite Database    │          │
                    │  │  (models, endpoints,   │          │
                    │  │   lb_config,           │          │
                    │  │   request_logs,        │          │
                    │  │   audit_logs)          │          │
                    │  └───────────────────────┘          │
                    │              Port 8000               │
                    └──────────────────────────────────────┘
```

## 2. Component Architecture

### 2.1 Backend (FastAPI)

```
backend/
├── app/
│   ├── main.py                 # App factory, lifespan, CORS, router mounting
│   ├── core/
│   │   ├── config.py           # App settings (pydantic-settings)
│   │   └── database.py         # Async engine, session factory, Base
│   ├── models/                 # SQLAlchemy ORM models
│   │   ├── provider.py         # Provider model (openai, anthropic, gemini)
│   │   ├── model_config.py     # Model ID → provider mapping
│   │   ├── endpoint.py         # BaseURL + APIKey entries
│   │   └── request_log.py      # Request telemetry log entries
│   ├── schemas/                # Pydantic request/response schemas
│   │   ├── provider.py
│   │   ├── model_config.py
│   │   ├── endpoint.py
│   │   └── stats.py            # Statistics query/response schemas
│   ├── routers/                # API route handlers
│   │   ├── providers.py        # CRUD for provider types
│   │   ├── models.py           # CRUD for model configurations
│   │   ├── endpoints.py        # CRUD for BaseURL/APIKey combos
│   │   ├── proxy.py            # LLM proxy endpoints
│   │   ├── stats.py            # Statistics query endpoints
│   │   ├── audit.py            # Audit log query/delete endpoints
│   │   └── config.py           # Config export/import + header blocklist CRUD
│   ├── services/               # Business logic
│   │   ├── proxy_service.py    # Request forwarding, streaming, header sanitization
│   │   ├── loadbalancer.py     # LB strategy, failover
│   │   ├── stats_service.py    # Request logging, aggregation queries
│   │   ├── audit_service.py    # Audit recording, redaction
│   │   └── costing_service.py  # Token costing, FX conversion, pricing snapshots
│   └── dependencies.py         # Shared FastAPI dependencies
├── requirements.txt
└── alembic/ (future)
```

### 2.2 Frontend (React + Vite)

```
frontend/
├── src/
│   ├── App.tsx                 # Root with router
│   ├── main.tsx                # Entry point
│   ├── lib/
│   │   ├── api.ts              # API client (fetch wrapper)
│   │   └── utils.ts            # Utility functions
│   ├── components/
│   │   └── ui/                 # shadcn/ui components
│   ├── pages/
│   │   ├── Dashboard.tsx       # Overview of all models
│   │   ├── ModelConfig.tsx     # Model configuration page
│   │   ├── EndpointConfig.tsx  # Endpoint management
│   │   ├── StatisticsPage.tsx  # Request statistics & analytics (endpoint column + filter)
│   │   ├── AuditPage.tsx       # Audit log browsing, endpoint filter, wide tabbed detail dialog
│   │   └── SettingsPage.tsx    # Audit config, config backup, data management
│   └── types/
│       └── api.ts              # TypeScript types matching backend schemas
├── components.json             # shadcn config
├── package.json
├── vite.config.ts
├── tsconfig.json
└── tailwind.config.ts
```

## 3. Request Flow

### 3.1 Proxy Request (Non-Streaming, Native Model)

```
Client → POST /v1/chat/completions {model: "gpt-4o"}
  → Router extracts model ID from body
  → LoadBalancer looks up model config
  → Model is native → select endpoint directly
  → ProxyService forwards request to upstream BaseURL
  → Upstream responds with JSON
  → Gateway returns JSON to client
```

### 3.2 Proxy Request (Proxy/Alias Model)

```
Client → POST /v1/messages {model: "claude-sonnet-4-5"}
  → Router extracts model ID from body
  → LoadBalancer looks up model config
  → Model is proxy → resolve redirect_to → "claude-sonnet-4-5-20250929"
  → Look up target native model config
  → Select endpoint from target model
  → ProxyService forwards request to upstream BaseURL (request body unchanged)
  → Upstream responds
  → Gateway returns response to client
```

### 3.3 Proxy Request (Streaming)

```
Client → POST /v1/chat/completions {model: "gpt-4o", stream: true}
  → Router extracts model ID
  → LoadBalancer selects endpoint (with proxy alias resolution if needed)
  → ProxyService opens streaming connection to upstream
  → SSE chunks piped directly to client via StreamingResponse
  → On upstream error: failover to next endpoint (if configured)
```

### 3.4 Provider-Specific Routing

| Provider               | Proxy Path                                    | Upstream Path                                      | Auth Header                                          |
| ---------------------- | --------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                 | `POST /v1/chat/completions`                   | `{base_url}/v1/chat/completions`                   | `Authorization: Bearer {key}`                        |
| Anthropic              | `POST /v1/messages`                           | `{base_url}/v1/messages`                           | `x-api-key: {key}` + `anthropic-version: 2023-06-01` |
| Gemini (OpenAI-compat) | `POST /v1/chat/completions`                   | `{base_url}/v1/chat/completions`                   | `Authorization: Bearer {key}`                        |
| Gemini (native)        | `POST /v1beta/models/{model}:generateContent` | `{base_url}/v1beta/models/{model}:generateContent` | `x-goog-api-key: {key}`                              |

Note: Gemini's OpenAI-compatible endpoint is used by default. For Gemini native API paths (e.g., `/v1beta/models/{model}:generateContent`), the proxy rewrites the model ID segment in the URL path to the resolved native model ID when a proxy alias is used. This ensures the upstream URL references the correct model even when the client sends a request using the alias model ID.

### 3.5 Custom Header Injection

When an endpoint has `custom_headers` configured, they are injected into the upstream request after all other headers:

```
build_upstream_headers():
  1. Start with client headers (minus hop-by-hop, minus client auth headers)
  2. Add provider auth headers (Authorization/x-api-key based on provider type)
  3. Add provider extra headers (e.g., anthropic-version)
  4. Apply endpoint custom_headers (from endpoints.custom_headers JSON)
     → Same-name headers from earlier steps are OVERWRITTEN
  5. Apply Header Blocklist (`sanitize_headers`):
     → Remove any headers matching active exact or prefix rules in `header_blocklist_rules`
     → This ensures blocked headers (like Cloudflare metadata) never reach the upstream
  6. Return final header dict

Custom headers are a power-user feature. While they can override most headers, they cannot be used to re-add headers that are blocked by the Header Blocklist. This is enforced by applying the blocklist last in the header construction pipeline.

## 4. Load Balancing
### 4.1 Strategies

- **single**: Use only the highest-priority active endpoint (no failover)
- **failover**: Try endpoints in priority order with automatic recovery

### 4.2 Failover with Passive Recovery

The `failover` strategy implements intelligent endpoint selection with automatic recovery:

**Endpoint Selection:**

1. Healthy endpoints (not in cooldown) are tried first in priority order
2. Cooldown-expired endpoints become probe-eligible (half-open state)
3. Endpoints still cooling down are skipped entirely

**Failure Detection:**

Failures that trigger failover and start cooldown:

- HTTP 403 (forbidden)
- HTTP 429 (rate limited)
- HTTP 500, 502, 503, 529 (server errors)
- Connection timeout (> 10s connect, > 120s read)
- Connection refused / DNS failure

**Recovery Mechanism:**

- When an endpoint fails, it enters cooldown for `failover_recovery_cooldown_seconds` (default: 60s)
- After cooldown expires, the endpoint becomes probe-eligible
- On first successful response, the endpoint is marked recovered and returns to healthy pool
- Recovery is passive (no background polling) - probes happen during normal request flow

**Per-Model Configuration:**

- `failover_recovery_enabled`: Enable/disable automatic recovery (default: true)
- `failover_recovery_cooldown_seconds`: Cooldown duration in seconds (range: 1-3600, default: 60)

If all endpoints are in cooldown with none probe-eligible, the gateway returns `503` with cooldown detail.

## 5. Model Proxy (Alias)

### 5.1 Concept

Proxy models are aliases that forward requests to a target native model. This resolves model ID suffix variations (e.g., `claude-sonnet-4-5` → `claude-sonnet-4-5-20250929`).

### 5.2 Rules

- Only same-provider proxying (OpenAI → OpenAI, Anthropic → Anthropic)
- Target must be a native model (no chained proxy aliases)
- Proxy models have no endpoints of their own
- Proxy models do not use load balancing (lb_strategy is ignored; target model's strategy applies)
- All model IDs are globally unique
- The gateway does NOT modify the request body — it only uses the target model's endpoints for routing

### 5.3 Resolution

```
resolve_model(model_id):
  config = lookup(model_id)
  if config.model_type == "proxy":
    return lookup(config.redirect_to)
  return config
```

## 6. Endpoint Health Detection

### 6.1 Concept

Manual health checks allow users to verify endpoint connectivity and authentication before relying on them for proxy traffic.

### 6.2 Health Probes (Provider-Specific)

Health checks send a real chat completion request using the endpoint's configured model ID and a simple question. This validates the full request chain (URL routing, authentication, model availability) using the same URL-building logic as the proxy engine.

- **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
- **Anthropic**: `POST {base_url}/messages` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`

### 6.3 Status Values

- `unknown` — Never checked (default)
- `healthy` — Last check succeeded (2xx or 429)
- `unhealthy` — Last check failed (401/403, connection error, timeout, other errors)

### 6.4 Endpoint Success Rate Badge

The primary visual health indicator for endpoints is the **success rate badge**, computed from `request_logs` data (not from the manual health check status).

- Success rate = `COUNT(2xx) / COUNT(*) * 100` per endpoint
- Badge colors: ≥98% green, 75-98% yellow, <75% red, N/A gray (no data)
- Displayed in the endpoint list on the Model Detail page, replacing the previous binary health dot
- The manual health check still updates `health_status`/`health_detail` in the database and is shown in the tooltip

### 6.5 Model Health Aggregation

Model-level health is computed by aggregating endpoint success rates:

- Weighted average across all endpoints: `SUM(success_count) / SUM(total_requests) * 100`
- Displayed on Dashboard and Models pages as a colored badge
- Same color thresholds as endpoint badges

### 6.4 Error Reporting

When a health check fails, the upstream error message is extracted from the response body and stored in `health_detail`. This provides actionable diagnostics (e.g., "HTTP 503: No available channel for model X" instead of just "HTTP 503"). The detail is shown in the frontend tooltip on hover.

### 6.5 URL Path Failsafe

To prevent the `/v1/v1` double-path bug (where `base_url` already contains `/v1` and the request path also starts with `/v1`):

1. **Runtime auto-correction**: `build_upstream_url()` detects repeated version segments (e.g., `/v1/v1`, `/v2/v2`) via regex and auto-corrects them, logging a warning.
2. **Input validation**: `validate_base_url()` rejects base URLs that already contain double version segments on endpoint create/update (HTTP 422).
3. **Normalization**: `normalize_base_url()` strips trailing slashes from base URLs on create/update to ensure consistent path joining.

## 7. Request Statistics

### 7.1 Concept

All proxy requests are automatically logged with telemetry data for analytics and debugging.

### 7.2 Logging Flow

```
Client → Proxy Router → LoadBalancer → ProxyService → Upstream
                                                         ↓
                                              Response received
                                                         ↓
                                              Return response to client

                              Background best-effort logging (async):
                                - Log request attempt to request_logs
                                - If audit_enabled: log attempt to audit_logs
```

### 7.3 Data Captured

- Model ID, provider type, endpoint used (ID, base URL, description)
- HTTP status code, response time (ms)
- Token usage (input, output, total) — extracted from upstream response
- Stream flag, request path, error details

### 7.4 Query Capabilities

- Filter by model, provider, status, time range
- Aggregated statistics with grouping by model/provider/endpoint
- Pagination for request log listing

## 8. Request Audit Logging

### 8.1 Concept

Full HTTP request/response recording for proxied requests, toggled per-provider. Captures raw upstream communication for debugging and compliance auditing. Sensitive data in headers (API keys, auth tokens) is redacted before storage.
Audit rows are written per upstream attempt, including failover attempts.

### 8.2 Audit Flow (Non-Streaming)

```
Client → POST /v1/chat/completions {model: "gpt-4o"}
  → Router resolves model + provider
  → Check provider.audit_enabled
  → ProxyService forwards request to upstream
  → Upstream responds with JSON
  → Log to request_logs (existing telemetry)
  → If audit_enabled:
       → One audit row for this upstream attempt
       → Redact sensitive headers
       → Record endpoint metadata (endpoint_id, base_url, description) as snapshot
       → Link to request_log entry via request_log_id (returned from log_request)
       → If audit_capture_bodies = TRUE: truncate bodies to 64KB
       → If audit_capture_bodies = FALSE: store request_body/response_body as NULL
       → INSERT into audit_logs (non-blocking, fire-and-forget)
  → Return response to client
```

### 8.3 Audit Flow (Streaming)

```
Client → POST /v1/chat/completions {model: "gpt-4o", stream: true}
  → Router resolves model + provider
  → Check provider.audit_enabled
  → ProxyService opens streaming connection
  → SSE chunks piped to client
  → On stream complete (finally block):
      → Log to request_logs (existing)
       → If audit_enabled:
           → One audit row for this upstream attempt
           → Record request headers/body + response headers/status
           → Record endpoint metadata (endpoint_id, base_url, description)
           → Link to request_log entry via request_log_id
           → response_body = NULL (streaming bodies are never stored)
           → INSERT into audit_logs (separate AsyncSessionLocal)
```

### 8.4 Non-Interference Guarantees

- Audit INSERT runs in try/except — failures logged to console, never propagated
- Streaming audit uses its own DB session (request-scoped session is closed)
- No modification to request or response pipeline
- Minimal overhead when `audit_enabled = FALSE` (flag checked once, no payload serialization)

### 8.5 Redaction

Applied at write time before INSERT — sensitive data never reaches the database:

- `authorization`, `x-api-key`, `x-goog-api-key` → `[REDACTED]`
- Any header name containing `key`, `secret`, `token`, `auth` → value redacted
- Body fields are not redacted and may contain sensitive user data; body capture can be disabled per provider

### 8.6 Provider Toggle

- `providers.audit_enabled` (BOOLEAN, default FALSE)
- `providers.audit_capture_bodies` (BOOLEAN, default TRUE)
- Toggled via `PATCH /api/providers/{id}` with `{"audit_enabled": true/false, "audit_capture_bodies": true/false}`
- Takes effect immediately for new requests
- Managed in frontend Settings page under "Audit Configuration"

### 8.7 Audit Detail Dialog

The audit detail view is a wide tabbed modal dialog with:

- Summary strip: model, provider, endpoint (ID + description + base URL), status, duration, timestamp
- Request tab: method, URL, headers (redacted), body (pretty-printed JSON)
- Response tab: status, headers, body (pretty-printed JSON, or "not recorded" notice for streaming)
- Endpoint identity fields (`endpoint_id`, `endpoint_base_url`, `endpoint_description`) are displayed in the summary strip

## 9. Batch Data Deletion

### 9.1 Concept

Flexible bulk deletion of historical `request_logs` and `audit_logs` to manage database growth. Users can select a preset time range (7, 15, or 30 days), enter a custom day count (any integer ≥ 1), or delete all records in a section.

### 9.2 Deletion Flow

```
User → Settings Page → "Data Management" section
  → Selects data type (Request Logs or Audit Logs)
  → Selects action (preset: 7/15/30 days, custom days, or delete all)
  → Clicks "Delete" button → Confirmation dialog
  → DELETE /api/stats/requests?older_than_days=7 (or delete_all=true)
  → Backend computes cutoff = current_utc - 7 days (or deletes all)
  → DELETE FROM request_logs WHERE created_at < cutoff (or no filter)
  → Returns { deleted_count: N }
  → Toast: "Deleted N request logs"
```

The UI uses a single action builder pattern: select data type → select action → execute. This replaces the previous layout of duplicated button groups per data type.

Same flow for audit logs via `DELETE /api/audit/logs?older_than_days=N` or `delete_all=true`.

### 9.3 Independence

- Deleting `request_logs` does NOT cascade to `audit_logs`
- Deleting `audit_logs` does NOT affect `request_logs`
- On request log deletion, `audit_logs.request_log_id` is set to `NULL` (`ON DELETE SET NULL`), preserving audit rows without dangling FK references
- Optional maintenance: after large deletions, operators may run SQLite `VACUUM` to reclaim file space

### 9.4 Frontend Placement

Data management controls are on the Settings page (`/settings`) under a "Data Management" section, below the existing "Audit Configuration" and "Configuration Backup" sections.

## 10. Database Design

See [DATA_MODEL.md](./DATA_MODEL.md) for complete schema.

## 11. API Design

See [API_SPEC.md](./API_SPEC.md) for complete endpoint documentation.

## 12. Security Considerations

- No authentication (trusted local network assumption)
- API keys stored in plaintext in SQLite (acceptable for single-user local)
- CORS allows all origins (wildcard)
- No TLS termination (run behind reverse proxy for HTTPS if needed)
- SQLite file permissions should be restricted to owner

## 13. Supported Providers

The application exclusively supports three LLM providers:

- **OpenAI** (`openai`) — GPT models
- **Anthropic** (`anthropic`) — Claude models
- **Gemini** (`gemini`) — Gemini models (via OpenAI-compatible endpoint)

All UI dropdowns, filters, and selectors are limited to these three providers. No other providers (e.g., Ollama, vLLM) are available.
