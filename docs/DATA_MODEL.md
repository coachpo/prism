# Data Model Document: Prism

## 1. Entity Relationship Diagram

```
┌──────────────────────┐       ┌──────────────────────┐       ┌──────────────────────┐
│      providers       │       │    model_configs     │       │      endpoints       │
├──────────────────────┤       ├──────────────────────┤       ├──────────────────────┤
│ id (PK)              │◀──┐   │ id (PK)              │       │ id (PK)              │
│ name                 │   └───│ provider_id (FK)     │       │ name                 │
│ provider_type        │       │ model_id (UNIQUE)    │       │ base_url             │
│ description          │       │ display_name         │       │ api_key              │
│ audit_enabled        │       │ model_type           │       │ created_at           │
│ audit_capture_bodies │       │ redirect_to          │       │ updated_at           │
│ created_at           │       │ lb_strategy          │       └──────────────────────┘
│ updated_at           │       │ is_enabled           │                  ▲
└──────┬───────────────┘       │ created_at           │                  │
       │                       │ updated_at           │                  │
       │                       └──────────────────────┘                  │
       │                                │                                │
       │                                │ redirect_to (self-ref)         │
       │                                └──────▶ model_configs.model_id  │
       │                                                                 │
       │                ┌──────────────────────┐       ┌─────────────────┴────┐
       │                │    request_logs      │       │     connections      │
       │                ├──────────────────────┤       ├──────────────────────┤
       └────────────────│ model_id             │       │ id (PK)              │
                        │ provider_type        │◀──────│ model_config_id (FK) │
                        │ connection_id        │       │ endpoint_id (FK)     │
                        │ endpoint_base_url    │       │ is_active            │
                        │ endpoint_description │       │ priority             │
                        │ status_code          │       │ description          │
                        │ response_time_ms     │       │ custom_headers       │
                        │ is_stream            │◀──┐   │ health_status        │
                        │ input_tokens         │   │   │ last_health_check    │
                        │ output_tokens        │   │   │ pricing_enabled      │
                        │ total_tokens         │   │   │ pricing_unit         │
                        │ success_flag         │   │   │ pricing_currency_code│
                        │ billable_flag        │   │   │ input_price        │
                        │ priced_flag          │   │   │ output_price       │
                        │ unpriced_reason      │   │   │ cached_input_price   │
                        │ cache_read_input_tokens ││   │ cache_creation_price │
                        │ cache_creation_input_tokens ││ reasoning_price      │
                        │ reasoning_tokens     │   │   │ missing_special_token_price_policy │
                        │ input_cost_micros    │   │   │ pricing_config_version │
                        │ output_cost_micros   │   │   │ created_at           │
                        │ cache_read_input_cost_micros ││ updated_at           │
                        │ cache_creation_input_cost_micros │└──────────────────────┘
                        │ reasoning_cost_micros│   │
                        │ total_cost_original_micros │ ┌──────────────────────┐
                        │ total_cost_user_currency_micros │      audit_logs      │
                        │ currency_code_original │     ├──────────────────────┤
                        │ report_currency_code │       │ request_log_id (FK)  │
                        │ report_symbol        │       │ id (PK)              │
                        │ fx_rate_used         │   ┌──▶│ provider_id (FK)     │
                        │ fx_rate_source       │   │   │ model_id             │
                        │ pricing_snapshot_*   │   │   │ connection_id        │
                        │ pricing_config_version_used ││ endpoint_base_url    │
                        │ request_path         │   │   │ endpoint_description │
                        │ error_detail         │   │   │ request_method       │
                        │ created_at           │   │   │ request_url          │
                        └──────────────────────┘   │   │ request_headers      │
                                                   │   │ request_body         │
                                                   │   │ response_status      │
                                                   │   │ response_headers     │
                                                   │   │ response_body        │
                                                   │   │ is_stream            │
                                                   │   │ duration_ms          │
                                                   │   │ created_at           │
                                                   └───└──────────────────────┘

┌──────────────────────────┐       ┌──────────────────────────┐
│  header_blocklist_rules  │       │      user_settings       │
├──────────────────────────┤       ├──────────────────────────┤
│ id (PK)                  │       │ id (PK)                  │
│ name                     │       │ report_currency_code     │
│ match_type               │       │ report_currency_symbol   │
│ pattern                  │       │ created_at               │
│ enabled                  │       │ updated_at               │
│ is_system                │       └──────────────────────────┘
│ created_at               │
│ updated_at               │       ┌──────────────────────────┐
└──────────────────────────┘       │ connection_fx_rate_settings│
                                   ├──────────────────────────┤
                                   │ id (PK)                  │
                                   │ model_id                 │
                                   │ connection_id            │
                                   │ fx_rate                  │
                                   │ created_at               │
                                   │ updated_at               │
                                   └──────────────────────────┘
```

## 2. Table Definitions

### 2.1 `providers`

Represents an LLM API provider type.

| Column               | Type         | Constraints             | Description                                                                      |
| -------------------- | ------------ | ----------------------- | -------------------------------------------------------------------------------- |
| id                   | INTEGER      | PK, AUTOINCREMENT       | Unique identifier                                                                |
| name                 | VARCHAR(100) | NOT NULL, UNIQUE        | Display name (e.g., "OpenAI")                                                    |
| provider_type        | VARCHAR(50)  | NOT NULL                | Enum: `openai`, `anthropic`, `gemini`                                            |
| description          | TEXT         | NULLABLE                | Optional description                                                             |
| audit_enabled        | BOOLEAN      | NOT NULL, DEFAULT FALSE | Whether to record audit logs for this provider's proxy requests                  |
| audit_capture_bodies | BOOLEAN      | NOT NULL, DEFAULT TRUE  | Whether request/response bodies are stored for audited requests on this provider |
| created_at           | DATETIME     | NOT NULL, DEFAULT NOW   | Creation timestamp                                                               |
| updated_at           | DATETIME     | NOT NULL, DEFAULT NOW   | Last update timestamp                                                            |

Seed data:

```sql
INSERT INTO providers (name, provider_type, description) VALUES
  ('OpenAI', 'openai', 'OpenAI API (GPT models)'),
  ('Anthropic', 'anthropic', 'Anthropic API (Claude models)'),
  ('Gemini', 'gemini', 'Gemini API');
```

### 2.2 `model_configs`

Maps a model ID string to a provider and load balancing configuration. Supports two model types: native (real model with connections) and proxy (alias that forwards to a native model).

| Column       | Type         | Constraints                 | Description                                                                                                   |
| ------------ | ------------ | --------------------------- | ------------------------------------------------------------------------------------------------------------- |
| id           | INTEGER      | PK, AUTOINCREMENT           | Unique identifier                                                                                             |
| provider_id  | INTEGER      | FK → providers.id, NOT NULL | Associated provider                                                                                           |
| model_id     | VARCHAR(200) | NOT NULL, UNIQUE            | Model identifier (e.g., "gpt-4o", "claude-sonnet-4-5")                                                        |
| display_name | VARCHAR(200) | NULLABLE                    | Human-friendly name                                                                                           |
| model_type   | VARCHAR(20)  | NOT NULL, DEFAULT 'native'  | Model type: `native` or `proxy`                                                                               |
| redirect_to  | VARCHAR(200) | NULLABLE                    | Target model_id for proxy models (must be a native model of the same provider)                                |
| lb_strategy | VARCHAR(50) | NOT NULL, DEFAULT 'single' | Load balancing: `single`, `failover` (only applies to native models; ignored for proxy models) |
| failover_recovery_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Whether automatic failover recovery is enabled (only applies to native models with `lb_strategy='failover'`) |
| failover_recovery_cooldown_seconds | INTEGER | NOT NULL, DEFAULT 60 | Cooldown period (1-3600 seconds) before retrying a failed connection (only applies when recovery is enabled) |
| is_enabled   | BOOLEAN      | NOT NULL, DEFAULT TRUE      | Whether this model is available for proxying                                                                  |
| created_at   | DATETIME     | NOT NULL, DEFAULT NOW       | Creation timestamp                                                                                            |
| updated_at   | DATETIME     | NOT NULL, DEFAULT NOW       | Last update timestamp                                                                                         |

**Constraints:**

- `model_id` is globally unique across all model types
- When `model_type = 'proxy'`: `redirect_to` must reference an existing native model's `model_id` with the same `provider_id`
- When `model_type = 'native'`: `redirect_to` must be NULL
- Proxy models cannot have connections (enforced at application level)
- Proxy chains are not allowed (proxy → proxy is invalid)
- Proxy models do not use load balancing (lb_strategy is ignored)

### 2.3 `endpoints`

Stores global reusable credentials (BaseURL + APIKey).

| Column            | Type         | Constraints                                        | Description                                                                                                                                     |
| ----------------- | ------------ | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| id                | INTEGER      | PK, AUTOINCREMENT                                  | Unique identifier                                                                                                                               |
| name              | VARCHAR(200) | NOT NULL                                           | Human-friendly name for the credential                                                                                                          |
| base_url          | VARCHAR(500) | NOT NULL                                           | API base URL (e.g., "https://api.openai.com")                                                                                                   |
| api_key           | VARCHAR(500) | NOT NULL                                           | API key for this endpoint                                                                                                                       |
| created_at        | DATETIME     | NOT NULL, DEFAULT NOW                              | Creation timestamp                                                                                                                              |
| updated_at        | DATETIME     | NOT NULL, DEFAULT NOW                              | Last update timestamp                                                                                                                           |

### 2.4 `connections`

Stores model-scoped routing, costing, and health configuration, referencing a global endpoint.

| Column            | Type         | Constraints                                        | Description                                                                                                                                     |
| ----------------- | ------------ | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| id                | INTEGER      | PK, AUTOINCREMENT                                  | Unique identifier                                                                                                                               |
| model_config_id   | INTEGER      | FK → model_configs.id, NOT NULL, ON DELETE CASCADE | Parent model config                                                                                                                             |
| endpoint_id       | INTEGER      | FK → endpoints.id, NOT NULL                        | Referenced global endpoint                                                                                                                      |
| is_active         | BOOLEAN      | NOT NULL, DEFAULT TRUE                             | Whether this connection is selected for use                                                                                                     |
| priority          | INTEGER      | NOT NULL, DEFAULT 0                                | Priority for failover (lower = higher priority)                                                                                                 |
| description       | TEXT         | NULLABLE                                           | Optional label (e.g., "Production key", "Backup key")                                                                                           |
| custom_headers    | TEXT         | NULLABLE                                           | JSON object of custom HTTP headers to append to upstream requests. NULL or empty means no custom headers.                                       |
| health_status     | VARCHAR(20)  | NOT NULL, DEFAULT 'unknown'                        | Health status: `unknown`, `healthy`, `unhealthy`                                                                                                |
| health_detail     | TEXT         | NULLABLE                                           | Detail message from last health check (e.g., error message from upstream)                                                                       |
| last_health_check | DATETIME     | NULLABLE                                           | Timestamp of last health check                                                                                                                  |
| pricing_enabled   | BOOLEAN      | NOT NULL, DEFAULT FALSE                            | Whether token costing is enabled for this connection                                                                                            |
| pricing_unit      | VARCHAR(10)  | NULLABLE                                           | Unit for pricing: `PER_1K` or `PER_1M`                                                                                                          |
| pricing_currency_code | VARCHAR(3) | NULLABLE                                           | Currency code for prices (e.g., "USD")                                                                                                          |
| input_price       | VARCHAR(20)  | NULLABLE                                           | Price per unit for input tokens (decimal string)                                                                                                |
| output_price      | VARCHAR(20)  | NULLABLE                                           | Price per unit for output tokens (decimal string)                                                                                               |
| cached_input_price | VARCHAR(20) | NULLABLE                                           | Price per unit for cached input tokens (decimal string)                                                                                         |
| cache_creation_price | VARCHAR(20) | NULLABLE                                           | Price per unit for cache creation tokens (decimal string)                                                                                       |
| reasoning_price   | VARCHAR(20)  | NULLABLE                                           | Price per unit for reasoning tokens (decimal string)                                                                                            |
| missing_special_token_price_policy | VARCHAR(20) | NOT NULL, DEFAULT 'MAP_TO_OUTPUT' | Policy for missing special token prices: `MAP_TO_OUTPUT`, `ZERO_COST`                                                                           |
| pricing_config_version | INTEGER   | NOT NULL, DEFAULT 0                                | Incremental version of the pricing configuration                                                                                               |
| created_at        | DATETIME     | NOT NULL, DEFAULT NOW                              | Creation timestamp                                                                                                                              |
| updated_at        | DATETIME     | NOT NULL, DEFAULT NOW                              | Last update timestamp                                                                                                                           |

**Constraints:**
- `UNIQUE(model_config_id, endpoint_id)`: A model can only have one connection to a specific endpoint.

### 2.5 `header_blocklist_rules`

Stores rules for blocking specific HTTP headers from being sent to upstream providers.

| Column     | Type         | Constraints             | Description                                                                 |
| ---------- | ------------ | ----------------------- | --------------------------------------------------------------------------- |
| id         | INTEGER      | PK, AUTOINCREMENT       | Unique identifier                                                           |
| name       | VARCHAR(200) | NOT NULL                | Rule name/description                                                       |
| match_type | VARCHAR(20)  | NOT NULL                | Match strategy: `exact` or `prefix`                                         |
| pattern    | VARCHAR(200) | NOT NULL                | Header name pattern to match (case-insensitive)                             |
| enabled    | BOOLEAN      | NOT NULL, DEFAULT TRUE  | Whether the rule is active                                                  |
| is_system  | BOOLEAN      | NOT NULL, DEFAULT FALSE | Whether this is a protected system rule (not deletable or pattern-editable) |
| created_at | DATETIME     | NOT NULL, DEFAULT NOW   | Creation timestamp                                                          |
| updated_at | DATETIME     | NOT NULL, DEFAULT NOW   | Last update timestamp                                                       |

**Constraints:**

- `UNIQUE(match_type, pattern)`: Prevents duplicate rules for the same pattern.
- Prefix patterns must end with `-` (enforced at application level).

### 2.6 `user_settings`

Stores global application settings for the user.

| Column                 | Type       | Constraints             | Description                                     |
| ---------------------- | ---------- | ----------------------- | ----------------------------------------------- |
| id                     | INTEGER    | PK, AUTOINCREMENT       | Unique identifier                               |
| report_currency_code   | VARCHAR(3) | NOT NULL, DEFAULT 'USD' | Default currency for spending reports           |
| report_currency_symbol | VARCHAR(5) | NOT NULL, DEFAULT '$'   | Symbol for the report currency                  |
| created_at             | DATETIME   | NOT NULL, DEFAULT NOW   | Creation timestamp                              |
| updated_at             | DATETIME   | NOT NULL, DEFAULT NOW   | Last update timestamp                           |

### 2.7 `connection_fx_rate_settings`

Stores custom foreign exchange rates for specific model/connection combinations.

| Column      | Type         | Constraints                 | Description                                     |
| ----------- | ------------ | --------------------------- | ----------------------------------------------- |
| id          | INTEGER      | PK, AUTOINCREMENT           | Unique identifier                               |
| model_id    | VARCHAR(100) | NOT NULL                    | Model ID the rate applies to                    |
| connection_id | INTEGER    | NOT NULL                    | Connection ID the rate applies to               |
| fx_rate     | VARCHAR(20)  | NOT NULL                    | Exchange rate (decimal string)                  |
| created_at  | DATETIME     | NOT NULL, DEFAULT NOW       | Creation timestamp                              |
| updated_at  | DATETIME     | NOT NULL, DEFAULT NOW       | Last update timestamp                           |

**Constraints:**

- `UNIQUE(model_id, connection_id)`: One custom rate per model/connection pair.

## 3. Indexes

```sql
CREATE UNIQUE INDEX idx_model_configs_model_id ON model_configs(model_id);
CREATE INDEX idx_model_configs_provider_id ON model_configs(provider_id);
CREATE INDEX idx_model_configs_model_type ON model_configs(model_type);
CREATE INDEX idx_model_configs_redirect_to ON model_configs(redirect_to);
CREATE INDEX idx_connections_model_config_id ON connections(model_config_id);
CREATE INDEX idx_connections_endpoint_id ON connections(endpoint_id);
CREATE INDEX idx_connections_is_active ON connections(is_active);
CREATE UNIQUE INDEX idx_header_blocklist_rules_match_pattern ON header_blocklist_rules(match_type, pattern);
CREATE INDEX idx_header_blocklist_rules_enabled ON header_blocklist_rules(enabled);
CREATE UNIQUE INDEX idx_connection_fx_rate_settings_mapping ON connection_fx_rate_settings(model_id, connection_id);
CREATE INDEX idx_request_logs_billable_flag ON request_logs(billable_flag);
CREATE INDEX idx_request_logs_priced_flag ON request_logs(priced_flag);
```

## 4. Relationships

- `providers` 1:N `model_configs` — One provider can have many model configurations
- `providers` 1:N `audit_logs` — One provider can have many audit records
- `model_configs` 1:N `connections` — One native model can have many connections (proxy models have zero)
- `endpoints` 1:N `connections` — One global endpoint can be reused across many model connections
- `model_configs` self-reference via `redirect_to` → `model_id` — A proxy model points to a native model
- `request_logs` 1:0..1 `audit_logs` — Each upstream attempt log may have one linked audit log (via `request_log_id`)
- `connections` 1:N `connection_fx_rate_settings` — One connection can have many custom FX rates (one per model)
- `model_configs` 1:N `connection_fx_rate_settings` — One model can have many custom FX rates (one per connection)
- `endpoints` 1:N `connections` — Deleting an endpoint is blocked if any connections reference it
- Cascade delete: Deleting a model_config deletes all its connections

## 5. Load Balancing Behavior

### Strategy: `single`

- Only the connection with `is_active = TRUE` and lowest `priority` is used
- If multiple are active, the lowest priority wins

### Strategy: `failover`

- Connections tried in `priority` order (ascending)
- On failure (HTTP 403, 429, 500, 502, 503, 529, timeout, connection error), next connection is tried
- All connections exhausted -> return 502 to client

### Failover Recovery

When `failover_recovery_enabled = TRUE` for a model:

- Failed connections are temporarily blocked for `failover_recovery_cooldown_seconds`
- After cooldown expires, connections are retried during normal request flow (passive half-open probe)
- Successful probe -> connection marked recovered and returned to rotation
- Failed probe -> connection blocked for another cooldown period
- Recovery state is in-memory (resets on backend restart)
- No background polling - probes happen only during actual requests

## 6. Model Proxy (Alias) Behavior

### Resolution Flow

1. Proxy receives request with `model` field (e.g., `claude-sonnet-4-5`)
2. Look up `model_configs` by `model_id`
3. If `model_type = 'proxy'`, resolve `redirect_to` to find the target native model
4. Use the target native model's connections and provider config for the upstream request
5. The original `model` field in the request body is NOT modified — the gateway is transparent

### Validation Rules

- A proxy model's `provider_id` must match the target native model's `provider_id`
- The target model (`redirect_to`) must exist and be of type `native`
- Circular/chained proxies are rejected at creation/update time
- Load balancing strategy is ignored for proxy models (always uses target model's strategy)

## 7. Health Check Behavior

### Check Process

1. User triggers health check via UI or API
2. Backend sends a real chat completion request using the connection's configured model ID and a simple question ("hi")
3. Uses the same URL-building logic as the proxy engine (`build_upstream_url`) to avoid path duplication
4. Provider-specific requests:
   - **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
   - **Anthropic**: `POST {base_url}/messages` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
5. Response determines status:
   - 2xx → `healthy`
   - 401/403 → `unhealthy` (authentication failed)
   - 429 → `healthy` (rate-limited but connection works)
   - Connection error / timeout → `unhealthy`
   - Other errors → `unhealthy`
6. `health_status`, `health_detail`, and `last_health_check` are updated in the database

## 8. Request Logging (Telemetry)

### 8.1 `request_logs`

Stores telemetry data for every proxy request processed by the gateway.

| Column               | Type         | Constraints             | Description                                                       |
| -------------------- | ------------ | ----------------------- | ----------------------------------------------------------------- |
| id                   | INTEGER      | PK, AUTOINCREMENT       | Unique identifier                                                 |
| model_id             | VARCHAR(200) | NOT NULL                | Model ID from the request                                         |
| provider_type        | VARCHAR(50)  | NOT NULL                | Provider type (openai, anthropic, gemini)                         |
| connection_id        | INTEGER      | NULLABLE                | Connection used (NULL if no connection selected)                  |
| endpoint_base_url    | VARCHAR(500) | NULLABLE                | Base URL of the endpoint used                                     |
| endpoint_description | TEXT         | NULLABLE                | Description/label of the connection used (snapshot at request time) |
| status_code          | INTEGER      | NOT NULL                | HTTP status code returned                                         |
| response_time_ms     | INTEGER      | NOT NULL                | Response time in milliseconds                                     |
| is_stream            | BOOLEAN      | NOT NULL, DEFAULT FALSE | Whether the request was streaming                                 |
| input_tokens         | INTEGER      | NULLABLE                | Input tokens (from upstream response, if available)               |
| output_tokens        | INTEGER      | NULLABLE                | Output tokens (from upstream response, if available)              |
| total_tokens         | INTEGER      | NULLABLE                | Total tokens (from upstream response, if available)               |
| success_flag         | BOOLEAN      | NULLABLE                | Whether the request was successful (2xx)                          |
| billable_flag        | BOOLEAN      | NULLABLE                | Whether the request is considered billable                        |
| priced_flag          | BOOLEAN      | NULLABLE                | Whether cost was successfully calculated                          |
| unpriced_reason      | VARCHAR(50)  | NULLABLE                | Reason if priced_flag is false                                    |
| cache_read_input_tokens | INTEGER   | NULLABLE                | Cached input tokens (read from cache)                             |
| cache_creation_input_tokens | INTEGER | NULLABLE                | Cache creation tokens (written to cache)                          |
| reasoning_tokens     | INTEGER      | NULLABLE                | Reasoning tokens                                                  |

> **Null vs zero semantics:** `NULL` means "no usage data available" (upstream didn't report). `0` means "usage block present but this token type was not used." See `missing_special_token_price_policy` on the `connections` table for how missing prices are resolved.

| input_cost_micros    | BIGINT       | NULLABLE                | Cost of input tokens in original currency (micro-units)           |
| output_cost_micros   | BIGINT       | NULLABLE                | Cost of output tokens in original currency (micro-units)          |
| cache_read_input_cost_micros | BIGINT | NULLABLE                | Cost of cached input tokens in original currency (micro-units)    |
| cache_creation_input_cost_micros | BIGINT | NULLABLE             | Cost of cache creation tokens in original currency (micro-units)  |
| reasoning_cost_micros | BIGINT       | NULLABLE                | Cost of reasoning tokens in original currency (micro-units)       |
| total_cost_original_micros | BIGINT  | NULLABLE                | Total cost in original currency (micro-units)                     |
| total_cost_user_currency_micros | BIGINT | NULLABLE                | Total cost in user's report currency (micro-units)                |
| currency_code_original | VARCHAR(3)  | NULLABLE                | Original currency code from connection pricing                    |
| report_currency_code | VARCHAR(3)   | NULLABLE                | User's report currency code at time of request                   |
| report_currency_symbol | VARCHAR(5)  | NULLABLE                | User's report currency symbol at time of request                 |
| fx_rate_used         | VARCHAR(20)  | NULLABLE                | Exchange rate used for conversion                                 |
| fx_rate_source       | VARCHAR(30)  | NULLABLE                | Source of the FX rate (`CONNECTION_SPECIFIC` or `DEFAULT_1_TO_1`)     |
| pricing_snapshot_unit | VARCHAR(10)  | NULLABLE                | Snapshot of pricing unit used                                     |
| pricing_snapshot_input | VARCHAR(20) | NULLABLE                | Snapshot of input price used                                      |
| pricing_snapshot_output | VARCHAR(20) | NULLABLE                | Snapshot of output price used                                     |
| pricing_snapshot_cache_read_input | VARCHAR(20) | NULLABLE       | Snapshot of cached input price used                               |
| pricing_snapshot_cache_creation_input | VARCHAR(20) | NULLABLE     | Snapshot of cache creation price used                             |
| pricing_snapshot_reasoning | VARCHAR(20) | NULLABLE               | Snapshot of reasoning price used                                  |
| pricing_snapshot_missing_special_token_price_policy | VARCHAR(20) | NULLABLE | Snapshot of missing token policy used                     |
| pricing_config_version_used | INTEGER | NULLABLE                | Version of pricing config used for this log                       |
| request_path         | VARCHAR(500) | NOT NULL                | Request path (e.g., /v1/chat/completions)                         |
| error_detail         | TEXT         | NULLABLE                | Error message if request failed                                   |
| created_at           | DATETIME     | NOT NULL, DEFAULT NOW   | Request timestamp                                                 |

### 8.2 Indexes

```sql
CREATE INDEX idx_request_logs_model_id ON request_logs(model_id);
CREATE INDEX idx_request_logs_provider_type ON request_logs(provider_type);
CREATE INDEX idx_request_logs_created_at ON request_logs(created_at);
CREATE INDEX idx_request_logs_status_code ON request_logs(status_code);
CREATE INDEX idx_request_logs_connection_id ON request_logs(connection_id);
```

### 8.3 Logging Behavior

- Every proxy request (streaming and non-streaming) is logged after completion
- Token usage is extracted from the upstream response body when available
- For streaming requests, token usage is extracted from the final SSE chunk if available
- Logging is non-blocking — failures to log do not affect the proxy response
- Batch deletion supported via `DELETE /api/stats/requests?older_than_days=N`
- Deleting request_logs does NOT delete audit_logs; linked `audit_logs.request_log_id` is set to `NULL`

## 9. Audit Logging

### 9.1 `audit_logs`

Stores full HTTP request/response data for audited proxy requests. Only populated when the provider's `audit_enabled` flag is `true`.

| Column               | Type          | Constraints                                                | Description                                                            |
| -------------------- | ------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------- |
| id                   | INTEGER       | PK, AUTOINCREMENT                                          | Unique identifier                                                      |
| request_log_id       | INTEGER       | FK → request_logs.id, NULLABLE, UNIQUE, ON DELETE SET NULL | Link to the corresponding request_log entry for this upstream attempt  |
| provider_id          | INTEGER       | FK → providers.id, NOT NULL                                | Provider that handled this request                                     |
| model_id             | VARCHAR(200)  | NOT NULL                                                   | Model ID from the request                                              |
| connection_id        | INTEGER       | NULLABLE                                                   | Connection used for this upstream attempt (NULL if no connection selected) |
| endpoint_base_url    | VARCHAR(500)  | NULLABLE                                                   | Base URL of the endpoint used (snapshot at request time)               |
| endpoint_description | TEXT          | NULLABLE                                                   | Description/label of the connection used (snapshot at request time)      |
| request_method       | VARCHAR(10)   | NOT NULL                                                   | HTTP method (POST, GET, etc.)                                          |
| request_url          | VARCHAR(2000) | NOT NULL                                                   | Full upstream URL the request was sent to                              |
| request_headers      | TEXT          | NOT NULL                                                   | JSON object of request headers (sensitive values redacted)             |
| request_body         | TEXT          | NULLABLE                                                   | Request body as text. NULL if no body. Truncated to 64KB.              |
| response_status      | INTEGER       | NOT NULL                                                   | HTTP status code from upstream                                         |
| response_headers     | TEXT          | NULLABLE                                                   | JSON object of response headers (sensitive values redacted)            |
| response_body        | TEXT          | NULLABLE                                                   | Response body as text. NULL for streaming requests. Truncated to 64KB. |
| is_stream            | BOOLEAN       | NOT NULL, DEFAULT FALSE                                    | Whether this was a streaming request                                   |
| duration_ms          | INTEGER       | NOT NULL                                                   | Total request duration in milliseconds                                 |
| created_at           | DATETIME      | NOT NULL, DEFAULT NOW                                      | When the audit record was created                                      |

### 9.2 Indexes

```sql
CREATE INDEX idx_audit_logs_provider_id ON audit_logs(provider_id);
CREATE INDEX idx_audit_logs_model_id ON audit_logs(model_id);
CREATE INDEX idx_audit_logs_response_status ON audit_logs(response_status);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX idx_audit_logs_connection_id ON audit_logs(connection_id);
CREATE INDEX idx_audit_logs_request_log_id ON audit_logs(request_log_id);
```

### 9.3 Audit Behavior

- Only records when `providers.audit_enabled = TRUE` for the request's provider
- One audit row is recorded per upstream attempt (including failover attempts)
- Sensitive header values are redacted before storage
- Body capture is controlled per provider via `providers.audit_capture_bodies`
- Request/response bodies are truncated to 64KB with `[TRUNCATED]` marker
- Streaming response bodies are not recorded (`response_body = NULL`)
- Recording is non-blocking — failures are logged to console but never affect proxy behavior
- Uses a separate DB session for streaming requests
- Batch deletion via `DELETE /api/audit/logs`
- Deleting audit_logs does NOT affect linked request_logs

### 9.4 Redaction Rules

Headers with the following names have their values replaced with `[REDACTED]`:

- `authorization` (value becomes `Bearer [REDACTED]`)
- `x-api-key`
- `x-goog-api-key`
- Any header name containing `key`, `secret`, `token`, or `auth` (case-insensitive)

## 10. Computed Fields (Not Stored)

### 10.1 Connection Success Rate

Computed at query time from `request_logs`, not stored in the `connections` table.

```sql
SELECT
  connection_id,
  COUNT(*) AS total_requests,
  SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END) AS success_count,
  ROUND(
    SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END) * 100.0 / COUNT(*),
    2
  ) AS success_rate
FROM request_logs
WHERE created_at >= :from_time AND created_at <= :to_time
GROUP BY connection_id;
```

- Returns `null` for `success_rate` when `total_requests = 0`
- Default time window: last 24 hours
- Badge color thresholds: ≥98% green, 75-98% yellow, <75% red, N/A gray

### 10.2 Model Health (Aggregated)

Computed by aggregating connection success rates for a model's connections.

- Weighted average: `SUM(connection_success_count) / SUM(connection_total_requests) * 100`
- Returns `null` when no connections have request data
- Included in `ModelConfigListResponse` as `health_success_rate` and `health_total_requests`
- Same badge color thresholds as connection success rate
