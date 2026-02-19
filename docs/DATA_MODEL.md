# Data Model Document: LLM Proxy Gateway

## 1. Entity Relationship Diagram

```
┌──────────────────┐       ┌──────────────────────┐       ┌──────────────────────┐
│   providers      │       │   model_configs      │       │     endpoints        │
├──────────────────┤       ├──────────────────────┤       ├──────────────────────┤
│ id (PK)          │◀──┐   │ id (PK)              │◀──┐   │ id (PK)              │
│ name             │   └───│ provider_id (FK)     │   └───│ model_config_id (FK) │
│ provider_type    │       │ model_id (UNIQUE)    │       │ base_url             │
│ description      │       │ display_name         │       │ api_key              │
│ created_at       │       │ model_type           │       │ is_active            │
│ updated_at       │       │ redirect_to          │       │ priority             │
└──────────────────┘       │ lb_strategy          │       │ description          │
                           │ is_enabled           │       │ health_status        │
                           │ created_at           │       │ last_health_check    │
                           │ updated_at           │       │ created_at           │
                           └──────────────────────┘       │ updated_at           │
                                    │                     └──────────────────────┘
                                    │ redirect_to (self-ref)
                                    └──────▶ model_configs.model_id
```

## 2. Table Definitions

### 2.1 `providers`

Represents an LLM API provider type.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| name | VARCHAR(100) | NOT NULL, UNIQUE | Display name (e.g., "OpenAI") |
| provider_type | VARCHAR(50) | NOT NULL | Enum: `openai`, `anthropic`, `gemini` |
| description | TEXT | NULLABLE | Optional description |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Seed data:
```sql
INSERT INTO providers (name, provider_type, description) VALUES
  ('OpenAI', 'openai', 'OpenAI API (GPT models)'),
  ('Anthropic', 'anthropic', 'Anthropic API (Claude models)'),
  ('Google Gemini', 'gemini', 'Google Gemini API');
```

### 2.2 `model_configs`

Maps a model ID string to a provider and load balancing configuration. Supports two model types: native (real model with endpoints) and proxy (alias that forwards to a native model).

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| provider_id | INTEGER | FK → providers.id, NOT NULL | Associated provider |
| model_id | VARCHAR(200) | NOT NULL, UNIQUE | Model identifier (e.g., "gpt-4o", "claude-sonnet-4-5") |
| display_name | VARCHAR(200) | NULLABLE | Human-friendly name |
| model_type | VARCHAR(20) | NOT NULL, DEFAULT 'native' | Model type: `native` or `proxy` |
| redirect_to | VARCHAR(200) | NULLABLE | Target model_id for proxy models (must be a native model of the same provider) |
| lb_strategy | VARCHAR(50) | NOT NULL, DEFAULT 'single' | Load balancing: `single`, `round_robin`, `failover` (only applies to native models; ignored for proxy models) |
| is_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Whether this model is available for proxying |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

**Constraints:**
- `model_id` is globally unique across all model types
- When `model_type = 'proxy'`: `redirect_to` must reference an existing native model's `model_id` with the same `provider_id`
- When `model_type = 'native'`: `redirect_to` must be NULL
- Proxy models cannot have endpoints (enforced at application level)
- Proxy chains are not allowed (proxy → proxy is invalid)
- Proxy models do not use load balancing (lb_strategy is ignored)

### 2.3 `endpoints`

Stores BaseURL + APIKey combinations for a model configuration, with health check status.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| model_config_id | INTEGER | FK → model_configs.id, NOT NULL, ON DELETE CASCADE | Parent model config |
| base_url | VARCHAR(500) | NOT NULL | API base URL (e.g., "https://api.openai.com") |
| api_key | VARCHAR(500) | NOT NULL | API key for this endpoint |
| is_active | BOOLEAN | NOT NULL, DEFAULT TRUE | Whether this endpoint is selected for use |
| priority | INTEGER | NOT NULL, DEFAULT 0 | Priority for failover (lower = higher priority) |
| description | TEXT | NULLABLE | Optional label (e.g., "Production key", "Backup key") |
| health_status | VARCHAR(20) | NOT NULL, DEFAULT 'unknown' | Health status: `unknown`, `healthy`, `unhealthy` |
| health_detail | TEXT | NULLABLE | Detail message from last health check (e.g., error message from upstream) |
| last_health_check | DATETIME | NULLABLE | Timestamp of last health check |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

## 3. Indexes

```sql
CREATE UNIQUE INDEX idx_model_configs_model_id ON model_configs(model_id);
CREATE INDEX idx_model_configs_provider_id ON model_configs(provider_id);
CREATE INDEX idx_model_configs_model_type ON model_configs(model_type);
CREATE INDEX idx_model_configs_redirect_to ON model_configs(redirect_to);
CREATE INDEX idx_endpoints_model_config_id ON endpoints(model_config_id);
CREATE INDEX idx_endpoints_is_active ON endpoints(is_active);
```

## 4. Relationships

- `providers` 1:N `model_configs` — One provider can have many model configurations
- `model_configs` 1:N `endpoints` — One native model can have many BaseURL/APIKey combinations (proxy models have zero endpoints)
- `model_configs` self-reference via `redirect_to` → `model_id` — A proxy model points to a native model
- Cascade delete: Deleting a model_config deletes all its endpoints

## 5. Load Balancing Behavior

### Strategy: `single`
- Only the endpoint with `is_active = TRUE` and lowest `priority` is used
- If multiple are active, the lowest priority wins

### Strategy: `round_robin`
- All endpoints with `is_active = TRUE` are rotated
- State tracked in-memory (not persisted)

### Strategy: `failover`
- Endpoints tried in `priority` order (ascending)
- On failure (HTTP 5xx, 429, timeout, connection error), next endpoint is tried
- All endpoints exhausted → return 502 to client

## 6. Model Proxy (Alias) Behavior

### Resolution Flow
1. Proxy receives request with `model` field (e.g., `claude-sonnet-4-5`)
2. Look up `model_configs` by `model_id`
3. If `model_type = 'proxy'`, resolve `redirect_to` to find the target native model
4. Use the target native model's endpoints and provider config for the upstream request
5. The original `model` field in the request body is NOT modified — the gateway is transparent

### Validation Rules
- A proxy model's `provider_id` must match the target native model's `provider_id`
- The target model (`redirect_to`) must exist and be of type `native`
- Circular/chained proxies are rejected at creation/update time
- Load balancing strategy is ignored for proxy models (always uses target model's strategy)

## 7. Health Check Behavior

### Check Process
1. User triggers health check via UI or API
2. Backend sends a real chat completion request using the endpoint's configured model ID and a simple question ("hi")
3. Uses the same URL-building logic as the proxy engine (`build_upstream_url`) to avoid path duplication
4. Provider-specific requests:
   - **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
   - **Anthropic**: `POST {base_url}/messages` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
5. Response determines status:
   - 2xx → `healthy`
   - 401/403 → `unhealthy` (authentication failed)
   - 429 → `healthy` (rate-limited but endpoint works)
   - Connection error / timeout → `unhealthy`
   - Other errors → `unhealthy`
6. `health_status`, `health_detail`, and `last_health_check` are updated in the database

## 8. Request Logging (Telemetry)

### 8.1 `request_logs`

Stores telemetry data for every proxy request processed by the gateway.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| model_id | VARCHAR(200) | NOT NULL | Model ID from the request |
| provider_type | VARCHAR(50) | NOT NULL | Provider type (openai, anthropic, gemini) |
| endpoint_id | INTEGER | NULLABLE | Endpoint used (NULL if no endpoint selected) |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Base URL of the endpoint used |
| status_code | INTEGER | NOT NULL | HTTP status code returned |
| response_time_ms | INTEGER | NOT NULL | Response time in milliseconds |
| is_stream | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether the request was streaming |
| input_tokens | INTEGER | NULLABLE | Input tokens (from upstream response, if available) |
| output_tokens | INTEGER | NULLABLE | Output tokens (from upstream response, if available) |
| total_tokens | INTEGER | NULLABLE | Total tokens (from upstream response, if available) |
| request_path | VARCHAR(500) | NOT NULL | Request path (e.g., /v1/chat/completions) |
| error_detail | TEXT | NULLABLE | Error message if request failed |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Request timestamp |

### 8.2 Indexes

```sql
CREATE INDEX idx_request_logs_model_id ON request_logs(model_id);
CREATE INDEX idx_request_logs_provider_type ON request_logs(provider_type);
CREATE INDEX idx_request_logs_created_at ON request_logs(created_at);
CREATE INDEX idx_request_logs_status_code ON request_logs(status_code);
CREATE INDEX idx_request_logs_endpoint_id ON request_logs(endpoint_id);
```

### 8.3 Logging Behavior
- Every proxy request (streaming and non-streaming) is logged after completion
- Token usage is extracted from the upstream response body when available (OpenAI `usage` field, Anthropic `usage` field)
- For streaming requests, token usage is extracted from the final SSE chunk if available
- Logging is non-blocking — failures to log do not affect the proxy response
- No automatic cleanup — logs accumulate (manual DB management for now)
