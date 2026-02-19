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
│ created_at       │       │ lb_strategy          │       │ is_active            │
│ updated_at       │       │ is_enabled           │       │ priority             │
└──────────────────┘       │ created_at           │       │ description          │
                           │ updated_at           │       │ health_status        │
                           └──────────────────────┘       │ success_count        │
                                                          │ failure_count        │
                                                          │ last_used_at         │
                                                          │ created_at           │
                                                          │ updated_at           │
                                                          └──────────────────────┘
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

Maps a model ID string to a provider and load balancing configuration.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| provider_id | INTEGER | FK → providers.id, NOT NULL | Associated provider |
| model_id | VARCHAR(200) | NOT NULL, UNIQUE | Model identifier (e.g., "gpt-4o", "claude-sonnet-4-20250514") |
| display_name | VARCHAR(200) | NULLABLE | Human-friendly name |
| lb_strategy | VARCHAR(50) | NOT NULL, DEFAULT 'single' | Load balancing: `single`, `round_robin`, `failover` |
| is_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Whether this model is available for proxying |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

### 2.3 `endpoints`

Stores BaseURL + APIKey combinations for a model configuration.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| model_config_id | INTEGER | FK → model_configs.id, NOT NULL, ON DELETE CASCADE | Parent model config |
| base_url | VARCHAR(500) | NOT NULL | API base URL (e.g., "https://api.openai.com") |
| api_key | VARCHAR(500) | NOT NULL | API key for this endpoint |
| is_active | BOOLEAN | NOT NULL, DEFAULT TRUE | Whether this endpoint is selected for use |
| priority | INTEGER | NOT NULL, DEFAULT 0 | Priority for failover (lower = higher priority) |
| description | TEXT | NULLABLE | Optional label (e.g., "Production key", "Backup key") |
| health_status | VARCHAR(20) | NOT NULL, DEFAULT 'unknown' | `healthy`, `unhealthy`, `unknown` |
| success_count | INTEGER | NOT NULL, DEFAULT 0 | Cumulative successful requests |
| failure_count | INTEGER | NOT NULL, DEFAULT 0 | Cumulative failed requests |
| last_used_at | DATETIME | NULLABLE | Last time this endpoint was used |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

## 3. Indexes

```sql
CREATE UNIQUE INDEX idx_model_configs_model_id ON model_configs(model_id);
CREATE INDEX idx_model_configs_provider_id ON model_configs(provider_id);
CREATE INDEX idx_endpoints_model_config_id ON endpoints(model_config_id);
CREATE INDEX idx_endpoints_is_active ON endpoints(is_active);
```

## 4. Relationships

- `providers` 1:N `model_configs` — One provider can have many model configurations
- `model_configs` 1:N `endpoints` — One model can have many BaseURL/APIKey combinations
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
- On failure, next endpoint is tried
- Failed endpoint marked `health_status = 'unhealthy'`
- Unhealthy endpoints retried after cooldown period (60s default)
