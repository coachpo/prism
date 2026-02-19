# API Specification: LLM Proxy Gateway

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
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Get Provider
```
GET /api/providers/{id}
```
Response `200`: Single provider object.

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
    "lb_strategy": "round_robin",
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
  "lb_strategy": "round_robin",
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
  "description": "Primary production key"
}
```
Response `201`: Created endpoint object.

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
  "description": "Updated key"
}
```
Response `200`: Updated endpoint object.

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
| `from_time` | datetime | 24h ago | Start of time range |
| `to_time` | datetime | now | End of time range |
| `group_by` | string | — | Group results by: `model`, `provider`, `endpoint` |

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
| `from_time` | datetime | 24h ago | Start of time range |
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

---

## 5. Error Responses

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

## 6. OpenAPI Spec

Auto-generated at:
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- JSON spec: `http://localhost:8000/openapi.json`
