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
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  },
  {
    "id": 2,
    "provider_id": 2,
    "provider": { "id": 2, "name": "Anthropic", "provider_type": "anthropic" },
    "model_id": "claude-sonnet-4-5",
    "display_name": "Claude Sonnet 4.5 (alias)",
    "model_type": "redirect",
    "redirect_to": "claude-sonnet-4-5-20250929",
    "lb_strategy": "single",
    "is_enabled": true,
    "endpoint_count": 0,
    "active_endpoint_count": 0,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

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
Request (redirect model):
```json
{
  "provider_id": 2,
  "model_id": "claude-sonnet-4-5",
  "display_name": "Claude Sonnet 4.5 (alias)",
  "model_type": "redirect",
  "redirect_to": "claude-sonnet-4-5-20250929",
  "is_enabled": true
}
```
Response `201`: Created model object.

Validation rules:
- `model_id` must be globally unique
- If `model_type = "redirect"`: `redirect_to` is required, must reference an existing native model with the same provider
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
Response `200`: Updated model object. Returns `409` if `model_id` conflicts with an existing model. Returns `400` if redirect validation fails.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `204`: No content. Cascades to delete all endpoints. Returns `400` if other redirect models point to this model.

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
Sends a lightweight probe request to the endpoint's base URL to verify connectivity and authentication.

Response `200`:
```json
{
  "endpoint_id": 1,
  "health_status": "healthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "Connection successful"
}
```

Response `200` (unhealthy):
```json
{
  "endpoint_id": 1,
  "health_status": "unhealthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "Connection refused"
}
```

The endpoint's `health_status` and `last_health_check` fields are updated in the database after each check.

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

## 4. Error Responses

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

## 5. OpenAPI Spec

Auto-generated at:
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- JSON spec: `http://localhost:8000/openapi.json`
