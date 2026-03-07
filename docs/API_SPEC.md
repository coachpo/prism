# API Specification: Prism

Base URL: `http://localhost:8000`

## 0. Profile Context Semantics
- Profile routes (`/api/profiles/*`) are global and do not require `X-Profile-Id`.
- Other management endpoints (`/api/*`) require `X-Profile-Id` to select explicit profile scope.
- Proxy endpoints (`/v1/*`, `/v1beta/*`) always use the active profile and ignore management scope overrides.
- Detail endpoints return `404` when a resource exists in another profile but not in the effective profile context.


## 1. Management API (`/api/*`)

### 1.0 Profiles
#### List Profiles
```
GET /api/profiles
```
Response `200`: Array of non-deleted profile objects.

#### Get Active Profile
```
GET /api/profiles/active
```
Response `200`: Active profile object.

#### Create Profile
```
POST /api/profiles
```
Request:
```json
{
  "name": "Profile A",
  "description": "OpenAI workspace"
}
```
Response `201`: Created profile object.
Returns `409` if 10 non-deleted profiles already exist.

#### Update Profile
```
PATCH /api/profiles/{id}
```
Request fields: `name` (optional), `description` (optional).
Response `200`: Updated profile object.

#### Activate Profile (CAS)
```
POST /api/profiles/{id}/activate
```
Request:
```json
{
  "expected_active_profile_id": 1,
  "expected_active_profile_version": 4
}
```
Response `200`: Updated active profile object.
Returns `409` on stale expected version (conflict-safe activation).

#### Delete Profile
```
DELETE /api/profiles/{id}
```
Response `204`: Soft-delete succeeds for inactive profile.
Returns `400` if the target profile is currently active.

---

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

Provider records are global/shared and are not profile-scoped in this phase.

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
    "connection_count": 2,
    "active_connection_count": 2,
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
    "connection_count": 0,
    "active_connection_count": 0,
    "health_success_rate": null,
    "health_total_requests": 0,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

New fields in model list response:
- `health_success_rate` (float | null): Weighted average success rate across all active connections. `null` if no request data exists.
- `health_total_requests` (int): Total number of requests across all connections for this model.

#### Create Model
```
POST /api/models
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
- `model_id` must be unique within the effective profile scope
- If `model_type = "proxy"`: `redirect_to` is required, must reference an existing native model with the same provider. `lb_strategy` is ignored.
- If `model_type = "native"`: `redirect_to` must be null/omitted

#### Update Model
```
PUT /api/models/{id}
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
Response `200`: Updated model object. Returns `409` if `model_id` conflicts within the effective profile. Returns `400` if proxy validation fails.

#### Delete Model
```
DELETE /api/models/{id}
```
Response `204`: No content. Cascades to delete all connections. Returns `400` if other proxy models point to this model.

---

### 1.3 Endpoints (Profile-Scoped Credentials)

#### List Endpoints
```
GET /api/endpoints
```
Response `200`: Array of endpoint objects in the effective profile scope, ordered by `position ASC, id ASC`.

Endpoint object fields include:
- `id`
- `profile_id`
- `name`
- `base_url`
- `api_key`
- `position` (zero-based contiguous ordering index within the effective profile)
- `created_at`
- `updated_at`

#### Create Endpoint
```
POST /api/endpoints
```
Request:
```json
{
  "name": "Primary OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-abc123..."
}
```
Response `201`: Created endpoint object.

#### Update Endpoint
```
PUT /api/endpoints/{id}
```
Request:
```json
{
  "name": "Updated OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-new-key..."
}
```
Response `200`: Updated endpoint object.

#### Move Endpoint Position
```
PATCH /api/endpoints/{id}/position
```
Request:
```json
{
  "to_index": 0
}
```
Response `200`: Ordered array of endpoint objects after the move.

Behavior:
- `to_index` must be in the range `0..(endpoint_count - 1)` or the API returns `422`.
- A no-op move returns the current ordered list unchanged.
- The backend rewrites endpoint positions to contiguous `0..N-1` values after every successful move.

#### Delete Endpoint
```
DELETE /api/endpoints/{id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` if any connections still reference this endpoint.
After a successful delete, later endpoints in the same profile are compacted so `position` remains contiguous.

### 1.4 Connections (Model-Scoped Routing)

#### List Connections for Model
```
GET /api/models/{model_id}/connections
```
Response `200`: Array of connection objects.

#### Create Connection
```
POST /api/models/{model_id}/connections
```
Request (using existing endpoint):
```json
{
  "endpoint_id": 1,
  "is_active": true,
  "priority": 0,
  "description": "Primary production key",
  "custom_headers": {
    "X-Custom-Org": "org-123"
  },
  "pricing_template_id": 2
}
```
Request (inline endpoint creation):
```json
{
  "endpoint_create": {
    "name": "New Endpoint",
    "base_url": "https://api.openai.com",
    "api_key": "sk-abc123..."
  },
  "is_active": true,
  "priority": 0,
  "pricing_template_id": null
}
```
Response `201`: Created connection object.

#### Update Connection
```
PUT /api/connections/{id}
```
Request: Same fields as Create Connection. `endpoint_create` is supported on update and is mutually exclusive with `endpoint_id`. Legacy per-connection pricing fields are rejected with `422`.
Response `200`: Updated connection object.

#### Update Connection Pricing Template
```
PUT /api/connections/{id}/pricing-template
```
Request:
```json
{
  "pricing_template_id": 2
}
```
Set to `null` to detach the template from the connection.

Response `200`: Updated connection object.

#### Delete Connection
```
DELETE /api/connections/{id}
```
Response `204`: No content.

#### Health Check Connection
```
POST /api/connections/{id}/health-check
```
Sends a provider-specific lightweight request using the configured model ID to validate URL routing, authentication, and model availability end to end.

Response `200`:
```json
{
  "connection_id": 1,
  "health_status": "healthy",
  "checked_at": "2025-01-15T10:30:00Z",
  "detail": "Connection successful",
  "response_time_ms": 523
}
```
Provider-specific health-check probes:
- OpenAI: `POST {base_url}/v1/responses` with `input: "hi"` (with legacy fallback to `/v1/chat/completions` when needed).
- Anthropic: `POST {base_url}/v1/messages` with a one-token user prompt.
- Gemini: `POST {base_url}/v1beta/models/{model}:generateContent` with minimal content payload.

#### Base URL Validation

On endpoint create (`POST`) and update (`PUT`), the `base_url` is:
1. **Normalized**: Trailing slashes are stripped (e.g., `https://api.example.com/` → `https://api.example.com`)
2. **Validated**: Rejected with HTTP 422 if scheme/host is missing.
3. **Version path is not allowed**: Rejected with HTTP 422 if `base_url` includes provider API version segments such as `/v1` or `/v1beta`.

Use host-root base URLs only:
- ✅ `https://api.openai.com`
- ✅ `https://generativelanguage.googleapis.com`
- ❌ `https://api.openai.com/v1`
- ❌ `https://generativelanguage.googleapis.com/v1beta`

### 1.5 Pricing Templates

#### List Pricing Templates
```
GET /api/pricing-templates
```
Response `200`: Array of pricing template list items in the effective profile scope.

#### Create Pricing Template
```
POST /api/pricing-templates
```
Request:
```json
{
  "name": "GPT-4o Standard",
  "description": "Default OpenAI pricing",
  "pricing_currency_code": "USD",
  "input_price": "5.00",
  "output_price": "15.00",
  "cached_input_price": "2.50",
  "cache_creation_price": null,
  "reasoning_price": "15.00",
  "missing_special_token_price_policy": "MAP_TO_OUTPUT"
}
```
Response `201`: Created pricing template object.

#### Update Pricing Template
```
PUT /api/pricing-templates/{id}
```
Request: Any mutable pricing template fields.
Response `200`: Updated pricing template object.

#### Delete Pricing Template
```
DELETE /api/pricing-templates/{id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the template is still referenced by connections; response `detail` includes a `connections` array with dependency details.

#### List Connections Using Template
```
GET /api/pricing-templates/{id}/connections
```
Response `200`: Usage payload with `template_id` and `items[]` (`connection_id`, `connection_name`, `model_config_id`, `model_id`, `endpoint_id`, `endpoint_name`).
---

### 1.6 Config Export/Import

#### Export Configuration
```
GET /api/config/export
```
Response `200`:
```json
{
  "version": 2,
  "exported_at": "2025-01-15T10:30:00Z",
  "user_settings": {
    "report_currency_code": "USD",
    "report_currency_symbol": "$",
    "endpoint_fx_mappings": [
      {
        "model_id": "gpt-4o",
        "endpoint_id": 1,
        "fx_rate": "1.0"
      }
    ]
  },
  "endpoints": [
    {
      "endpoint_id": 1,
      "name": "Primary OpenAI",
      "base_url": "https://api.openai.com",
      "api_key": "sk-abc123...",
      "position": 0
    }
  ],
  "pricing_templates": [
    {
      "pricing_template_id": 2,
      "name": "GPT-4o Standard",
      "description": "Default OpenAI pricing",
      "pricing_unit": "PER_1M",
      "pricing_currency_code": "USD",
      "input_price": "5.00",
      "output_price": "15.00",
      "cached_input_price": "2.50",
      "cache_creation_price": null,
      "reasoning_price": "15.00",
      "missing_special_token_price_policy": "MAP_TO_OUTPUT",
      "version": 3
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
      "connections": [
        {
          "connection_id": 11,
          "endpoint_id": 1,
          "is_active": true,
          "priority": 0,
          "description": "Primary production key",
          "custom_headers": {
            "X-Custom-Org": "org-123"
          },
          "pricing_template_id": 2
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
```
Request: Full configuration object (accepts version 2 only).
Response `200`:
```json
{
  "endpoints_imported": 2,
  "pricing_templates_imported": 4,
  "models_imported": 5,
  "connections_imported": 10
}
```
Importing is profile-targeted and replaces configuration in the effective profile only. Other profiles are not deleted or mutated. Providers remain global and are never globally deleted by import.
When endpoint `position` is present, import uses it as the ordering hint; when omitted, import falls back to endpoint file order. Persisted endpoint positions are always normalized to contiguous `0..N-1` values.

Compatibility and versioning semantics:
- Version 2 is the canonical and only accepted format.
- Export/import uses explicit IDs (`endpoint_id`, `connection_id`, `pricing_template_id`) and validates all references.
- Import defaults to `replace` behavior for the target profile in this phase.

---

### 1.7 Settings API (Profile-Scoped)

#### Get Costing Settings
```
GET /api/settings/costing
```
Response `200`:
```json
{
  "report_currency_code": "USD",
  "report_currency_symbol": "$",
  "timezone_preference": "Europe/Helsinki",
  "endpoint_fx_mappings": [
    {
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "fx_rate": "1.0"
    }
  ]
}
```

Settings APIs are profile-scoped by explicit `X-Profile-Id` (required header).

#### Update Costing Settings
```
PUT /api/settings/costing
```
Request:
```json
{
  "report_currency_code": "EUR",
  "report_currency_symbol": "€",
  "timezone_preference": "Europe/Helsinki",
  "endpoint_fx_mappings": [
    {
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "fx_rate": "0.92"
    }
  ]
}
```
Response `200`: Updated settings object.

---

### 1.8 Header Blocklist Rules (System Global + User Profile-Scoped)

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
Response `201`: Created rule object. Returns `409` if a user rule with the same `match_type` and `pattern` already exists in the effective profile. Prefix patterns must end with `-`.

#### Update Header Blocklist Rule
```
PATCH /api/config/header-blocklist-rules/{id}
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

Both routes support streaming when `"stream": true` is set. The response will be `text/event-stream` (SSE) with chunks proxied directly from the upstream provider.

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
| OpenAI | Final chunk/event containing a `usage` object (when provided by upstream) | Same as non-streaming `usage` object |
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

Stats APIs are profile-scoped by explicit `X-Profile-Id` (required header).

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
| `connection_id` | integer | — | Filter by connection ID |
| `limit` | integer | 50 | Max results (1-500) |
| `offset` | integer | 0 | Pagination offset |

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 2,
      "model_id": "gpt-4o",
      "provider_type": "openai",
      "endpoint_id": 12,
      "connection_id": 1,
      "endpoint_base_url": "https://api.openai.com",
      "endpoint_description": "Primary production key",
      "status_code": 200,
      "response_time_ms": 1234,
      "is_stream": false,
      "input_tokens": 15,
      "output_tokens": 42,
      "total_tokens": 57,
      "cache_read_input_tokens": 0,
      "cache_creation_input_tokens": 0,
      "reasoning_tokens": 0,
      "success_flag": true,
      "billable_flag": true,
      "priced_flag": true,
      "total_cost_user_currency_micros": 1250,
      "report_currency_code": "USD",
      "report_currency_symbol": "$",
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
| `connection_id` | integer | — | Filter by connection ID |

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

### 4.3 Get Connection Success Rates
```
GET /api/stats/connection-success-rates
```
Returns success rate data for all connections, computed from `request_logs`.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |

Response `200`:
```json
[
  {
    "connection_id": 1,
    "total_requests": 150,
    "success_count": 148,
    "error_count": 2,
    "success_rate": 98.67
  },
  {
    "connection_id": 2,
    "total_requests": 0,
    "success_count": 0,
    "error_count": 0,
    "success_rate": null
  }
]
```

Fields:
- `connection_id` (int): The connection ID
- `total_requests` (int): Total requests routed through this connection
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

### 4.5 Get Spending Reports
```
GET /api/stats/spending
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | — | Time preset: `today`, `24h`, `last_7_days`, `7d`, `last_30_days`, `30d`, `custom`, `all` |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `provider_type` | string | — | Filter by provider type |
| `model_id` | string | — | Filter by model ID |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |
| `group_by` | string | `none` | Group by: `none`, `day`, `week`, `month`, `provider`, `model`, `endpoint`, `model_endpoint` |
| `limit` | integer | 50 | Max results (1-500) |
| `offset` | integer | 0 | Pagination offset |
| `top_n` | integer | 5 | Number of top spenders to return (1-50) |

Response `200`:
```json
{
  "summary": {
    "total_cost_micros": 1250000,
    "successful_request_count": 1500,
    "priced_request_count": 1450,
    "unpriced_request_count": 50,
    "total_input_tokens": 50000,
    "total_output_tokens": 120000,
    "total_cache_read_input_tokens": 10000,
    "total_cache_creation_input_tokens": 1500,
    "total_reasoning_tokens": 2000,
    "total_tokens": 182000,
    "avg_cost_per_successful_request_micros": 833
  },
  "groups": [
    {
      "key": "gpt-4o",
      "total_cost_micros": 850000,
      "total_requests": 800,
      "priced_requests": 790,
      "unpriced_requests": 10,
      "total_tokens": 90000
    }
  ],
  "groups_total": 12,
  "top_spending_models": [
    {
      "model_id": "gpt-4o",
      "total_cost_micros": 850000
    }
  ],
  "top_spending_endpoints": [
    {
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "total_cost_micros": 740000
    }
  ],
  "unpriced_breakdown": {
    "PRICING_DISABLED": 30,
    "UNKNOWN": 20
  },
  "report_currency_code": "USD",
  "report_currency_symbol": "$"
}
```

---

## 5. Audit API

Audit APIs are profile-scoped by explicit `X-Profile-Id` (required header).

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
| `connection_id` | integer | — | Filter by connection ID |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `limit` | integer | 50 | Max results (1-200) |
| `offset` | integer | 0 | Pagination offset |

The list API returns one row per upstream attempt. If a proxy request fails over across connections, each attempt has its own audit row.

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 2,
      "request_log_id": 42,
      "provider_id": 1,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "connection_id": 1,
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

The list API returns `request_body_preview` (first 200 characters of the request body) instead of the full body. Use the detail API for full content.
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
  "profile_id": 2,
  "request_log_id": 42,
  "provider_id": 1,
  "model_id": "gpt-4o",
  "endpoint_id": 12,
  "connection_id": 1,
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
| 409 | Conflict (stale activation CAS version, profile capacity reached, or duplicate scoped identifier) |
| 502 | Upstream provider error |
| 503 | No active connections available |

---

## 7. OpenAPI Spec

Auto-generated at:
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`
- JSON spec: `http://localhost:8000/openapi.json`


## 8. Revision Provenance (Profile Isolation, 2026-02-28)

Source inputs: `docs/PROFILE_ISOLATION_REQUIREMENTS.md`, `docs/PROFILE_ISOLATION_UPGRADE_PLAN.md`, `docs/PROFILE_ISOLATION_FRONTEND_ITERATION_PLAN.md`, `docs/PROFILE_ISOLATION_RESEARCH_REFERENCES.md`, and `docs/PROFILE_ISOLATION_SUPPORTING_EVIDENCE.md`.


This appendix links the API surface in this document to the profile-isolation delivery revisions and requirement sources.

Commit alignment:

- Backend `c0f2daa`: introduced active-vs-effective profile dependency split, profile lifecycle endpoints, profile-scoped routing/config behavior, and v2 config import/export with pricing template references.
- Frontend `02c70ce`: introduced management-scope propagation via `X-Profile-Id`, selected/active profile shell behavior, and profile-aware refetch flows that consume these APIs.
- Root/docs `f6f0106`: aligned architecture/docs and bootstrap narrative to the profile-isolated API model.

Normative API invariants in this spec:

- `/api/*` profile-scoped endpoints require explicit `X-Profile-Id`; profile lifecycle endpoints under `/api/profiles/*` are global.
- `/v1/*` and `/v1beta/*` always use active profile context and ignore management profile overrides.
- Cross-profile detail access resolves as `404` under effective profile scope.
- Profile activation is conflict-safe via CAS payload and returns `409` on stale state.
- Profile creation is capped at 10 non-deleted profiles and returns `409` at capacity.
- Config export is canonical `version=2` with explicit endpoint/connection IDs and `pricing_templates`; import accepts only `version=2` and applies target-profile replace semantics in this phase.

Requirement trace anchors: `FR-001`, `FR-003`, `FR-004`, `FR-006`, `FR-007`, `FR-009`, `FR-010`.
