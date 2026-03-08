# Data Model Document: Prism

Scope: profile-isolated runtime/management model with pricing templates and strict config v2 semantics.

## 1. Entity Relationship Diagram

```
providers (global)
  id PK
  name UNIQUE
  provider_type
  description
  audit_enabled
  audit_capture_bodies
  created_at, updated_at
      | 1:N
      v
model_configs (profile-scoped) <---- self redirect_to (same profile)
  id PK
  profile_id FK -> profiles.id
  provider_id FK -> providers.id
  model_id
  display_name
  model_type (native|proxy)
  redirect_to (model_id in same profile)
  lb_strategy
  failover_recovery_enabled
  failover_recovery_cooldown_seconds
  is_enabled
  created_at, updated_at
  UNIQUE(profile_id, model_id)
      | 1:N
      v
connections (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  model_config_id FK -> model_configs.id
  endpoint_id FK -> endpoints.id
  is_active, priority
  name, custom_headers
  health_status, health_detail, last_health_check
  pricing_template_id FK -> pricing_templates.id (nullable, RESTRICT)
  created_at, updated_at
  INDEX(profile_id, model_config_id, is_active, priority)
  INDEX(pricing_template_id)

profiles
  id PK
  name UNIQUE
  description
  is_active
  version
  deleted_at NULL
  created_at, updated_at
  partial UNIQUE where is_active = TRUE

endpoints (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  name
  base_url
  api_key
  position
  created_at, updated_at
  UNIQUE(profile_id, name)
  INDEX(profile_id, position)

header_blocklist_rules
  id PK
  profile_id FK -> profiles.id NULLABLE
  name
  match_type (exact|prefix)
  pattern
  enabled
  is_system
  created_at, updated_at
  - system rule: is_system = TRUE, profile_id IS NULL
  - user rule:   is_system = FALSE, profile_id IS NOT NULL
  - user UNIQUE(profile_id, match_type, pattern)

user_settings (profile-scoped singleton)
  id PK
  profile_id FK -> profiles.id
  report_currency_code, timezone_preference
  report_currency_symbol
  created_at, updated_at
  UNIQUE(profile_id)

endpoint_fx_rate_settings (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  model_id
  endpoint_id
  fx_rate
  created_at, updated_at
  UNIQUE(profile_id, model_id, endpoint_id)

request_logs (immutable attribution)
  id PK
  profile_id FK -> profiles.id
  model_id, provider_type
  connection_id, endpoint_base_url, endpoint_description
  status_code, response_time_ms, is_stream
  usage token fields
  costing snapshot fields
  request_path, error_detail
  created_at

audit_logs (immutable attribution)
  id PK
  profile_id FK -> profiles.id
  request_log_id FK -> request_logs.id ON DELETE SET NULL
  provider_id FK -> providers.id
  model_id, connection_id, endpoint_base_url, endpoint_description
  request/response payload fields
  is_stream, duration_ms
  created_at
```

## 2. Table Definitions

### 2.1 `providers` (global/shared)

Provider records remain global and are shared across all profiles.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| name | VARCHAR(100) | NOT NULL, UNIQUE | Display name (`OpenAI`, `Anthropic`, `Gemini`) |
| provider_type | VARCHAR(50) | NOT NULL | `openai`, `anthropic`, `gemini` |
| description | TEXT | NULLABLE | Optional description |
| audit_enabled | BOOLEAN | NOT NULL, DEFAULT FALSE | Provider-level audit toggle |
| audit_capture_bodies | BOOLEAN | NOT NULL, DEFAULT TRUE | Provider-level body capture toggle |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

### 2.2 `profiles`

Profiles are isolated configuration namespaces. One profile is active for runtime routing at any time.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| name | VARCHAR(120) | NOT NULL, UNIQUE | Profile name |
| description | TEXT | NULLABLE | Optional description |
| is_active | BOOLEAN | NOT NULL, DEFAULT FALSE | Runtime-active marker |
| version | INTEGER | NOT NULL, DEFAULT 0 | Optimistic concurrency token for activation CAS |
| deleted_at | DATETIME | NULLABLE | Soft-delete marker for inactive profiles |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints and lifecycle rules:
- Exactly one non-deleted profile is active at any time (partial unique index).
- Routine delete is soft-delete (`deleted_at`) and only allowed for inactive profiles.
- Capacity limit: maximum 10 non-deleted profiles (`deleted_at IS NULL`) enforced at application level.

### 2.3 `model_configs` (profile-scoped)

Maps a model ID to a provider and routing behavior within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| provider_id | INTEGER | FK -> providers.id, NOT NULL | Provider reference |
| model_id | VARCHAR(200) | NOT NULL | Model identifier (scoped by profile) |
| display_name | VARCHAR(200) | NULLABLE | Human-readable name |
| model_type | VARCHAR(20) | NOT NULL, DEFAULT 'native' | `native` or `proxy` |
| redirect_to | VARCHAR(200) | NULLABLE | Target native `model_id` in same profile |
| lb_strategy | VARCHAR(50) | NOT NULL, DEFAULT 'single' | `single` or `failover` (native only) |
| failover_recovery_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Failover recovery toggle |
| failover_recovery_cooldown_seconds | INTEGER | NOT NULL, DEFAULT 60 | Cooldown duration (1-3600) |
| is_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Runtime availability |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints:
- `UNIQUE(profile_id, model_id)`.
- Proxy target (`redirect_to`) must resolve to a native model in the same profile.
- Proxy chains are not allowed (proxy cannot point to proxy).

### 2.4 `endpoints` (profile-scoped credentials)

Reusable credential objects scoped to one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Endpoint label |
| base_url | VARCHAR(500) | NOT NULL | Upstream base URL |
| api_key | VARCHAR(500) | NOT NULL | API key |
| position | INTEGER | NOT NULL | Zero-based contiguous ordering index within profile |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints and indexes:
- `UNIQUE(profile_id, name)`.
- `INDEX(profile_id, position)` for ordered reads.

### 2.5 `connections` (profile-scoped routing)

Model-to-endpoint routing objects within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| model_config_id | INTEGER | FK -> model_configs.id, NOT NULL, ON DELETE CASCADE | Parent model config |
| endpoint_id | INTEGER | FK -> endpoints.id, NOT NULL | Referenced endpoint |
| is_active | BOOLEAN | NOT NULL, DEFAULT TRUE | Active routing candidate |
| priority | INTEGER | NOT NULL, DEFAULT 0 | Zero-based contiguous ordering index within `(profile_id, model_config_id)`; lower value = higher failover priority |
| name | TEXT | NULLABLE | Optional connection label |
| custom_headers | TEXT | NULLABLE | JSON headers applied before blocklist filtering |
| health_status | VARCHAR(20) | NOT NULL, DEFAULT 'unknown' | `unknown`, `healthy`, `unhealthy` |
| health_detail | TEXT | NULLABLE | Last health-check detail |
| last_health_check | DATETIME | NULLABLE | Last probe timestamp |
| pricing_template_id | INTEGER | FK -> pricing_templates.id, NULLABLE, ON DELETE RESTRICT | Assigned pricing template |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Indexes include `idx_connections_profile_model_active_priority` for routing lookups by `(profile_id, model_config_id, is_active, priority)` and `idx_connections_pricing_template_id` for template dependency checks.

Connection ordering invariants:
- Priorities are normalized to contiguous `0..N-1` per `(profile_id, model_config_id)`.
- Deterministic reads use `(priority, id)` ordering for both management responses and runtime connection selection.
- Connection create/update contracts do not allow client-written `priority`; ordering changes flow through the dedicated move API.

### 2.6 `pricing_templates` (profile-scoped reusable token pricing)

Reusable token pricing definitions that can be attached to many connections within a profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Template name (profile-unique) |
| description | TEXT | NULLABLE | Optional notes |
| pricing_unit | VARCHAR(20) | NOT NULL, DEFAULT 'PER_1M' | Billing unit |
| pricing_currency_code | VARCHAR(3) | NOT NULL | Template currency code |
| input_price | VARCHAR(20) | NOT NULL | Input token price |
| output_price | VARCHAR(20) | NOT NULL | Output token price |
| cached_input_price | VARCHAR(20) | NULLABLE | Cached input token price |
| cache_creation_price | VARCHAR(20) | NULLABLE | Cache write token price |
| reasoning_price | VARCHAR(20) | NULLABLE | Reasoning token price |
| missing_special_token_price_policy | VARCHAR(20) | NOT NULL, DEFAULT 'MAP_TO_OUTPUT' | `MAP_TO_OUTPUT` or `ZERO_COST` |
| version | INTEGER | NOT NULL, DEFAULT 1 | Auto-incremented on pricing-impacting changes |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraint: `UNIQUE(profile_id, name)`.


### 2.7 `header_blocklist_rules` (mixed scope)

Header blocklist is split between global system rules and profile-scoped user rules.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NULLABLE | NULL for system rules; profile FK for user rules |
| name | VARCHAR(200) | NOT NULL | Rule label |
| match_type | VARCHAR(20) | NOT NULL | `exact` or `prefix` |
| pattern | VARCHAR(200) | NOT NULL | Header match token (case-insensitive) |
| enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Rule enabled flag |
| is_system | BOOLEAN | NOT NULL, DEFAULT FALSE | Protected global rule |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints:
- System rule: `is_system = TRUE` implies `profile_id IS NULL`.
- User rule: `is_system = FALSE` implies `profile_id IS NOT NULL`.
- User rule uniqueness: `UNIQUE(profile_id, match_type, pattern)`.

### 2.8 `user_settings` (profile-scoped singleton)

Per-profile costing/report display preferences.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL, UNIQUE | One row per profile |
| report_currency_code | VARCHAR(3) | NOT NULL, DEFAULT 'USD' | Spending report currency |
| report_currency_symbol | VARCHAR(5) | NOT NULL, DEFAULT '$' | Currency symbol |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |
| timezone_preference | VARCHAR(100) | NULLABLE | Preferred timezone for UI/report rendering |

### 2.9 `endpoint_fx_rate_settings` (profile-scoped)

Custom FX mappings used by costing within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| model_id | VARCHAR(100) | NOT NULL | Model identifier in profile scope |
| endpoint_id | INTEGER | NOT NULL | Endpoint reference in profile scope |
| fx_rate | VARCHAR(20) | NOT NULL | Decimal exchange rate |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraint: `UNIQUE(profile_id, model_id, endpoint_id)`.

### 2.10 `request_logs` (immutable profile attribution)

Telemetry rows for every proxy attempt with immutable profile attribution captured at request start.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| model_id | VARCHAR(200) | NOT NULL | Model ID used for attempt |
| provider_type | VARCHAR(50) | NOT NULL | Provider type |
| connection_id | INTEGER | NULLABLE | Connection used |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Endpoint base URL snapshot |
| endpoint_description | TEXT | NULLABLE | Endpoint description snapshot |
| status_code | INTEGER | NOT NULL | Upstream status code |
| response_time_ms | INTEGER | NOT NULL | Latency in ms |
| is_stream | BOOLEAN | NOT NULL, DEFAULT FALSE | Streaming flag |
| usage + costing snapshot fields | mixed | NULLABLE | Token/cost telemetry and pricing snapshots |
| request_path | VARCHAR(500) | NOT NULL | Requested route path |
| error_detail | TEXT | NULLABLE | Error details for failed attempts |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Attempt timestamp |

### 2.11 `audit_logs` (immutable profile attribution)

Audit rows for upstream attempts with immutable profile attribution.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| request_log_id | INTEGER | FK -> request_logs.id, NULLABLE, ON DELETE SET NULL | Optional telemetry linkage |
| provider_id | INTEGER | FK -> providers.id, NOT NULL | Provider reference |
| model_id | VARCHAR(200) | NOT NULL | Model ID |
| connection_id | INTEGER | NULLABLE | Connection snapshot |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Endpoint base URL snapshot |
| endpoint_description | TEXT | NULLABLE | Endpoint description snapshot |
| request_method/request_url/request_headers/request_body | mixed | request fields | Upstream request snapshot |
| response_status/response_headers/response_body | mixed | response fields | Upstream response snapshot |
| is_stream | BOOLEAN | NOT NULL, DEFAULT FALSE | Streaming flag |
| duration_ms | INTEGER | NOT NULL | Request duration |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Audit timestamp |

## 3. Indexes and Constraints (Profile Isolation)

```sql
-- Profiles
CREATE UNIQUE INDEX idx_profiles_single_active ON profiles(is_active) WHERE is_active = TRUE;
CREATE INDEX idx_profiles_not_deleted ON profiles(deleted_at);

-- Scoped uniqueness
CREATE UNIQUE INDEX idx_model_configs_profile_model_id ON model_configs(profile_id, model_id);
CREATE UNIQUE INDEX idx_endpoints_profile_name ON endpoints(profile_id, name);
CREATE UNIQUE INDEX idx_endpoint_fx_profile_model_endpoint ON endpoint_fx_rate_settings(profile_id, model_id, endpoint_id);
CREATE UNIQUE INDEX idx_user_settings_profile_id ON user_settings(profile_id);

-- Performance indexes
CREATE INDEX idx_model_configs_profile_model_enabled ON model_configs(profile_id, model_id, is_enabled);
CREATE INDEX idx_endpoints_profile_position ON endpoints(profile_id, position);
CREATE INDEX idx_connections_profile_model_active_priority ON connections(profile_id, model_config_id, is_active, priority);
CREATE INDEX idx_connections_pricing_template_id ON connections(pricing_template_id);
CREATE INDEX idx_request_logs_profile_created_at ON request_logs(profile_id, created_at);
CREATE INDEX idx_audit_logs_profile_created_at ON audit_logs(profile_id, created_at);
CREATE INDEX idx_endpoint_fx_profile_model_endpoint_lookup ON endpoint_fx_rate_settings(profile_id, model_id, endpoint_id);
```

## 4. Relationship and Ownership Rules

- `providers` are global and shared across all profiles.
- `profiles` own all scoped entities: `model_configs`, `endpoints`, `connections`, `user_settings`, `endpoint_fx_rate_settings`, user `header_blocklist_rules`.
- `request_logs` and `audit_logs` keep immutable `profile_id` attribution and are not rewritten when active profile changes.
- Cross-profile resource lookups are treated as not found (`404`) under effective profile scope.
- Connection create/update must enforce profile consistency between model and endpoint references.

## 5. Deletion and Retention Semantics

- Routine profile deletion (`DELETE /api/profiles/{id}`) is soft-delete of inactive profile (`deleted_at` set).
- Active profile deletion is rejected.
- Profile-scoped config entities are removable through explicit profile-targeted replace/purge workflows.
- Historical telemetry/audit retention is independent; routine profile delete does not erase historical attribution rows.

## 6. Runtime Isolation Notes

- Proxy routing always resolves against the active profile snapshot.
- Failover recovery in memory is namespaced by `(profile_id, connection_id)` to avoid cross-profile cooldown leakage.
- Runtime failover recovery state tracks `consecutive_failures`, `blocked_until_mono`, `last_cooldown_seconds`, `last_failure_kind`, and `probe_eligible_logged` for each `(profile_id, connection_id)` entry.
- Failures are classified as `transient_http`, `auth_like`, `connect_error`, or `timeout`; auth-like failures use dedicated cooldown settings while transient failures use threshold/backoff/jitter policy.
- Non-failover client errors do not force-clear existing recovery state; successful responses (`2xx`/`3xx`) clear recovery state for the connection.
- Header blocklist at runtime is resolved as: all enabled system rules + enabled user rules for active profile.

## 7. Config Import/Export Versioning

- Canonical export format is config version `2` with explicit IDs and pricing template definitions.
- Import accepts `v2` only and validates strict schema compatibility, including template references.
- Import replace semantics are profile-targeted by effective profile context and do not globally delete other profiles.


## 8. Revision Provenance and Invariant Notes (Profile Isolation, 2026-02-28)

Source inputs: `docs/PROFILE_ISOLATION_REQUIREMENTS.md`, `docs/PROFILE_ISOLATION_UPGRADE_PLAN.md`, `docs/PROFILE_ISOLATION_FRONTEND_ITERATION_PLAN.md`, `docs/PROFILE_ISOLATION_RESEARCH_REFERENCES.md`, and `docs/PROFILE_ISOLATION_SUPPORTING_EVIDENCE.md`.


This appendix captures the implemented revision mapping for the profile-isolated schema described above.

Commit alignment:

- Backend `c0f2daa`: establishes profile ownership boundaries for scoped entities, immutable `profile_id` attribution in telemetry/audit rows, and profile-aware routing/import semantics that depend on these relational guarantees.
- Frontend `02c70ce`: consumes profile-scoped data contracts through selected-profile API context and active-profile runtime activation UX.
- Root/docs `f6f0106`: synchronized architecture and operational documentation to current migration/bootstrap behavior.

Schema invariants emphasized by the profile-isolation requirement set:

- Exactly one non-deleted active profile at a time (partial active uniqueness).
- Capacity limit enforced at application layer: maximum 10 rows where `deleted_at IS NULL`.
- Scoped uniqueness is composite by `profile_id` for model IDs, endpoint names, and FX mapping keys.
- Runtime ownership checks must prevent cross-profile joins for model/endpoint/connection mutation paths.
- Observability rows keep immutable profile attribution after profile activation changes.
- In-memory failover state must be namespaced by `(profile_id, connection_id)` so DB and runtime isolation semantics remain consistent.

Requirement trace anchors: `FR-001`, `FR-002`, `FR-005`, `FR-008`, `FR-009`, `FR-011`.
