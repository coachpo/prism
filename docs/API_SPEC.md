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
    "lb_strategy": "round_robin",
    "is_enabled": true,
    "endpoints": [
      {
        "id": 1,
        "base_url": "https://api.openai.com",
        "api_key": "sk-***masked***",
        "is_active": true,
        "priority": 0,
        "description": "Primary key",
        "health_status": "healthy",
        "success_count": 142,
        "failure_count": 2
      }
    ],
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
Request:
```json
{
  "provider_id": 1,
  "model_id": "gpt-4o",
  "display_name": "GPT-4o",
  "lb_strategy": "single",
  "is_enabled": true
}
```
Response `201`: Created model object.

#### Update Model
```
PUT /api/models/{id}
Content-Type: application/json
```
Request:
```json
{
  "display_name": "GPT-4o (Updated)",
  "lb_strategy": "round_robin",
  "is_enabled": true
}
```
Response `200`: Updated model object.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `204`: No content. Cascades to delete all endpoints.

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

#### Reset Endpoint Health
```
POST /api/endpoints/{id}/reset-health
```
Response `200`:
```json
{
  "id": 1,
  "health_status": "unknown",
  "success_count": 0,
  "failure_count": 0
}
```

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
| 503 | No healthy endpoints available |

---

## 5. OpenAPI Spec

Auto-generated at:
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- JSON spec: `http://localhost:8000/openapi.json`
