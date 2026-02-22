# API Specification: Prism

Base URL: `http://localhost:8000`

## 1. Configuration API

### 1.1 Providers

#### List Providers
```
GET /api/providers
```
Response `200`:
```json
[
  {
    "id": 1,
    "name": "OpenAI",
    "provider_type": "openai",
    "description": "OpenAI API (GPT models)",
    "audit_enabled": false,
    "audit_capture_bodies": true,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Get Provider
```
GET /api/providers/{id}
```
Response `200`: Single provider object (same schema as list item).

#### Update Provider
```
PATCH /api/providers/{id}
Content-Type: application/json
```
Request:
```json
{
  "audit_enabled": true,
  "audit_capture_bodies": false
}
```
Response `200`: Updated provider object.

Mutable provider fields:
- `audit_enabled` (enable/disable audit for this provider)
- `audit_capture_bodies` (when false, request/response bodies are stored as `null` for this provider)

Provider name, type, and description are seed data and not user-editable.

---

### 1.2 Model Configurations

#### List Models
```
GET /api/models
```
Response `200`:
```json
[
  {
    "id": 1,
    "provider_id": 1,
    "provider": { "id": 1, "name": "OpenAI", "provider_type": "openai" },
    "model_id": "gpt-4o",
    "display_name": "GPT-4o",
    "model_type": "native",
    "redirect_to": null,
    "lb_strategy": "failover",
    "failover_recovery_enabled": true,
    "failover_recovery_cooldown_seconds": 60,
    "is_enabled": true,
    "endpoint_count": 2,
    "active_endpoint_count": 2,
    "health_success_rate": 99.5,
    "health_total_requests": 200,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  },
  {
    "id": 2,
    "provider_id": 2,
    "provider": { "id": 2, "name": "Anthropic", "provider_type": "anthropic" },
    "model_id": "claude-sonnet-4-5",
    "display_name": "Claude Sonnet 4.5 (alias)",
    "model_type": "proxy",
    "redirect_to": "claude-sonnet-4-5-20250929",
    "lb_strategy": "single",
    "is_enabled": true,
    "endpoint_count": 0,
    "active_endpoint_count": 0,
    "health_success_rate": null,
    "health_total_requests": 0,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

New fields in model list response:
- `health_success_rate` (float | null): Weighted average success rate across all active endpoints. `null` if no request data exists.
- `health_total_requests` (int): Total number of requests across all endpoints for this model.

#### Create Model
```
POST /api/models
Content-Type: application/json
```
Request (native model):
```json
{
  "provider_id": 1,
  "model_id": "gpt-4o",
  "display_name": "GPT-4o",
  "model_type": "native",
  "lb_strategy": "single",
  "failover_recovery_enabled": true,
  "failover_recovery_cooldown_seconds": 60,
  "is_enabled": true
}
```
Request (proxy model):
```json
{
  "provider_id": 2,
  "model_id": "claude-sonnet-4-5",
  "display_name": "Claude Sonnet 4.5 (alias)",
  "model_type": "proxy",
  "redirect_to": "claude-sonnet-4-5-20250929",
  "is_enabled": true
}
```
Response `201`: Created model object.

Validation rules:
- `model_id` must be globally unique
- If `model_type = "proxy"`: `redirect_to` is required, must reference an existing native model with the same provider. `lb_strategy` is ignored.
- If `model_type = "native"`: `redirect_to` must be null/omitted

#### Update Model
```
PUT /api/models/{id}
Content-Type: application/json
```
Request (all fields optional):
```json
{
  "provider_id": 2,
  "model_id": "gpt-4o-updated",
  "display_name": "GPT-4o (Updated)",
  "model_type": "native",
  "lb_strategy": "failover",
  "is_enabled": true
}
```
Response `200`: Updated model object. Returns `409` if `model_id` conflicts with an existing model. Returns `400` if proxy validation fails.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `204`: No content. Cascades to delete all endpoints. Returns `400` if other proxy models point to this model.

---

### 1.3 Endpoints

#### List Endpoints for Model
```
GET /api/models/{model_id}/endpoints
```
Response `200`: Array of endpoint objects.

#### Create Endpoint
```
POST /api/models/{model_id}/endpoints
Content-Type: application/json
```
Request:
```json
{
  "base_url": "https://api.openai.com",
  "api_key": "sk-abc123...",
  "is_active": true,
  "priority": 0,
  "description": "Primary production key",
  "custom_headers": {
    "X-Custom-Org": "org-123",
    "X-Priority": "high"
  }
}
```
Response `201`: Created endpoint object.

Note: `custom_headers` is optional. If omitted or `null`, no custom headers are applied. Custom headers are appended to upstream requests after all other headers, overriding same-name headers.

#### Update Endpoint
```
PUT /api/endpoints/{id}
Content-Type: application/json
```
Request:
```json
{
  "base_url": "https://api.openai.com",
  "api_key": "sk-new-key...",
  "is_active": true,
  "priority": 0,
  "description": "Updated key",
  "custom_headers": {
    "X-Custom-Org": "org-456"
  }
}
```
Response `200`: Updated endpoint object.

Setting `custom_headers` to `null` or `{}` removes all custom headers. Omitting the field leaves existing custom headers unchanged.

#### Delete Endpoint
```
DELETE /api/endpoints/{id}
```
Response `204`: No content.

#### Health Check Endpoint
```
POST /api/endpoints/{id}/health-check
```
Sends a real chat completion request to the endpoint using the configured model ID and a simple question to validate the full request chain (URL routing, authentication, model availability). Uses the same URL-building logic as the proxy engine.

Provider-specific probes:
- **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
- **Anthropic**: `POST {base_url}/messages` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`

Response `200`:
```json
{
  "endpoint_id": 1,
  "health_status": "healthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "Connection successful",
  "response_time_ms": 523
}
```

Response `200` (unhealthy):
```json
{
  "endpoint_id": 1,
  "health_status": "unhealthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "HTTP 503: No available channel for model claude-haiku-4-5-20251001",
  "response_time_ms": 150
}
```

Health status determination:
- 2xx → `healthy`
- 401/403 → `unhealthy` (authentication failed)
- 429 → `healthy` (rate-limited but endpoint works)
- Connection error / timeout → `unhealthy`
- Other errors → `unhealthy`

For non-2xx responses, the upstream error message is extracted from the response body (JSON `error.message` field) and appended to the detail string for actionable diagnostics.

The endpoint's `health_status`, `health_detail`, and `last_health_check` fields are updated in the database after each check. The `health_detail` is shown in the frontend tooltip on hover.

#### Base URL Validation

On endpoint create (`POST`) and update (`PUT`), the `base_url` is:
1. **Normalized**: Trailing slashes are stripped (e.g., `https://api.example.com/v1/` → `https://api.example.com/v1`)
2. **Validated**: Rejected with HTTP 422 if it contains a repeated version segment (e.g., `/v1/v1`) or is missing scheme/host

Additionally, `build_upstream_url()` includes a runtime failsafe that auto-corrects any `/vN/vN` double-path and logs a warning.

---

### 1.4 Config Export/Import

#### Export Configuration
```
GET /api/config/export
```
Response `200`:
```json
{
  "version": 2,
  "exported_at": "2025-01-15T10:30:00Z",
  "providers": [
    {
      "name": "OpenAI",
      "provider_type": "openai",
      "description": "OpenAI API (GPT models)",
      "audit_enabled": false,
      "audit_capture_bodies": true
    }
  ],
  "models": [
    {
      "provider_type": "openai",
      "model_id": "gpt-4o",
      "display_name": "GPT-4o",
      "model_type": "native",
      "redirect_to": null,
      "lb_strategy": "failover",
      "failover_recovery_enabled": true,
      "failover_recovery_cooldown_seconds": 60,
      "is_enabled": true,
      "endpoints": [
        {
          "base_url": "https://api.openai.com",
          "api_key": "sk-abc123...",
          "is_active": true,
          "priority": 0,
          "description": "Primary production key",
          "auth_type": null,
          "custom_headers": {
            "X-Custom-Org": "org-123"
          }
        }
      ]
    }
  ],
  "header_blocklist_rules": [
    {
      "name": "Cloudflare Ray",
      "match_type": "exact",
      "pattern": "cf-ray",
      "enabled": true,
      "is_system": true
    }
  ]
}
```
The response includes a `Content-Disposition` header to trigger a file download: `attachment; filename="gateway-config-YYYY-MM-DD.json"`.

#### Import Configuration
```
POST /api/config/import
Content-Type: application/json
```
Request: Full configuration object (same schema as export, `exported_at` is optional).
Response `200`:
```json
{
  "providers_imported": 3,
  "models_imported": 5,
  "endpoints_imported": 10
}
```
Importing is a destructive operation that replaces all existing providers, models, and endpoints. User-defined header blocklist rules are replaced, while system rules have their `enabled` state updated from the import data.

---

### 1.5 Header Blocklist Rules

#### List Header Blocklist Rules
```
GET /api/config/header-blocklist-rules
```
Query parameters:
- `include_disabled` (boolean, default `true`): Whether to include disabled rules in the list.

Response `200`:
```json
[
  {
    "id": 1,
    "name": "Cloudflare Ray",
    "match_type": "exact",
    "pattern": "cf-ray",
    "enabled": true,
    "is_system": true,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Get Header Blocklist Rule
```
GET /api/config/header-blocklist-rules/{id}
```
Response `200`: Single rule object.

#### Create Header Blocklist Rule
```
POST /api/config/header-blocklist-rules
Content-Type: application/json
```
Request:
```json
{
  "name": "My Custom Header",
  "match_type": "prefix",
  "pattern": "x-custom-",
  "enabled": true
}
```
Response `201`: Created rule object. Returns `409` if a rule with the same `match_type` and `pattern` already exists. Prefix patterns must end with `-`.

#### Update Header Blocklist Rule
```
PATCH /api/config/header-blocklist-rules/{id}
Content-Type: application/json
```
Request (all fields optional):
```json
{
  "name": "Updated Name",
  "enabled": false
}
```
Response `200`: Updated rule object.
Note: For system rules (`is_system: true`), only the `enabled` field can be modified. Attempting to change other fields returns `400`.

#### Delete Header Blocklist Rule
```
DELETE /api/config/header-blocklist-rules/{id}
```
Response `204`: No content.
Note: System rules cannot be deleted. Attempting to delete a system rule returns `400`.

---

## 2. Proxy API

### 2.1 OpenAI-Compatible Chat Completions
```
POST /v1/chat/completions
Content-Type: application/json
```
Request (standard OpenAI format):
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "stream": false
}
```
Response: Proxied directly from upstream provider. Format matches the provider's native response.

### 2.2 Anthropic-Compatible Messages
```
POST /v1/messages
Content-Type: application/json
```
Request (standard Anthropic format):
```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}
```
Response: Proxied directly from upstream Anthropic API.

### 2.3 Streaming

Both endpoints support streaming when `"stream": true` is set. The response will be `text/event-stream` (SSE) with chunks proxied directly from the upstream provider.

### 2.4 Token Usage Extraction

The gateway extracts token usage from upstream responses and logs it to `request_logs`. Extraction is provider-aware:

**Non-streaming responses:**
| Provider | Response Format | Extraction Path |
|---|---|---|
| OpenAI | `{"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}` | `usage.prompt_tokens`, `usage.completion_tokens`, `usage.total_tokens` |
| Anthropic Messages | `{"usage": {"input_tokens": N, "output_tokens": N}}` | `usage.input_tokens`, `usage.output_tokens`; `total_tokens` = sum |
| Anthropic count_tokens | `{"input_tokens": N}` | Top-level `input_tokens`; `output_tokens` and `total_tokens` = null |

**Streaming responses:**
The gateway accumulates SSE chunks during streaming and extracts usage from the final events:
| Provider | Usage Events | Extraction |
|---|---|---|
| OpenAI | Final chunk with `usage` field (requires `stream_options.include_usage: true`) | Same as non-streaming `usage` object |
| Anthropic | `message_start` event → `message.usage.input_tokens`; `message_delta` event → `usage.output_tokens` | Accumulated from both events; `total_tokens` = sum |

If token data cannot be extracted, all token fields are logged as `null`.

---

## 3. Health Check

```
GET /health
```
Response `200`:
```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

---

## 4. Statistics API

### 4.1 List Request Logs
```
GET /api/stats/requests
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `model_id` | string | — | Filter by model ID |
| `provider_type` | string | — | Filter by provider type (openai, anthropic, gemini only) |
| `status_code` | integer | — | Filter by HTTP status code |
| `success` | boolean | — | Filter by success (true = 2xx, false = non-2xx) |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `limit` | integer | 50 | Max results (1-500) |
| `offset` | integer | 0 | Pagination offset |

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "model_id": "gpt-4o",
      "provider_type": "openai",
      "endpoint_id": 1,
      "endpoint_base_url": "https://api.openai.com",
      "endpoint_description": "Primary production key",
      "status_code": 200,
      "response_time_ms": 1234,
      "is_stream": false,
      "input_tokens": 15,
      "output_tokens": 42,
      "total_tokens": 57,
      "request_path": "/v1/chat/completions",
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

### 4.2 Get Aggregated Statistics
```
GET /api/stats/summary
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |
| `group_by` | string | — | Group results by: `model`, `provider`, `endpoint` |
| `model_id` | string | — | Filter by model ID |
| `provider_type` | string | — | Filter by provider type (openai, anthropic, gemini) |
| `endpoint_id` | integer | — | Filter by endpoint ID |

Response `200`:
```json
{
  "total_requests": 1500,
  "success_count": 1450,
  "error_count": 50,
  "success_rate": 96.67,
  "avg_response_time_ms": 850,
  "p95_response_time_ms": 2100,
  "total_input_tokens": 50000,
  "total_output_tokens": 120000,
  "total_tokens": 170000,
  "groups": [
    {
      "key": "gpt-4o",
      "total_requests": 800,
      "success_count": 790,
      "error_count": 10,
      "avg_response_time_ms": 750,
      "total_tokens": 90000
    }
  ]
}
```

### 4.3 Get Endpoint Success Rates
```
GET /api/stats/endpoint-success-rates
```
Returns success rate data for all endpoints, computed from `request_logs`.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |

Response `200`:
```json
[
  {
    "endpoint_id": 1,
    "total_requests": 150,
    "success_count": 148,
    "error_count": 2,
    "success_rate": 98.67
  },
  {
    "endpoint_id": 2,
    "total_requests": 0,
    "success_count": 0,
    "error_count": 0,
    "success_rate": null
  }
]
```

Fields:
- `endpoint_id` (int): The endpoint ID
- `total_requests` (int): Total requests routed through this endpoint
- `success_count` (int): Requests with 2xx status codes
- `error_count` (int): Requests with non-2xx status codes
- `success_rate` (float | null): Success percentage (0-100), `null` if no requests

### 4.4 Delete Request Logs (Batch)
```
DELETE /api/stats/requests
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `older_than_days` | integer | — | Delete logs older than N days. Must be ≥ 1. |
| `delete_all` | boolean | false | Delete all request logs. |

Exactly one of `older_than_days` or `delete_all=true` must be provided. If both are provided, returns `400`. If neither is provided, returns `400`.

When using `older_than_days`, the cutoff timestamp is computed server-side from UTC app time as `current_utc - older_than_days`. All `request_logs` with `created_at` before the cutoff are deleted.

Response `200`:
```json
{
  "deleted_count": 5432
}
```

Response `400`:
```json
{
  "detail": "Provide either 'older_than_days' (integer >= 1) or 'delete_all=true'"
}
```

Deleting request logs does NOT cascade to `audit_logs`. Linked audit rows remain, and `audit_logs.request_log_id` is set to `null` (`ON DELETE SET NULL`).

---

## 5. Audit API

### 5.1 List Audit Logs
```
GET /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `provider_id` | integer | — | Filter by provider ID |
| `model_id` | string | — | Filter by model ID |
| `status_code` | integer | — | Filter by response status code |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `limit` | integer | 50 | Max results (1-200) |
| `offset` | integer | 0 | Pagination offset |

The list endpoint returns one row per upstream attempt. If a proxy request fails over across endpoints, each attempt has its own audit row.

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "request_log_id": 42,
      "provider_id": 1,
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "endpoint_base_url": "https://api.openai.com",
      "endpoint_description": "Primary production key",
      "request_method": "POST",
      "request_url": "https://api.openai.com/v1/chat/completions",
      "request_headers": "{\"content-type\": \"application/json\", \"authorization\": \"Bearer [REDACTED]\"}",
      "request_body_preview": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"con...",
      "response_status": 200,
      "is_stream": false,
      "duration_ms": 1234,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

The list endpoint returns `request_body_preview` (first 200 characters of the request body) instead of the full body. Use the detail endpoint for full content.
If provider body capture is disabled, `request_body_preview` is `null`.
Rows are ordered by `created_at DESC`.

### 5.2 Get Audit Log Detail
```
GET /api/audit/logs/{id}
```
Response `200`:
```json
{
  "id": 1,
  "request_log_id": 42,
  "provider_id": 1,
  "model_id": "gpt-4o",
  "endpoint_id": 1,
  "endpoint_base_url": "https://api.openai.com",
  "endpoint_description": "Primary production key",
  "request_method": "POST",
  "request_url": "https://api.openai.com/v1/chat/completions",
  "request_headers": "{\"content-type\": \"application/json\", \"authorization\": \"Bearer [REDACTED]\"}",
  "request_body": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello!\"}],\"temperature\":0.7}",
  "response_status": 200,
  "response_headers": "{\"content-type\": \"application/json\", \"x-request-id\": \"req_abc123\"}",
  "response_body": "{\"id\":\"chatcmpl-abc\",\"choices\":[...],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}",
  "is_stream": false,
  "duration_ms": 1234,
  "created_at": "2025-01-15T10:30:00Z"
}
```

For streaming requests, `response_body` is `null` (streaming response bodies are not recorded).
If provider body capture is disabled, both `request_body` and `response_body` are `null`.

Response `404`: Audit log not found.

### 5.3 Delete Audit Logs (Batch)
```
DELETE /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `before` | datetime | — | Delete logs created before this time (ISO 8601). |
| `older_than_days` | integer | — | Delete logs older than N days. Must be ≥ 1. |
| `delete_all` | boolean | false | Delete all audit logs. |

Exactly one of `before`, `older_than_days`, or `delete_all=true` must be provided. If multiple are provided or none are provided, returns `400`.

When using `older_than_days`, the cutoff timestamp is computed server-side from UTC app time as `current_utc - older_than_days`.

Response `200`:
```json
{
  "deleted_count": 1234
}
```

Response `400`: Missing or conflicting parameters.

### 5.4 Redaction Rules

All audit log entries have sensitive header values redacted before storage:
- `authorization` → `Bearer [REDACTED]`
- `x-api-key` → `[REDACTED]`
- `x-goog-api-key` → `[REDACTED]`
- Any header name containing `key`, `secret`, `token`, or `auth` (case-insensitive) → value replaced with `[REDACTED]`

Request and response bodies are not header-redacted and may contain user-provided secrets or PII.
Body capture is configurable per provider via `audit_capture_bodies`; when disabled, both `request_body` and `response_body` are `null`.

### 5.5 Body Size Limits

When body capture is enabled for the provider, request and response bodies are truncated to 64KB before storage. A `[TRUNCATED]` marker is appended when truncation occurs.

---

## 6. Error Responses

All errors follow this format:
```json
{
  "detail": "Error message describing what went wrong"
}
```

| Status Code | Meaning |
|---|---|
| 400 | Bad request (invalid input) |
| 404 | Resource not found |
| 409 | Conflict (duplicate model_id) |
| 502 | Upstream provider error |
| 503 | No active endpoints available |

---

## 7. OpenAPI Spec

Auto-generated at:
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- JSON spec: `http://localhost:8000/openapi.json`
