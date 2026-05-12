# Data Model Document: Prism

Scope: profile-isolated runtime/management model with pricing templates, vendor metadata, profile-scoped adaptive routing policies, UNLOGGED routing hot state, global sidecar control-plane tables, and the current split-bundle configuration format (`version: 1` profile bundle, `version: 1` vendor catalog bundle).

## 1. Entity Relationship Diagram

```
vendors (global)
  id PK
  name UNIQUE
  key UNIQUE
  description
  icon_key NULLABLE
  created_at, updated_at
  created_at, updated_at
      | 1:N
      v
model_configs (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  vendor_id FK -> vendors.id
  api_family (fixed enum)
  model_id
  display_name
  model_type (native|proxy)
  loadbalance_strategy_id FK -> loadbalance_strategies.id (native only)
  proxy_selection_strategy (proxy only: ordered_fallback|weighted_static|priority_static)
  is_enabled
  created_at, updated_at
  UNIQUE(profile_id, model_id)
      |
      v
model_proxy_targets (profile-scoped proxy metadata)
  id PK
  source_model_config_id FK -> model_configs.id (proxy only)
  target_model_config_id FK -> model_configs.id (native only)
  position
  weight
  target_priority
  UNIQUE(source_model_config_id, position)
  UNIQUE(source_model_config_id, target_model_config_id)
      |
      v
loadbalance_strategies (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  name
  routing_policy JSONB
  created_at, updated_at
  UNIQUE(profile_id, name)
      | 1:N
      v
connections (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  model_config_id FK -> model_configs.id
  endpoint_id FK -> endpoints.id
  is_active, priority
  qps_limit, max_in_flight_non_stream, max_in_flight_stream
  name, custom_headers, openai_probe_endpoint_variant
  health_status, health_detail, last_health_check
  pricing_template_id FK -> pricing_templates.id (nullable, RESTRICT)
  created_at, updated_at
  INDEX(profile_id, model_config_id, is_active, priority)
  INDEX(pricing_template_id)

routing_connection_runtime_state (profile-scoped runtime state, UNLOGGED)
  id PK
  profile_id FK -> profiles.id
  connection_id FK -> connections.id
  window_started_at
  window_request_count
  in_flight_non_stream
  in_flight_stream
  circuit_state, open_until_at, probe_available_at
  live_p95_latency_ms, last_probe_status, last_probe_at
  endpoint_ping_ewma_ms, conversation_delay_ewma_ms
  created_at, updated_at
  UNIQUE(profile_id, connection_id)

routing_connection_runtime_leases (profile-scoped runtime repair table, UNLOGGED)
  lease_token PK
  profile_id FK -> profiles.id
  connection_id FK -> connections.id
  lease_kind (stream|non_stream|half_open_probe)
  expires_at, heartbeat_at
  created_at, updated_at
  INDEX(profile_id, connection_id)
  INDEX(expires_at)

profiles
  id PK
  name UNIQUE
  description
  is_active
  is_default
  is_editable
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
  report_currency_code, report_currency_symbol
  timezone_preference
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

request_logs (partitioned immutable attribution)
  PK (created_at, id)
  profile_id FK -> profiles.id
  model_id, resolved_target_model_id, api_family
  ingress_request_id, attempt_number, provider_correlation_id
  connection_id, endpoint_base_url, endpoint_description
  status_code, response_time_ms, is_stream
  stream_outcome, stream_error_kind, stream_error_detail
  usage token fields
  costing snapshot fields
  request_path, error_detail
  created_at partition key

usage_request_events (partitioned immutable usage attribution)
  PK (created_at, id)
  profile_id FK -> profiles.id
  ingress_request_id UNIQUE per profile
  model_id, resolved_target_model_id, api_family
  endpoint_id, connection_id
  proxy_api_key_id, proxy_api_key_name_snapshot
  status_code, success_flag
  stream_outcome, stream_error_kind
  usage token fields
  costing snapshot fields
  created_at

audit_logs (partitioned immutable attribution)
  PK (created_at, id)
  profile_id FK -> profiles.id
  request_log_id weak request metadata, nullable
  request_log_created_at weak request metadata, nullable
  ingress_request_id weak request metadata, nullable
  vendor_id FK -> vendors.id
  model_id, connection_id, endpoint_base_url, endpoint_description
  request/response payload fields
  is_stream, duration_ms
  created_at partition key

  id PK
  profile_id FK -> profiles.id
  vendor_id FK -> vendors.id
  model_config_id FK -> model_configs.id
  connection_id FK -> connections.id
  endpoint_id
  endpoint_ping_status, endpoint_ping_ms
  conversation_status, conversation_delay_ms
  failure_kind, detail
  checked_at

loadbalance_events (partitioned immutable attribution)
  PK (created_at, id)
  profile_id FK -> profiles.id
  connection_id
  event_type (opened|extended|max_cooldown_strike|banned|probe_eligible|recovered|not_opened)
  failure_kind (transient_http|connect_error|timeout)
  consecutive_failures
  cooldown_seconds, failure_threshold, backoff_multiplier, max_cooldown_seconds
  max_cooldown_strikes, ban_mode, banned_until_at
  blocked_until_mono
  model_id, endpoint_id, vendor_id
  created_at

app_auth_settings (singleton)
  id PK
  singleton_key UNIQUE
  auth_enabled
  username, email, pending_email, password_hash
  email_bound_at, email_verification_code_hash, email_verification_expires_at
  email_verification_attempt_count, must_change_password, last_login_at, token_version
  created_at, updated_at

refresh_tokens
  id PK
  auth_subject_id FK -> app_auth_settings.id
  token_hash UNIQUE
  session_duration, expires_at, rotated_from_id, revoked_at, last_used_at
  user_agent, ip_address
  created_at

password_reset_challenges
  id PK
  auth_subject_id FK -> app_auth_settings.id
  otp_hash
  expires_at, consumed_at, attempt_count
  requested_ip
  created_at

webauthn_challenges
  id PK
  challenge_key UNIQUE
  challenge
  expires_at
  created_at

proxy_api_keys
  id PK
  name, key_prefix UNIQUE, key_hash, last_four
  is_active, expires_at, last_used_at, last_used_ip
  created_by_auth_subject_id FK -> app_auth_settings.id, notes, rotated_from_id
  created_at, updated_at

webauthn_credentials
  id PK
  auth_subject_id FK -> app_auth_settings.id
  credential_id UNIQUE, public_key, sign_count
  device_name, aaguid, transports
  backup_eligible, backup_state
  last_used_at, last_used_ip, created_at, updated_at

sidecar_instances (global)
  id PK
  live lower(name) UNIQUE, live base_url_canonical UNIQUE
  encrypted management_password
  enabled, sync_interval_seconds, request_timeout_seconds
  network policy flags, management_auth_state, sync metadata
      | 1:N observations/actions, 1:1 watchdog policy
      v
  sidecar_auth_snapshots / sidecar_provider_snapshots / sidecar_watchdog_policies / sidecar_watchdog_holds / sidecar_watchdog_pending_actions / sidecar_watchdog_actions
  sidecar_id FK -> sidecar_instances.id
  normalized auth/provider observations plus watchdog policy, holds, pending repair queue, retained history, quota states, and scan runs
```

## 2. Table Definitions

### 2.1 `vendors` (global/shared)

Vendor records remain global and are shared across all profiles.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| name | VARCHAR(100) | NOT NULL, UNIQUE | Display name (`OpenAI`, `Anthropic`, `Gemini`) |
| key | VARCHAR(100) | NOT NULL, UNIQUE | Stable vendor key |
| description | TEXT | NULLABLE | Optional description |
| icon_key | VARCHAR(100) | NULLABLE | Optional presentation-only vendor icon key (`zhipu` for Z.ai, `azure` for Microsoft/Azure) |
| audit_enabled | BOOLEAN | NOT NULL, DEFAULT FALSE | Vendor-level audit toggle |
| audit_capture_bodies | BOOLEAN | NOT NULL, DEFAULT TRUE | Vendor-level body capture toggle |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Lifecycle notes:
- Vendor rows are managed globally from Settings → Global.
- Vendor catalog import/export now lives under `/api/config/vendors/*` and is the authoritative bundle path for shared vendor metadata.
- Profile config import/export now lives under `/api/config/profile/*`; profile bundles resolve vendors by `vendor_key` when present and never mutate existing global vendor metadata from profile bundle hint drift.
- Canonical system vendor keys (`openai`, `anthropic`, `gemini`) surface through the API with a derived `is_readonly` flag and reject identity edits or deletion through `/api/vendors/*`; `is_readonly` is behavior, not a persisted table column.
- `icon_key` is shared global metadata and is presentation-only; runtime routing and compatibility continue to use `api_family` on model rows.
- `model_configs.vendor_id` references these shared rows as optional metadata; deleting a vendor never cascades into model deletion.
- `GET /api/vendors/{id}/models` returns the current profile-scoped referencing model rows as informational delete context.
- `DELETE /api/vendors/{id}` hard-deletes editable vendors and clears `model_configs.vendor_id` plus delete-safe observability vendor foreign keys to `NULL`; readonly system vendors are rejected earlier by the API layer.

### 2.2 `profiles`

Profiles are isolated configuration namespaces. One profile is active for runtime routing at any time.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| name | VARCHAR(120) | NOT NULL, UNIQUE | Profile name |
| description | TEXT | NULLABLE | Optional description |
| is_active | BOOLEAN | NOT NULL, DEFAULT FALSE | Runtime-active marker |
| is_default | BOOLEAN | NOT NULL, DEFAULT FALSE | Seeded default marker |
| is_editable | BOOLEAN | NOT NULL, DEFAULT TRUE | Editable flag; current startup invariants keep the system default profile editable |
| version | INTEGER | NOT NULL, DEFAULT 0 | Optimistic concurrency token for activation CAS |
| deleted_at | DATETIME | NULLABLE | Soft-delete marker for inactive profiles |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints and lifecycle rules:
- Exactly one non-deleted profile is active at any time (partial unique index).
- Startup invariants ensure the single default profile exists and remains editable.
- Routine delete is soft-delete (`deleted_at`) and only allowed for inactive profiles.
- Capacity limit: maximum 10 non-deleted profiles (`deleted_at IS NULL`) enforced at application level.

### 2.3 `model_configs` (profile-scoped)

Maps a model ID to optional vendor metadata, fixed api family, and routing behavior within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| vendor_id | INTEGER | NULLABLE, FK -> vendors.id, ON DELETE SET NULL | Optional vendor metadata reference |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| model_id | VARCHAR(200) | NOT NULL | Model identifier (scoped by profile) |
| display_name | VARCHAR(200) | NULLABLE | Human-readable name |
| model_type | VARCHAR(20) | NOT NULL, DEFAULT 'native' | `native` or `proxy` |
| loadbalance_strategy_id | INTEGER | NULLABLE, FK -> loadbalance_strategies.id | Required for native models; null for proxy models |
| proxy_selection_strategy | VARCHAR(50) | NULLABLE, CHECK IN (`ordered_fallback`, `weighted_static`, `priority_static`) | Required selector for proxy models; null for native models |
| is_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Runtime availability |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints:
- `UNIQUE(profile_id, model_id)`.
- Native models must attach one profile-scoped loadbalance strategy and must keep `proxy_selection_strategy` null.
- Proxy models must not attach a loadbalance strategy and must set `proxy_selection_strategy` to `ordered_fallback`, `weighted_static`, or `priority_static`.
- Proxy models route through rows in `model_proxy_targets` instead of a singular redirect field.
- Proxy targets must resolve to native models in the same profile and same `api_family`.

### 2.3A `model_proxy_targets` (profile-scoped proxy routing metadata)

Proxy-model targets. One proxy model can point to multiple native targets with explicit position, weight, and static-priority metadata.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| source_model_config_id | INTEGER | FK -> model_configs.id, NOT NULL, ON DELETE CASCADE | Proxy model owning the target list |
| target_model_config_id | INTEGER | FK -> model_configs.id, NOT NULL, ON DELETE RESTRICT | Native model target |
| position | INTEGER | NOT NULL, CHECK >= 0 | Zero-based contiguous authoring order; used by `ordered_fallback` and as a tie-breaker |
| weight | INTEGER | NOT NULL, CHECK >= 1 | `weighted_static` target weight |
| target_priority | INTEGER | NOT NULL, CHECK >= 0 | `priority_static` target priority |

Constraints:
- `UNIQUE(source_model_config_id, position)`.
- `UNIQUE(source_model_config_id, target_model_config_id)`.
- Positions are normalized and validated as contiguous `0..N-1` in management contracts.
- Database checks enforce `weight >= 1` and `target_priority >= 0`.
- Go management and config-bundle import validation rejects self-reference, cross-profile targets, cross-api-family targets, and proxy-to-proxy chains; these relationship semantics are not enforced by database triggers.

### 2.4 `loadbalance_strategies` (profile-scoped reusable routing behavior)

Reusable routing strategy objects attached by native models within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Strategy name (profile-unique) |
| strategy_type | VARCHAR(20) | NOT NULL, CHECK IN (`legacy`, `adaptive`) | Strategy family discriminator |
| legacy_strategy_type | VARCHAR(20) | NULLABLE, CHECK IN (`single`, `fill-first`, `round-robin`) | Legacy-only routing subtype |
| auto_recovery | JSONB | NULLABLE | Legacy-only retry/cooldown/ban document |
| routing_policy | JSONB | NULLABLE | Adaptive-only routing document with `routing_objective`, `hedge`, `circuit_breaker`, and `admission` branches |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints and lifecycle rules:
- `UNIQUE(profile_id, name)`.
- Strategy rows are shape-checked: `legacy` rows require `legacy_strategy_type` and `auto_recovery` with no `routing_policy`, while `adaptive` rows require `routing_policy` with no legacy-only fields.
- Effective runtime policy resolves once per request from the attached strategy row.
- The adaptive `circuit_breaker` branch carries failure status codes, threshold/backoff/jitter tuning, and optional ban escalation.
- The selected profile's loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default legacy routing` and `Default adaptive routing` for that profile.
- Strategies cannot be deleted while attached to one or more native models.

### 2.5 `endpoints` (profile-scoped credentials)

Reusable credential objects scoped to one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Endpoint label |
| base_url | VARCHAR(500) | NOT NULL | Upstream base URL |
| api_key | VARCHAR(500) | NOT NULL | Prism-at-rest encrypted endpoint secret |
| position | INTEGER | NOT NULL | Zero-based contiguous ordering index within profile |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints and indexes:
- `UNIQUE(profile_id, name)`.
- `INDEX(profile_id, position)` for ordered reads.
- Profile config export never emits plaintext `api_key`; the `version: 1` profile bundle uses `api_key_secret_ref` plus encrypted `secret_payload.entries[]` instead.
- Endpoints with no upstream credential export `api_key_secret_ref = null` and do not emit a bundle secret entry.

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
| qps_limit | INTEGER | NULLABLE | Per-connection QPS cap; `NULL` means unlimited |
| max_in_flight_non_stream | INTEGER | NULLABLE | Concurrent non-stream request cap; `NULL` means unlimited |
| max_in_flight_stream | INTEGER | NULLABLE | Concurrent stream request cap; `NULL` means unlimited |
| openai_probe_endpoint_variant | VARCHAR(40) | NULLABLE | OpenAI-family probe target and payload variant; `responses_minimal` is the default for OpenAI connections, while non-OpenAI connections persist `NULL` |
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
| timezone_preference | VARCHAR(100) | NULLABLE | Preferred timezone for UI/report rendering |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

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

### 2.10 `request_logs` (partitioned immutable profile attribution)

Telemetry rows for every proxy attempt with immutable profile attribution captured at request start. The table is range-partitioned by UTC `created_at` day. The partition-compatible primary key is `(created_at, id)`, with `id` still sequence-backed for lookup convenience.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| model_id | VARCHAR(200) | NOT NULL | Model ID used for attempt |
| resolved_target_model_id | VARCHAR(200) | NULLABLE | Native target model chosen for a proxy-model request |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| ingress_request_id | VARCHAR(36) | NULLABLE | Prism-generated incoming request grouping ID |
| attempt_number | INTEGER | NULLABLE | Per-ingress attempt order, starting at 1 |
| provider_correlation_id | VARCHAR(255) | NULLABLE | Best-effort provider-visible correlation ID |
| connection_id | INTEGER | NULLABLE | Connection used |
| proxy_api_key_id | INTEGER | NULLABLE | Proxy API key snapshot used for the request |
| proxy_api_key_name_snapshot | VARCHAR(200) | NULLABLE | Display-name snapshot for the proxy key at request time |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Endpoint base URL snapshot |
| endpoint_description | TEXT | NULLABLE | Endpoint description snapshot |
| status_code | INTEGER | NOT NULL | Upstream status code |
| response_time_ms | INTEGER | NOT NULL | Latency in ms |
| is_stream | BOOLEAN | NOT NULL, DEFAULT FALSE | Streaming flag |
| stream_outcome | VARCHAR(50) | NOT NULL, DEFAULT `not_streaming` | Stream classification: `not_streaming`, `completed`, `provider_incomplete`, `client_disconnected`, `upstream_read_error`, `upstream_ended_without_terminal`, or `unknown` |
| stream_error_kind | VARCHAR(50) | NULLABLE | Stream diagnostic kind: `client_write_failed`, `request_context_canceled`, `upstream_read_failed`, or `missing_terminal_event` |
| stream_error_detail | TEXT | NULLABLE | Sanitized request-log-detail-only diagnostic text for stream failures |
| usage + costing snapshot fields | mixed | NULLABLE | Token/cost telemetry and pricing snapshots |
| request_path | VARCHAR(500) | NOT NULL | Requested route path |
| error_detail | TEXT | NULLABLE | Error details for failed attempts |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Attempt timestamp |

Request-log semantics:
- One row is written per upstream attempt, not per incoming runtime request.
- `ingress_request_id` groups the rows created by one incoming runtime request.
- `attempt_number` preserves retry/failover ordering within that group.
- For proxy traffic, `model_id` stays the requested proxy identifier while `resolved_target_model_id` records the chosen native target for that attempt.
- `stream_error_detail` is exposed only by exact request-log detail reads. List and realtime payloads expose `stream_outcome` and `stream_error_kind` without detail text.
- Prism prices only observed usage. `STREAM_USAGE_UNAVAILABLE` marks interrupted or no-terminal stream rows where required tokens are absent; completed streams missing required usage keep `MISSING_TOKEN_USAGE`.

### 2.11 `usage_request_events` (partitioned immutable usage attribution)

Usage-event rows are the finalized source for the unified statistics snapshot. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| ingress_request_id | VARCHAR(36) | NOT NULL, UNIQUE per profile | Incoming request grouping ID preserved for aggregate attribution and cross-table correlation |
| model_id | VARCHAR(200) | NOT NULL | Requested model ID |
| resolved_target_model_id | VARCHAR(200) | NULLABLE | Native target selected for the request |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| endpoint_id | INTEGER | NULLABLE | Endpoint snapshot |
| connection_id | INTEGER | NULLABLE | Connection snapshot |
| proxy_api_key_id | INTEGER | NULLABLE | Proxy API key snapshot |
| proxy_api_key_name_snapshot | VARCHAR(200) | NULLABLE | Proxy key name at event time |
| attempt_count | INTEGER | NOT NULL | Number of upstream attempts that contributed to the finalized event |
| status_code | INTEGER | NOT NULL | HTTP status code |
| success_flag | BOOLEAN | NOT NULL | Success indicator |
| stream_outcome | VARCHAR(50) | NOT NULL, DEFAULT `not_streaming` | Finalized stream classification copied from the contributing request-log attempt |
| stream_error_kind | VARCHAR(50) | NULLABLE | Finalized stream diagnostic kind without detail text |
| usage + costing snapshot fields | mixed | NULLABLE | Token and cost telemetry snapshots |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Event timestamp |

Usage-event semantics:
- One row captures the finalized usage event that feeds the statistics snapshot.
- `ingress_request_id` preserves the stable request-group identifier shared with the attempt-level `request_logs` rows for the same incoming runtime request.
- `proxy_api_key_name_snapshot` preserves display intent even if the key name later changes.
- Usage events keep the final stream outcome and error kind for aggregate explanation, but not `stream_error_detail`.

### 2.12 `audit_logs` (partitioned immutable profile attribution)

Audit rows for upstream attempts with immutable profile attribution. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key. Audit-to-request linkage is weak so audit rows can outlive request-log partitions.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| request_log_id | INTEGER | NULLABLE | Weak request-log identifier retained for historical linking |
| request_log_created_at | DATETIME | NULLABLE | Weak request-log partition key retained for historical linking |
| ingress_request_id | VARCHAR(36) | NULLABLE | Weak incoming request grouping ID retained for correlation |
| vendor_id | INTEGER | NULLABLE, FK -> vendors.id, ON DELETE SET NULL | Optional vendor reference |
| model_id | VARCHAR(200) | NOT NULL | Model ID |
| connection_id | INTEGER | NULLABLE | Connection snapshot |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Endpoint base URL snapshot |
| endpoint_description | TEXT | NULLABLE | Endpoint description snapshot |
| request_method/request_url/request_headers/request_body | mixed | request fields | Upstream request snapshot |
| response_status/response_headers/response_body | mixed | response fields | Upstream response snapshot |
| audit_enabled_at_request | BOOLEAN | NOT NULL | Whether audit was enabled when the request started |
| audit_capture_bodies_at_request | BOOLEAN | NOT NULL | Whether body capture was enabled when the request started |
| request_body_stored | BOOLEAN | NOT NULL | Whether request body content was stored |
| response_body_stored | BOOLEAN | NOT NULL | Whether response body content was stored |
| is_stream | BOOLEAN | NOT NULL, DEFAULT FALSE | Streaming flag |
| duration_ms | INTEGER | NOT NULL | Request duration |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Audit timestamp and partition key |

Audit-link semantics:
- `request_log_id`, `request_log_created_at`, and `ingress_request_id` are retained as weak metadata.
- Request detail linkage can be absent after request-log retention expires before audit-log retention.
- Audit retention and request-log retention are independent global jobs.

### 2.13 `loadbalance_events` (partitioned immutable profile attribution)

Persistent record of failover, recovery, and health transitions. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| connection_id | INTEGER | NOT NULL | Connection ID |
| event_type | VARCHAR(20) | NOT NULL | `opened`, `extended`, `max_cooldown_strike`, `banned`, `probe_eligible`, `recovered`, `not_opened` |
| failure_kind | VARCHAR(20) | NULLABLE | `transient_http`, `connect_error`, `timeout` |
| consecutive_failures | INTEGER | NOT NULL | Failure count |
| cooldown_seconds | NUMERIC | NOT NULL | Applied cooldown |
| failure_threshold | INTEGER | NULLABLE | Threshold snapshot used for the event |
| backoff_multiplier | NUMERIC | NULLABLE | Backoff multiplier snapshot |
| max_cooldown_seconds | INTEGER | NULLABLE | Maximum cooldown snapshot |
| max_cooldown_strikes | INTEGER | NULLABLE | Strike counter snapshot when relevant |
| ban_mode | VARCHAR(20) | NULLABLE | `off`, `temporary`, or `manual` when relevant |
| banned_until_at | DATETIME | NULLABLE | Temporary-ban expiry when relevant |
| blocked_until_mono | NUMERIC | NULLABLE | Monotonic block timestamp |
| model_id | VARCHAR(200) | NULLABLE | Model ID snapshot |
| endpoint_id | INTEGER | NULLABLE | Endpoint ID snapshot |
| vendor_id | INTEGER | NULLABLE, FK -> vendors.id, ON DELETE SET NULL | Optional vendor snapshot |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Event timestamp and partition key |

### 2.14 `log_retention_settings` (global singleton)

Global normal-retention policy for partitioned log tables.

| Column | Type | Constraints | Description |
|---|---|---|---|
| singleton_key | VARCHAR(20) | PK, CHECK = `global` | Singleton row key |
| request_logs_retention_days | INTEGER | NULLABLE | Global request-log retention window |
| audit_logs_retention_days | INTEGER | NULLABLE | Global audit-log retention window |
| statistics_retention_days | INTEGER | NULLABLE | Global `usage_request_events` retention window |
| loadbalance_events_retention_days | INTEGER | NULLABLE | Global load-balance event retention window |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Retention semantics:
- Normal retention is global across all profiles and implemented by durable `log_retention` jobs with `profile_id = 0`.
- Whole child partitions with upper bound `<= cutoff` are dropped. Only the cutoff-overlapping boundary child receives bounded row cleanup and `VACUUM (ANALYZE, PROCESS_TOAST TRUE)`.
- Managed partition diagnostics should read `pg_class`, `pg_inherits`, `pg_total_relation_size`, `pg_relation_size`, and `pg_class.reltoastrelid` so operators can see root, child, and TOAST relations without mutating data.
- The partitioned-log upgrade is a clean break. Old log rows are not preserved.
- `VACUUM FULL`, `CLUSTER`, and `pg_repack` are manual or emergency shrink options only. `pg_repack` is not installed in the default local `postgres:16-alpine` image.

Safe catalog inspection template:

```sql
WITH managed_roots(root_name) AS (
  VALUES
    ('request_logs'),
    ('audit_logs'),
    ('usage_request_events'),
    ('loadbalance_events')
)
SELECT
  parent.relname AS root_relation,
  parent.reltoastrelid::int8 AS root_reltoastrelid,
  pg_total_relation_size(parent.oid) AS root_total_bytes,
  pg_relation_size(parent.oid) AS root_main_bytes,
  child.relname AS child_partition,
  pg_get_expr(child.relpartbound, child.oid) AS child_partition_bound,
  child.reltoastrelid::int8 AS child_reltoastrelid,
  pg_total_relation_size(child.oid) AS child_total_bytes,
  pg_relation_size(child.oid) AS child_main_bytes,
  toast_ns.nspname AS toast_schema,
  toast.relname AS toast_relation,
  COALESCE(pg_total_relation_size(toast.oid), 0) AS toast_total_bytes,
  COALESCE(pg_relation_size(toast.oid), 0) AS toast_main_bytes
FROM managed_roots
JOIN pg_class parent ON parent.relname = managed_roots.root_name
JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
JOIN pg_inherits inheritance ON inheritance.inhparent = parent.oid
JOIN pg_class child ON child.oid = inheritance.inhrelid
JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
LEFT JOIN pg_class toast ON toast.oid = child.reltoastrelid
LEFT JOIN pg_namespace toast_ns ON toast_ns.oid = toast.relnamespace
WHERE parent_ns.nspname = 'public'
  AND child_ns.nspname = 'public'
ORDER BY parent.relname, child.relname;
```

When an operator performs manual bounded deletes on the cutoff-overlapping boundary child, follow with child-only analysis and TOAST processing:

```sql
VACUUM (ANALYZE, PROCESS_TOAST TRUE) public.request_logs_pYYYYMMDD;
```

### 2.14A `sidecar_*` tables (global CLIProxyAPI control plane)

Sidecar tables are global instance state. They are not profile-scoped and do not participate in profile bundle import/export. Migration `000014_cli_proxy_sidecars.sql` creates this domain.

| Table | Purpose |
|---|---|
| `sidecar_instances` | Sidecar registration, canonical base URL, encrypted management password, enabled flag, sync interval, request timeout, network policy flags, management-auth state, pause metadata, and sync timestamps. |
| `sidecar_auth_snapshots` | Normalized latest auth-file observations from CLIProxyAPI `/auth-files`, including status, disabled/unavailable flags, priority, quota/retry metadata, recent requests, model states, redacted snapshot JSON, and observation time. Snapshot quota fields are retained as observed inventory metadata, not watchdog quota authority. |
| `sidecar_provider_snapshots` | Normalized provider inventory observations for Gemini, Claude, Codex, Vertex, and OpenAI-compatible credentials. |
| `sidecar_watchdog_policies` | One per-sidecar watchdog settings row: enabled, failure threshold/window, fallback cooldown, `using_priority`, `quota_exceeded_priority`, `error_priority`, manual-override pause duration, probe batch size, probe timeout seconds, `probe_batch_cooldown_seconds`, `probe_jitter_min_ms`, `probe_jitter_max_ms`, `cooldown_jitter_percent`, quota inventory switches, initial scan switch, rolling refresh switch, rolling refresh age, and latest batch cursor metadata. |
| `sidecar_watchdog_holds` | Active, paused, or released holds created by watchdog reconciliation or operator mutations. |
| `sidecar_watchdog_pending_actions` | Live repair queue for watchdog deprioritize, restore, skip, probe, and quota-hold follow-up work. Rows carry the actionable payload, retry state, claim state, and a soft link to retained action history when one exists. |
| `sidecar_watchdog_actions` | Partitioned retained audit trail for instance CRUD, connection tests, manual sync, operator patches, watchdog deprioritize/restore/skips, probe outcomes, quota hold extensions, and policy updates. |
| `sidecar_watchdog_probe_observations` | Sanitized append-only probe observations from watchdog quota checks, including sidecar, auth id/index, provider key, probe timestamp, probe status, upstream status code, normalized quota result, quota reset, blocking window, safe window summaries, and safe error code. Raw probe requests, raw responses, token material, and provider identity payloads are never stored here. |
| `sidecar_auth_quota_states` | Latest observed quota inventory per sidecar auth id, including optional auth name, provider, snapshot observation time, `quota_band`, `probe_status`, `reason_code`, `quota_reset_at`, `blocking_window`, last observation link, `last_probed_at`, and safe `last_error_code`. Public responses also derive auth index presence, disabled state, priority, and active-hold state. Public bands are limited to `using`, `quota_exceeded`, and `error`. |
| `sidecar_quota_scan_runs` | Asynchronous quota scan progress for initial, manual, and scheduled scans, including status, requester, private scan position, planned count, `using_count`, `quota_exceeded_count`, `error_count`, `skipped_count`, cancellation marker, start/completion timestamps, and safe last error code. |

Ownership notes:
- Active `sidecar_instances` rows are unique on `lower(name)` and `base_url_canonical` among non-deleted registrations.
- Stored management passwords use the backend secret-encryption key and are write-only at the API boundary.
- Snapshot, action, and probe-observation JSON must not persist raw token, secret, password, API-key, authorization, raw provider response, or raw provider identity values.
- The sidecar watchdog treats active `/api-call` probe observations as quota authority. Snapshot `quota_*` fields remain inventory observations only, while `sidecar_auth_quota_states` is the public latest observed quota view.
- Probe observations are retained for 15 days; the watchdog worker removes older rows after reconcile work.
- `sidecar_watchdog_pending_actions` owns live repair state and is not operator history. `sidecar_watchdog_actions` owns retained action history and is managed by global retention through `sidecar_action_history_retention_days`.
- `sidecar_quota_scan_runs` may keep private scan position for resumable work, but public quota scan responses expose progress counters and status rather than internal position.
- Sync and watchdog work is scheduler-owned low-priority background work; request handlers enqueue or trigger bounded service methods rather than owning recurring timers.

### 2.15 `routing_connection_runtime_state` (profile-scoped runtime state, `UNLOGGED`)

Ephemeral hot-state row for per-connection admission, circuit state, and probe-aware runtime signals. This table is intentionally `UNLOGGED`, so it resets after crash or unclean shutdown.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| connection_id | INTEGER | FK -> connections.id, NOT NULL | Connection under adaptive-routing tracking |
| window_started_at | DATETIME | NULLABLE | Current QPS window start |
| window_request_count | INTEGER | NOT NULL, DEFAULT 0 | Requests admitted in current one-second window |
| in_flight_non_stream | INTEGER | NOT NULL, DEFAULT 0 | Current non-stream reservations |
| in_flight_stream | INTEGER | NOT NULL, DEFAULT 0 | Current stream reservations |
| circuit_state | VARCHAR(20) | NOT NULL, DEFAULT `closed` | `closed`, `open`, or `half_open` |
| probe_available_at | DATETIME | NULLABLE | Next synthetic probe eligibility time |
| open_until_at | DATETIME | NULLABLE | Wall-clock circuit-open expiry |
| live_p95_latency_ms | INTEGER | NULLABLE | Passive-request latency signal |
| last_probe_status | VARCHAR(20) | NULLABLE | Latest fused probe status |
| last_probe_at | DATETIME | NULLABLE | Latest probe timestamp |
| endpoint_ping_ewma_ms | NUMERIC | NULLABLE | EWMA endpoint ping signal |
| conversation_delay_ewma_ms | NUMERIC | NULLABLE | EWMA conversation delay signal |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Row creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last mutation timestamp |

Constraints:
- `UNIQUE(profile_id, connection_id)`.
- Counter and strike fields are non-negative.
- `circuit_state` is restricted to `closed`, `open`, or `half_open`.

### 2.16 `routing_connection_runtime_leases` (profile-scoped runtime lease table, `UNLOGGED`)

Ephemeral lease rows used for non-stream attempts, streaming heartbeats, and half-open probes.

| Column | Type | Constraints | Description |
|---|---|---|---|
| lease_token | VARCHAR(64) | PK | Lease identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| connection_id | INTEGER | FK -> connections.id, NOT NULL | Connection under adaptive-routing tracking |
| lease_kind | VARCHAR(20) | NOT NULL | `stream`, `non_stream`, or `half_open_probe` |
| expires_at | DATETIME | NOT NULL | Lease expiry for repair/reconciliation |
| heartbeat_at | DATETIME | NULLABLE | Latest stream or probe heartbeat |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Row creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last mutation timestamp |

Constraints:
- `lease_kind` is restricted to `stream`, `non_stream`, or `half_open_probe`.

### 2.17 `app_auth_settings` (singleton)

Global operator authentication settings and credentials.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| singleton_key | VARCHAR(20) | NOT NULL, UNIQUE | `app` |
| auth_enabled | BOOLEAN | NOT NULL, DEFAULT FALSE | Auth toggle |
| username | VARCHAR(200) | NULLABLE | Operator username |
| email | VARCHAR(320) | NULLABLE | Verified email |
| pending_email | VARCHAR(320) | NULLABLE | Email awaiting OTP confirmation |
| password_hash | TEXT | NULLABLE | Argon2 password hash |
| email_bound_at | DATETIME | NULLABLE | When the verified email was bound |
| email_verification_code_hash | VARCHAR(64) | NULLABLE | Hashed OTP for email verification |
| email_verification_expires_at | DATETIME | NULLABLE | Email verification expiry |
| email_verification_attempt_count | INTEGER | NOT NULL, DEFAULT 0 | Failed email-verification attempts |
| must_change_password | BOOLEAN | NOT NULL, DEFAULT FALSE | First-login/reset follow-up flag |
| last_login_at | DATETIME | NULLABLE | Most recent successful login |
| token_version | INTEGER | NOT NULL, DEFAULT 0 | Global token revocation version |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

### 2.18 `refresh_tokens`

Cookie-backed management sessions with family rotation and revocation.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| auth_subject_id | INTEGER | FK -> app_auth_settings.id, NOT NULL | Singleton operator auth subject |
| token_hash | VARCHAR(64) | NOT NULL, UNIQUE | SHA-256 hash of the refresh token |
| session_duration | VARCHAR(20) | NOT NULL, DEFAULT `7_days` | Requested session lifetime bucket |
| expires_at | DATETIME | NOT NULL | Refresh-token expiry |
| rotated_from_id | INTEGER | FK -> refresh_tokens.id, NULLABLE | Previous token in the family |
| revoked_at | DATETIME | NULLABLE | Revocation timestamp |
| last_used_at | DATETIME | NULLABLE | Most recent redemption time |
| user_agent | TEXT | NULLABLE | Client user-agent snapshot |
| ip_address | VARCHAR(100) | NULLABLE | Client IP snapshot |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |

### 2.18 `proxy_api_keys`

Runtime data-plane credentials.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| name | VARCHAR(200) | NOT NULL | Key label |
| key_prefix | VARCHAR(200) | NOT NULL, UNIQUE | Public prefix |
| key_hash | VARCHAR(64) | NOT NULL | SHA-256 hash |
| last_four | VARCHAR(4) | NOT NULL | Display suffix |
| is_active | BOOLEAN | NOT NULL, DEFAULT TRUE | Active flag |
| expires_at | DATETIME | NULLABLE | Expiration timestamp |
| last_used_at | DATETIME | NULLABLE | Most recent proxy use |
| last_used_ip | VARCHAR(100) | NULLABLE | Most recent proxy client IP |
| created_by_auth_subject_id | INTEGER | FK -> app_auth_settings.id, NULLABLE | Operator who created the key |
| notes | TEXT | NULLABLE | Operator notes |
| rotated_from_id | INTEGER | FK -> proxy_api_keys.id, NULLABLE | Previous key in a rotation chain |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Rotation and expiry semantics:
- `rotated_from_id` preserves predecessor/successor lineage across key rotation instead of mutating one row in place.
- Expired or retired keys remain as historical rows; runtime enforcement uses `is_active` plus `expires_at`, while management list views keep the rows for attribution and lineage.

### 2.19 `password_reset_challenges`

Password-reset OTP challenges for the singleton operator account.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| auth_subject_id | INTEGER | FK -> app_auth_settings.id, NOT NULL | Operator auth subject |
| otp_hash | VARCHAR(64) | NOT NULL | Hashed OTP |
| expires_at | DATETIME | NOT NULL | Challenge expiry |
| consumed_at | DATETIME | NULLABLE | Consumption timestamp |
| attempt_count | INTEGER | NOT NULL, DEFAULT 0 | Failed-attempt counter |
| requested_ip | VARCHAR(100) | NULLABLE | Request origin IP |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |

### 2.21 `webauthn_challenges`

Retained internal challenge storage from the earlier passkey design. Prism's current supported auth surface does not expose active passkey ceremonies.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| challenge_key | VARCHAR(100) | NOT NULL, UNIQUE | Lookup key |
| challenge | BYTEA | NOT NULL | Raw challenge bytes |
| expires_at | DATETIME | NOT NULL | Challenge expiry |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |

### 2.22 `webauthn_credentials`

Retained internal credential storage from the earlier passkey design. Prism's current supported auth surface does not expose active passkey credential management.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| auth_subject_id | INTEGER | FK -> app_auth_settings.id, NOT NULL | Singleton operator auth subject |
| credential_id | BYTEA | NOT NULL, UNIQUE | Raw credential ID |
| public_key | BYTEA | NOT NULL | Credential public key |
| sign_count | BIGINT | NOT NULL, DEFAULT 0 | Authenticator signature counter |
| device_name | VARCHAR(200) | NULLABLE | Operator-provided device label |
| aaguid | BYTEA | NULLABLE | Authenticator AAGUID |
| transports | TEXT[] | NULLABLE | Reported authenticator transports |
| backup_eligible | BOOLEAN | NULLABLE, DEFAULT FALSE | Backup eligibility flag |
| backup_state | BOOLEAN | NULLABLE, DEFAULT FALSE | Current backup/sync state |
| last_used_at | DATETIME | NULLABLE | Most recent assertion time |
| last_used_ip | VARCHAR(45) | NULLABLE | Most recent assertion IP |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

## 3. Indexes and Constraints (Profile Isolation)

```sql
-- Profiles
CREATE UNIQUE INDEX idx_profiles_single_active ON profiles(is_active) WHERE is_active = TRUE;
CREATE UNIQUE INDEX idx_profiles_single_default ON profiles(is_default) WHERE is_default = TRUE;
CREATE INDEX idx_profiles_not_deleted ON profiles(deleted_at);

-- Scoped uniqueness
CREATE UNIQUE INDEX idx_model_configs_profile_model_id ON model_configs(profile_id, model_id);
CREATE UNIQUE INDEX idx_model_proxy_targets_source_position ON model_proxy_targets(source_model_config_id, position);
CREATE UNIQUE INDEX idx_model_proxy_targets_source_target ON model_proxy_targets(source_model_config_id, target_model_config_id);
CREATE UNIQUE INDEX idx_endpoints_profile_name ON endpoints(profile_id, name);
CREATE UNIQUE INDEX idx_endpoint_fx_profile_model_endpoint ON endpoint_fx_rate_settings(profile_id, model_id, endpoint_id);
CREATE UNIQUE INDEX idx_user_settings_profile_id ON user_settings(profile_id);

-- Performance indexes
CREATE INDEX idx_model_configs_profile_model_enabled ON model_configs(profile_id, model_id, is_enabled);
CREATE INDEX idx_model_proxy_targets_target_model ON model_proxy_targets(target_model_config_id);
CREATE INDEX idx_endpoints_profile_position ON endpoints(profile_id, position);
CREATE INDEX idx_connections_profile_model_active_priority ON connections(profile_id, model_config_id, is_active, priority);
CREATE INDEX idx_connections_pricing_template_id ON connections(pricing_template_id);
CREATE INDEX idx_request_logs_profile_created_at ON request_logs(profile_id, created_at);
CREATE INDEX idx_request_logs_ingress_request_id ON request_logs(ingress_request_id);
CREATE INDEX idx_audit_logs_profile_created_at ON audit_logs(profile_id, created_at);
CREATE INDEX idx_loadbalance_events_profile_created ON loadbalance_events(profile_id, created_at);
CREATE INDEX idx_loadbalance_events_connection ON loadbalance_events(connection_id, created_at);
CREATE INDEX idx_loadbalance_events_event_type ON loadbalance_events(event_type);
CREATE INDEX idx_connection_limiter_state_profile_connection ON connection_limiter_state(profile_id, connection_id);
CREATE INDEX idx_connection_limiter_leases_profile_connection ON connection_limiter_leases(profile_id, connection_id);
CREATE INDEX idx_connection_limiter_leases_expires_at ON connection_limiter_leases(expires_at);
CREATE INDEX idx_endpoint_fx_profile_model_endpoint_lookup ON endpoint_fx_rate_settings(profile_id, model_id, endpoint_id);

-- Auth and retained passkey-era tables
CREATE INDEX idx_refresh_tokens_revoked_at ON refresh_tokens(revoked_at);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
CREATE INDEX idx_proxy_api_keys_is_active ON proxy_api_keys(is_active);
CREATE INDEX idx_password_reset_challenges_expires_at ON password_reset_challenges(expires_at);
CREATE INDEX idx_password_reset_challenges_consumed_at ON password_reset_challenges(consumed_at);
CREATE INDEX idx_webauthn_challenges_expires_at ON webauthn_challenges(expires_at);
CREATE INDEX idx_webauthn_challenges_challenge_key ON webauthn_challenges(challenge_key);
CREATE INDEX idx_webauthn_credentials_auth_subject ON webauthn_credentials(auth_subject_id);
CREATE INDEX idx_webauthn_credentials_last_used ON webauthn_credentials(last_used_at);
```

Sidecar uniqueness and indexes are defined in `000014_cli_proxy_sidecars.sql`; they cover active sidecar names and canonical URLs, per-sidecar snapshot lookups, active watchdog holds, and action-history ordering.

## 4. Relationship and Ownership Rules

- `vendors` are global and shared across all profiles.
- `model_configs` reference shared vendor rows without vendor-owned delete cascade semantics; deleting a vendor must not delete profile-scoped model rows.
- `profiles` own all scoped entities: `model_configs`, `endpoints`, `connections`, `user_settings`, `endpoint_fx_rate_settings`, user `header_blocklist_rules`.
- `app_auth_settings` is the singleton auth root for `refresh_tokens`, `proxy_api_keys`, and `password_reset_challenges`; retained `webauthn_credentials` rows remain schema-level historical state rather than an active supported workflow surface.
- `sidecar_instances` is a global control-plane root for sidecar snapshots, watchdog policies, holds, and actions; it is not owned by a profile.
- `request_logs`, `audit_logs`, and `loadbalance_events` keep immutable `profile_id` attribution and are not rewritten when active profile changes.
- `request_logs.ingress_request_id` is the canonical operator drill-in key for grouped request investigation.
- `connection_limiter_state` and `connection_limiter_leases` are profile-scoped runtime state and intentionally `UNLOGGED`; operators accept reset-on-crash semantics.
- Cross-profile resource lookups are treated as not found (`404`) under effective profile scope.
- Connection create/update must enforce profile consistency between model and endpoint references.

## 5. Deletion and Retention Semantics

- Routine profile deletion (`DELETE /api/profiles/{id}`) is soft-delete of inactive profile (`deleted_at` set).
- Active profile deletion is rejected.
- Vendor deletion hard-deletes the shared vendor row and nulls `model_configs.vendor_id` plus delete-safe observability vendor foreign keys instead of rejecting the delete or cascading to model rows.
- Profile-scoped config entities are removable through explicit profile-targeted replace/purge workflows.
- Historical telemetry/audit retention is independent; routine profile delete does not erase historical attribution rows.

## 6. Runtime Isolation Notes

- Proxy routing always resolves against the active profile snapshot.
- Runtime routing state is persisted in profile-scoped hot tables and namespaced by `(profile_id, connection_id)` so current state, cooldown, and ban decisions do not leak across profiles.
- Ban escalation state stays with the same per-connection runtime row and tracks the resolved cooldown and ban fields together.
- Connection limiter state is persisted in profile-scoped `UNLOGGED` hot tables plus lease rows and remains namespaced by `(profile_id, connection_id)` to avoid cross-profile admission leakage.
- Runtime current-state rows track `consecutive_failures`, `blocked_until_at`, `last_cooldown_seconds`, `last_failure_kind`, `max_cooldown_strikes`, `ban_mode`, `banned_until_at`, and `probe_eligible_logged` for each `(profile_id, connection_id)` entry.
- Runtime connection limiter rows reset after crash or unclean shutdown because the limiter tables are `UNLOGGED`; startup reconciliation recreates or compacts state from fresh traffic and surviving leases.
- Failures are classified as `transient_http`, `connect_error`, or `timeout`; failover-worthy HTTP responses use the same threshold/backoff/jitter policy path as transport failures.
- Ban escalation counts only failure transitions that newly hit the resolved max-cooldown cap under the explicit strategy policy.
- Non-failover client errors do not force-clear existing persisted current state; successful responses (`2xx`/`3xx`) clear persisted current state for the connection.
- Resetting current state deletes the row and therefore clears cooldown, strike, and ban state together.
- Header blocklist at runtime is resolved as: all enabled system rules + enabled user rules for active profile.

## 7. Config Import/Export Versioning

- Canonical profile export format is Go-era config version `1` with `bundle_kind = profile_config`, `vendor_refs`, `profile_settings`, encrypted `secret_payload`, proxy `proxy_selection_strategy`, explicit `proxy_targets`, nullable model `vendor_key`, and model `api_family`.
- Canonical global vendor export format is Go-era config version `1` with `bundle_kind = vendor_catalog` and authoritative `vendors[]` metadata.
- Profile import accepts `v1` profile bundles only and validates top-level strategy family discrimination (`legacy` or `adaptive`), legacy `legacy_strategy_type + auto_recovery`, adaptive `routing_policy`, optional `vendor_key`, `loadbalance_strategy_name` for native models, `proxy_selection_strategy` plus `proxy_targets` with target metadata for proxy models, connection limiter fields, and encrypted `secret_payload` entries.
- Profile bundles never export plaintext endpoint `api_key`; endpoints with credentials use `api_key_secret_ref` plus encrypted secret entries, and endpoints without credentials use `api_key_secret_ref = null`.
- Vendor `icon_key` remains authoritative only in vendor-catalog bundles and in the global `vendors` table; profile bundles expose non-authoritative `icon_key_hint` through `vendor_refs` only.
- Persisted rows created by import always receive fresh database IDs; the v1 profile bundle contract omits internal IDs entirely and relies on name-based references.
- Profile import replace semantics are targeted by effective profile context and do not globally delete other profiles.
- Profile import reuses exact global vendor keys when provided, creates missing vendors when the proposed name is unique, and rejects duplicate bundle keys or vendor-name collisions before destructive profile-scoped replacement begins.


## 8. Invariant Notes

- The canonical split-bundle contract version is `1` for both profile bundles and vendor catalog bundles.
- Runtime hot state remains profile-scoped and reset-on-crash by design.
