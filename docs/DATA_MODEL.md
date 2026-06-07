# Data Model Document: Prism

Scope: profile-isolated runtime/management model with pricing templates, vendor metadata, profile-scoped explicit Ban Policy routing, UNLOGGED routing hot state, global sidecar control-plane tables, and the current split-bundle configuration format (`version: 3` profile bundle, `version: 1` vendor catalog bundle).

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
  loadbalance_strategy_id FK -> loadbalance_strategies.id
  context_window_tokens, default_output_token_reserve, max_context_utilization,
  preferred_context_utilization_threshold,
  facade_enabled, facade_selection_policy, facade_fallback_policy,
  context_overflow_promotion_target_id,
  is_enabled
  created_at, updated_at
  UNIQUE(profile_id, model_id)
      |
      v
model_access_targets (profile-scoped access metadata)
  id PK
  source_model_config_id FK -> model_configs.id
  target_model_config_id FK -> model_configs.id NULLABLE
  target_connection_id FK -> connections.id NULLABLE
  position
  weight
  target_priority
  UNIQUE(source_model_config_id, position)
      |
      v
loadbalance_strategies (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  name
  routing and explicit Ban Policy fields
  created_at, updated_at
  UNIQUE(profile_id, name)
      | 1:N
      v
connections (profile-scoped private endpoint bindings)
  id PK
  profile_id FK -> profiles.id
  api_family
  endpoint_id FK -> endpoints.id
  pricing_template_id FK -> pricing_templates.id (nullable, RESTRICT)
  context_window_tokens, default_output_token_reserve, max_context_utilization
  qps_limit, max_in_flight_non_stream, max_in_flight_stream
  is_active, priority
  name, auth_type, custom_headers, openai_probe_endpoint_variant
  health_status, health_detail, last_health_check
  monitoring_probe_interval_seconds
  created_at, updated_at
  INDEX(profile_id, api_family, is_active, priority)
  INDEX(endpoint_id)
  INDEX(pricing_template_id)

routing_connection_runtime_state (profile-scoped Ban Mode runtime state, UNLOGGED)
  id PK
  profile_id FK -> profiles.id
  connection_id FK -> connections.id
  window_started_at
  window_request_count
  in_flight_non_stream
  in_flight_stream
  cycle_retry_attempts
  cumulative_retry_attempts
  next_retry_at
  last_retry_delay_ms
  ban_mode, banned_until_at
  last_failure_kind, last_success_at
  live_p95_latency_ms
  created_at, updated_at
  UNIQUE(profile_id, connection_id)

routing_connection_runtime_leases (profile-scoped runtime repair table, UNLOGGED)
  lease_token PK
  profile_id FK -> profiles.id
  connection_id FK -> connections.id
  lease_kind (stream|non_stream)
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
  event_type (retry_scheduled|retry_exhausted|banned|unbanned|recovered|admission_rejected)
  failure_kind (transient_http|connect_error|timeout)
  cycle_retry_attempts, cumulative_retry_attempts
  next_retry_at, last_retry_delay_ms
  ban_mode, banned_until_at, last_success_at
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
      | 1:N optional display observations
      v
  sidecar_provider_snapshots
  sidecar_id FK -> sidecar_instances.id
  normalized provider observations

  live auth-files remain in CLIProxyAPI
  Prism reads them on demand through /api/sidecars/{id}/auth-files
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
| loadbalance_strategy_id | INTEGER | NULLABLE, FK -> loadbalance_strategies.id | Strategy used while planning this model's targets |
| context_window_tokens | INTEGER | NULLABLE | Model default context window for preflight routing |
| default_output_token_reserve | INTEGER | NOT NULL, DEFAULT 4096 | Output reserve used when request output budget is omitted |
| max_context_utilization | DOUBLE PRECISION | NOT NULL, DEFAULT 0.9 | Hard-fit usable-window multiplier for preflight routing |
| preferred_context_utilization_threshold | DOUBLE PRECISION | NULLABLE | Optional preferred-band multiplier for cheapest eligible context routing |
| facade_enabled | BOOLEAN | NOT NULL, DEFAULT FALSE | Enables Release 1 exact-ID OpenAI facade routing for this requested model |
| facade_selection_policy | VARCHAR(64) | NULLABLE | Exact facade selection policy; when facade routing is enabled, Release 1 accepts only `weighted_eligible_context` |
| facade_fallback_policy | VARCHAR(64) | NULLABLE | Exact facade ineligible-weight policy; when facade routing is enabled, Release 1 accepts only `redistribute_ineligible_weight` |
| context_overflow_promotion_target_id | VARCHAR(200) | NULLABLE | Exact model ID for one-shot CLIProxyAPI context overflow promotion target |
| is_enabled | BOOLEAN | NOT NULL, DEFAULT TRUE | Runtime availability |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints:
- `UNIQUE(profile_id, model_id)`.
- Public model authoring uses ordered rows in `model_access_targets` to reach same-family model targets. Internal connection target rows own and route to Terminal Targets, Prism's product-facing model-private endpoint bindings.
- Runtime compatibility is checked against `api_family`.
- Release 1 exact facade routing is keyed by the requested model's exact `model_id`; there is no regex matcher or capability-metadata expansion in the persisted model contract.
- `facade_enabled = true` is OpenAI-only and requires canonical `facade_selection_policy = weighted_eligible_context` plus `facade_fallback_policy = redistribute_ineligible_weight`.
- Management and config-bundle validation reject nested facades: public model targets cannot point at facade-enabled target models, and enabling facade routing on a model with inbound model-target referrers is rejected.
- `context_overflow_promotion_target_id` is nullable and model-scoped. When set, it must reference an enabled same-profile, same-`api_family`, non-facade model with a strictly larger effective usable context window, and it must not resolve to the same model or same terminal target as the source.
- Context capability defaults are normalized by management and config-bundle imports. Missing reserves become `4096`, missing utilization becomes `0.90`, and missing `preferred_context_utilization_threshold` becomes `null`. Utilization values must be greater than `0` and less than or equal to `1`; the preferred threshold must be less than or equal to `max_context_utilization` when provided.

### 2.3A `model_access_targets` (profile-scoped model access metadata)

Ordered access targets. Public authoring creates same-family model targets only. Internal connection targets are terminal ownership and routing edges from one source model to one Terminal Target, while model targets may chain until a Terminal Target is reached.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| source_model_config_id | INTEGER | FK -> model_configs.id, NOT NULL, ON DELETE CASCADE | Model owning the target list |
| target_model_config_id | INTEGER | FK -> model_configs.id, NULLABLE, ON DELETE RESTRICT | Optional model target |
| target_connection_id | INTEGER | FK -> connections.id, NULLABLE, ON DELETE RESTRICT | Optional Terminal Target ownership and routing edge |
| position | INTEGER | NOT NULL, CHECK >= 0 | Zero-based contiguous authoring order |
| weight | INTEGER | NULLABLE, CHECK >= 1 when present | Optional public model-target weighting metadata on input; backend defaults omitted public model-target values to `1`, while internal connection targets omit it |
| target_priority | INTEGER | NULLABLE, CHECK >= 0 when present | Optional public model-target priority metadata on input; backend defaults omitted public model-target values to `position`, while internal connection targets omit it |

Constraints:
- `UNIQUE(source_model_config_id, position)`.
- Each row references exactly one target model or target connection.
- Source and target rows must stay in the same profile and same `api_family`.
- Positions are normalized and validated as contiguous `0..N-1` in management contracts.
- Public model targets may omit `weight` and `target_priority` on input; the backend defaults omitted values to `1` and `position`. Internal Terminal Target entries must leave both fields null.
- Release 1 exact facade routing consumes model-target `weight` values only for exact-ID OpenAI facades and redistributes weight across the eligible subset only. Connection-owner targets remain terminal routing edges, not public facade candidates.
- Go management and config-bundle import validation rejects self-reference, cross-profile targets, cross-api-family targets, cycles, and nested facades; these relationship semantics are not enforced by database triggers.

### 2.4 `loadbalance_strategies` (profile-scoped reusable routing behavior)

Reusable explicit Ban Policy strategy objects attached by models within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Strategy name (profile-unique) |
| legacy_strategy_type | VARCHAR(40) | NOT NULL, CHECK IN (`single`, `fill-first`, `round-robin`, `cheapest_eligible_context`) | Routing subtype |
| failure_status_codes | INTEGER[] | NOT NULL | Status codes that count as retry-window failures |
| ban_mode | VARCHAR(20) | NOT NULL | `off`, `temporary`, or `until_reset` |
| retry_base_delay_ms | INTEGER | NOT NULL | First retry-window delay in milliseconds |
| retry_backoff_multiplier | NUMERIC | NOT NULL | Backoff multiplier |
| retry_jitter_ratio | NUMERIC | NOT NULL | Retry-window jitter ratio |
| retry_max_delay_ms | INTEGER | NOT NULL | Maximum retry-window delay in milliseconds |
| cycle_retry_attempt_limit | INTEGER | NOT NULL | Inclusive retry-cycle exhaustion limit |
| ban_cumulative_retry_attempt_threshold | INTEGER | NOT NULL | Inclusive cumulative retry threshold for Ban Policy bans, or zero when `ban_mode = off` |
| ban_duration_seconds | INTEGER | NOT NULL | Temporary ban duration, or zero when mode requires no duration |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraints and lifecycle rules:
- `UNIQUE(profile_id, name)`.
- Effective runtime policy resolves once per request from the attached strategy row.
- `cheapest_eligible_context` filters terminal targets by hard preflight context fit, labels fitting targets as preferred or discretionary from `preferred_context_utilization_threshold`, labels hard-fit rejects as ineligible, then ranks preferred candidates before discretionary candidates. Within a band, ranking is priced first, then estimated blended request cost, access-target position, terminal target ID, and target ID.
- Ban Policy fields carry failure status codes, retry-window delay/backoff/jitter tuning, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, and ban duration semantics.
- Retry-cycle exhaustion is inclusive at `cycle_retry_attempts >= cycle_retry_attempt_limit`.
- Ban creation is inclusive at `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; Prism does not derive the ban threshold from the cycle limit.
- `ban_mode = off` requires threshold and duration `0`; `temporary` requires threshold `>= cycle_retry_attempt_limit` plus positive duration; `until_reset` requires threshold `>= cycle_retry_attempt_limit` plus duration `0`.
- The selected profile's loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default single routing`, `Default fill-first routing`, and `Default round-robin routing` for that profile.
- Strategies cannot be deleted while attached to one or more models.

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
- Profile config export never emits plaintext `api_key`; the `version: 3` profile bundle uses `api_key_secret_ref` plus encrypted `secret_payload.entries[]` instead.
- Endpoints with no upstream credential export `api_key_secret_ref = null` and do not emit a bundle secret entry.

### 2.5 `connections` (profile-scoped Terminal Target storage)

Terminal Targets are represented as `connections` / `connection_id` in the compatibility API and database schema. Each compatibility connection row is owned by exactly one model through `model_access_targets.target_connection_id`, while endpoints remain reusable across many Terminal Targets.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| api_family | VARCHAR(50) | NOT NULL | Runtime compatibility family used for same-family target validation |
| endpoint_id | INTEGER | FK -> endpoints.id, NOT NULL | Referenced endpoint |
| pricing_template_id | INTEGER | FK -> pricing_templates.id, NULLABLE, ON DELETE RESTRICT | Assigned pricing template |
| context_window_tokens | INTEGER | NULLABLE | Terminal-target context window override or inherited model default |
| default_output_token_reserve | INTEGER | NOT NULL, DEFAULT 4096 | Output reserve used when request output budget is omitted |
| max_context_utilization | DOUBLE PRECISION | NOT NULL, DEFAULT 0.9 | Hard-fit usable-window multiplier for preflight routing |
| preferred_context_utilization_threshold | DOUBLE PRECISION | NULLABLE | Effective terminal-target preferred-band multiplier or inherited null |
| preferred_context_utilization_threshold_overridden | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether the preferred threshold is explicitly overridden from the owner model |
| qps_limit | INTEGER | NULLABLE | Per-Terminal Target QPS cap; `NULL` means unlimited |
| max_in_flight_non_stream | INTEGER | NULLABLE | Concurrent non-stream request cap; `NULL` means unlimited |
| max_in_flight_stream | INTEGER | NULLABLE | Concurrent stream request cap; `NULL` means unlimited |
| is_active | BOOLEAN | NOT NULL, DEFAULT TRUE | Active routing candidate |
| priority | INTEGER | NOT NULL, DEFAULT 0 | Legacy fallback ordering hint for family-level reads; model routing order comes from access-target `position` |
| name | TEXT | NULLABLE | Optional Terminal Target label |
| auth_type | VARCHAR(50) | NULLABLE | Optional auth behavior metadata |
| custom_headers | TEXT | NULLABLE | JSON headers applied before blocklist filtering |
| health_status | VARCHAR(20) | NOT NULL, DEFAULT 'unknown' | `unknown`, `healthy`, `unhealthy` |
| health_detail | TEXT | NULLABLE | Last health-check detail |
| last_health_check | DATETIME | NULLABLE | Last health-check timestamp |
| openai_probe_endpoint_variant | VARCHAR(40) | NULLABLE | OpenAI-family probe target and payload variant; `responses_minimal` is the default for OpenAI Terminal Targets, while non-OpenAI Terminal Targets persist `NULL` |
| monitoring_probe_interval_seconds | INTEGER | NOT NULL, DEFAULT 300 | Reserved monitoring cadence field |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Indexes include `idx_connections_profile_family_active_priority` for family-scoped active candidate reads, `idx_connections_endpoint_id` for endpoint dependency checks, and `idx_connections_pricing_template_id` for template dependency checks.

Connection invariants:
- `api_family` is the compatibility source for access-target validation and runtime planning.
- Product-facing routing surfaces present these rows as Terminal Targets while persisted compatibility remains `connections` and `target_type = "connection"`.
- A connection can be referenced by exactly one model access target in the same profile.
- The partial unique index `uq_model_access_targets_connection_owner` enforces one owner for every non-null `target_connection_id`.
- Public model target authoring cannot attach Terminal Targets by ID. Model detail creates, updates, health-checks, and deletes Terminal Targets through model-scoped routes.
- Deleting a Terminal Target removes its owning `model_access_targets.target_connection_id` row in the same operation.
- Connection create/update contracts do not allow client-written `priority`; model-specific ordering changes flow through `/api/models/{model_config_id}/targets/{target_id}/position`.
- `preferred_context_utilization_threshold` is owner-scoped. A non-overridden Terminal Target inherits the owner model value, explicit `null` resets inheritance, and an overridden value must stay less than or equal to the effective `max_context_utilization`.
- `openai_probe_endpoint_variant` derives OpenAI terminal-target operation capability for planning: blank or `responses_*` variants map to `openai.responses`, while `chat_completions_*` variants map to `openai.chat_completions`.

### 2.6 `pricing_templates` (profile-scoped reusable token pricing)

Reusable token pricing definitions that can be attached to many Terminal Targets within a profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Template name (profile-unique) |
| description | TEXT | NULLABLE | Optional notes |
| pricing_unit | VARCHAR(20) | NOT NULL, DEFAULT 'PER_1M' | Billing unit |
| pricing_currency_code | VARCHAR(3) | NOT NULL | Template currency code |
| input_price | VARCHAR(20) | NOT NULL | Base input token price string |
| output_price | VARCHAR(20) | NOT NULL | Base output token price string |
| cached_input_price | VARCHAR(20) | NOT NULL | Cache-read input token price string |
| cache_creation_price | VARCHAR(20) | NOT NULL | Cache-creation input token price string |
| reasoning_price | VARCHAR(20) | NOT NULL | Reasoning output token price string |
| version | INTEGER | NOT NULL, DEFAULT 1 | Auto-incremented on pricing-impacting changes |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last update timestamp |

Constraint: `UNIQUE(profile_id, name)`.

Pricing templates use five concrete pricing strings in steady state. Management API writes and profile bundle v3 import normalize missing, null, or blank pricing inputs for any of the five pricing fields to `"0"` before decimal validation. Explicit `"0"` means configured free pricing. `MISSING_PRICE_DATA` applies only when a pricing template or runtime pricing snapshot is absent, unusable, or invalid, or when required FX data cannot be applied.

Token costing consumes canonical disjoint token components: base input, cache-read input, cache-creation input, base output, reasoning output, and provider or derived total. `cached_tokens` is derived-only for aggregate and presentation surfaces from cache-read plus cache-creation input tokens.


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
| resolved_target_model_id | VARCHAR(200) | NULLABLE | Final target model selected for the attempt |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| ingress_request_id | VARCHAR(36) | NULLABLE | Prism-generated incoming request grouping ID |
| attempt_number | INTEGER | NULLABLE | Per-ingress attempt order, starting at 1 |
| operation_name | VARCHAR(100) | NOT NULL | Ingress canonical operation name |
| upstream_operation_name | VARCHAR(100) | NULLABLE | Provider-facing operation name used for the attempt |
| operation_translation_mode | VARCHAR(80) | NOT NULL, DEFAULT `none` | `none`, `openai_responses_to_chat_completions`, or `openai_chat_completions_to_responses` |
| upstream_request_path | VARCHAR(500) | NULLABLE | Sanitized provider-facing operation path |
| provider_correlation_id | VARCHAR(255) | NULLABLE | Best-effort provider-visible correlation ID |
| connection_id | INTEGER | NULLABLE | Executed connection snapshot |
| selected_terminal_target_id | INTEGER | NULLABLE | Planner-selected terminal target before execution or no-fit rejection |
| context_routing | JSONB | NULLABLE | Preflight context-routing metadata, skipped-target reasons, optional nested `facade_selection`, and optional nested `context_overflow_promotion` metadata |
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
- `model_id` records the requested model ID while `resolved_target_model_id` records the final target model ID selected for that attempt.
- Exact facade attempts keep that same top-level split: `model_id` stays the requested public facade ID, `resolved_target_model_id` stays the selected child model ID, and optional `context_routing.facade_selection` is additive planner metadata rather than a replacement for those top-level fields.
- `operation_name` and `request_path` remain ingress-led. `upstream_operation_name`, `operation_translation_mode`, and `upstream_request_path` are additive upstream attribution for native or translated attempts.
- `selected_terminal_target_id` can differ from `connection_id` when the planner selected one terminal target but execution later failed over to another attempt. Exact facade routing adds no sibling-target failover after child selection; any later retry remains inside the selected child model's own terminal strategy. No-fit `413` rows keep executed target fields null and preserve skipped-target detail in `context_routing`.
- Context overflow promotion stores additive `context_routing.context_overflow_promotion` detail for the CLIProxyAPI-specific, non-stream, one-shot replay path. Source overflow rows keep the source resolved model and terminal target, promoted rows keep the promoted resolved model and terminal target, and both stay grouped by the same `ingress_request_id`.
- `stream_error_detail` is exposed only by exact request-log detail reads. List and realtime payloads expose `stream_outcome` and `stream_error_kind` without detail text.
- Prism prices only observed usage. `STREAM_USAGE_UNAVAILABLE` marks interrupted or no-terminal stream rows where required tokens are absent; completed streams missing required usage keep `MISSING_TOKEN_USAGE`.
- Token usage fields are canonical disjoint components. `input_tokens` is base input only, `output_tokens` is base output only, and cache-read input, cache-creation input, and reasoning output stay in their split fields.
- Pricing snapshots persist the five concrete pricing strings used for the attempt. Explicit `"0"` prices mean configured free pricing, while absent or invalid pricing snapshots and missing FX data stay unpriced with `MISSING_PRICE_DATA`.

### 2.11 `usage_request_events` (partitioned immutable usage attribution)

Usage-event rows are the finalized source for the unified statistics snapshot. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| ingress_request_id | VARCHAR(36) | NOT NULL, UNIQUE per profile | Incoming request grouping ID preserved for aggregate attribution and cross-table correlation |
| model_id | VARCHAR(200) | NOT NULL | Requested model ID |
| resolved_target_model_id | VARCHAR(200) | NULLABLE | Final target model selected for the request |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| operation_name | VARCHAR(100) | NOT NULL | Ingress canonical operation name |
| upstream_operation_name | VARCHAR(100) | NULLABLE | Provider-facing operation name for finalized attribution |
| operation_translation_mode | VARCHAR(80) | NOT NULL, DEFAULT `none` | Translation mode copied from the finalized attempt |
| upstream_request_path | VARCHAR(500) | NULLABLE | Sanitized provider-facing operation path |
| endpoint_id | INTEGER | NULLABLE | Endpoint snapshot |
| connection_id | INTEGER | NULLABLE | Executed connection snapshot |
| selected_terminal_target_id | INTEGER | NULLABLE | Planner-selected terminal target for the finalized request |
| context_routing | JSONB | NULLABLE | Preflight context-routing metadata copied from runtime planning, including optional nested `facade_selection` and `context_overflow_promotion` metadata |
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
- Usage events copy canonical disjoint token totals, runtime pricing results, selected-terminal-target metadata, context-routing metadata when it exists, and additive ingress/upstream operation attribution. Exact facade events preserve the same top-level requested/resolved model split as request logs and carry facade planner detail only through nested `context_routing.facade_selection`. Aggregate `cached_tokens` is derived from cache-read plus cache-creation input tokens rather than stored as its own runtime component.
- When context overflow promotion occurs, final usage ownership belongs to the final returned response only. The final usage event uses the final response status, usage, pricing, resolved target model, selected terminal target, and `attempt_count` across source plus promoted phases; failed source overflow attempts remain attempt-level rows and may have null usage.
- Explicit `"0"` pricing contributes zero-cost component micros on priced events. Rows with absent or invalid pricing snapshots, or missing FX data, remain unpriced with `MISSING_PRICE_DATA`.

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
- Translated OpenAI attempts store upstream-native request and response bodies when body capture is enabled. Audit rows do not store the translated client-facing body shape.

### 2.13 `loadbalance_events` (partitioned immutable profile attribution)

Persistent record of retry-window, ban, recovery, and admission transitions. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| connection_id | INTEGER | NOT NULL | Private connection ID |
| event_type | VARCHAR(32) | NOT NULL | `retry_scheduled`, `retry_exhausted`, `banned`, `unbanned`, `recovered`, `admission_rejected` |
| failure_kind | VARCHAR(20) | NULLABLE | `transient_http`, `connect_error`, `timeout` |
| cycle_retry_attempts | INTEGER | NOT NULL | Retry attempts in the current retry cycle |
| cumulative_retry_attempts | INTEGER | NOT NULL | Retry attempts accumulated for Ban Policy thresholding |
| policy_cycle_retry_attempt_limit | INTEGER | NULLABLE | Strategy cycle limit snapshot for events produced by Ban Policy evaluation |
| policy_ban_cumulative_retry_attempt_threshold | INTEGER | NULLABLE | Strategy cumulative ban threshold snapshot for events produced by Ban Policy evaluation |
| next_retry_at | DATETIME | NULLABLE | Wall-clock time when the next retry cycle can run |
| last_retry_delay_ms | INTEGER | NOT NULL | Last resolved retry-window delay in milliseconds |
| model_id | VARCHAR(200) | NULLABLE | Model ID snapshot |
| endpoint_id | INTEGER | NULLABLE | Endpoint ID snapshot |
| vendor_id | INTEGER | NULLABLE, FK -> vendors.id, ON DELETE SET NULL | Optional vendor snapshot |
| ban_mode | VARCHAR(20) | NULLABLE | `off`, `temporary`, or `until_reset` when relevant |
| banned_until_at | DATETIME | NULLABLE | Temporary-ban expiry when relevant |
| last_success_at | DATETIME | NULLABLE | Successful response time that cleared retry state when relevant |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Event timestamp and partition key |

Event snapshot semantics:
- Ban Policy event rows keep immutable SQL storage snapshots in `policy_cycle_retry_attempt_limit` and `policy_ban_cumulative_retry_attempt_threshold` from the strategy evaluated at event time.
- Event list/detail APIs expose those snapshots as `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` so the public payload matches the strategy contract.
- Current-state rows stay connection-global and do not store strategy threshold fields; policy thresholds belong to immutable event snapshots from the owner model's strategy.
- Historical events can explain inclusive threshold behavior even after a strategy changes later.

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
- `backend/internal/platform/logretention` maintains a 15-day future partition horizon for the four managed log tables.
- Whole child partitions with upper bound `<= cutoff` are dropped. Only the cutoff-overlapping boundary child receives bounded row cleanup and `VACUUM (ANALYZE, PROCESS_TOAST TRUE)`.
- Managed partition diagnostics should read `pg_class`, `pg_inherits`, `pg_total_relation_size`, `pg_relation_size`, and `pg_class.reltoastrelid` so operators can see root, child, and TOAST relations without mutating data.
- Partitioned retention manages the current log-table set only; historical log storage shapes are not rewritten into current partitions.
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

Sidecar tables are global instance state. They are not profile-scoped and do not participate in profile bundle import/export. The baseline schema creates the sidecar domain.

| Table | Purpose |
|---|---|
| `sidecar_instances` | Sidecar registration, canonical base URL, encrypted management password, enabled flag, sync interval, request timeout, network policy flags, management-auth state, pause metadata, and sync timestamps. |
| `sidecar_provider_snapshots` | Optional normalized provider inventory observations for Gemini, Claude, Codex, Vertex, and OpenAI-compatible credentials. |

Ownership notes:
- Active `sidecar_instances` rows are unique on `lower(name)` and `base_url_canonical` among non-deleted registrations.
- Stored management passwords use the backend secret-encryption key and are write-only at the API boundary.
- Provider observation JSON must not persist raw token, secret, password, API-key, authorization, raw provider response, or raw provider identity values.
- Auth-file inventory is sourced live from CLIProxyAPI and is not stored as a Prism table.
- Provider inventory is sourced from CLIProxyAPI and may be persisted as normalized observations for operator display.
- Sync work is scheduler-owned low-priority background work; request handlers enqueue or trigger bounded service methods rather than owning recurring timers.

### 2.15 `routing_connection_runtime_state` (profile-scoped Ban Mode runtime state, `UNLOGGED`)

Ephemeral hot-state row for per-connection admission counters and Ban Mode retry-cycle state. This table is intentionally `UNLOGGED`, so it resets after crash or unclean shutdown.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, AUTOINCREMENT | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| connection_id | INTEGER | FK -> connections.id, NOT NULL | Private connection under Ban Policy tracking |
| window_started_at | DATETIME | NULLABLE | Current QPS window start |
| window_request_count | INTEGER | NOT NULL, DEFAULT 0 | Requests admitted in current one-second window |
| in_flight_non_stream | INTEGER | NOT NULL, DEFAULT 0 | Current non-stream reservations |
| in_flight_stream | INTEGER | NOT NULL, DEFAULT 0 | Current stream reservations |
| cycle_retry_attempts | INTEGER | NOT NULL | Retry attempts in the current retry cycle |
| cumulative_retry_attempts | INTEGER | NOT NULL | Retry attempts accumulated for Ban Policy thresholding |
| next_retry_at | DATETIME | NULLABLE | Wall-clock time when the next retry cycle can run |
| last_retry_delay_ms | INTEGER | NOT NULL | Last resolved retry-window delay in milliseconds |
| ban_mode | VARCHAR(20) | NOT NULL | `off`, `temporary`, or `until_reset` |
| banned_until_at | DATETIME | NULLABLE | Temporary-ban expiry when relevant |
| last_failure_kind | VARCHAR(20) | NULLABLE | Latest retryable failure kind: `transient_http`, `connect_error`, or `timeout` |
| last_success_at | DATETIME | NULLABLE | Successful response time that cleared retry state when relevant |
| live_p95_latency_ms | INTEGER | NULLABLE | Passive-request latency signal |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Row creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last mutation timestamp |

Constraints:
- `UNIQUE(profile_id, connection_id)`.
- Admission and retry counters are non-negative.
- `ban_mode` is restricted to `off`, `temporary`, or `until_reset`.
- `last_failure_kind` is restricted to `transient_http`, `connect_error`, or `timeout` when present.

Ban Mode semantics:
- `next_retry_at` represents the retry-window boundary. Until it passes, runtime planning treats the connection as `retry_wait` and can try other eligible final targets.
- `cycle_retry_attempts` resets when the next retry window opens; `cumulative_retry_attempts` is the Ban Policy counter for the private connection. The configured threshold is not stored here because current state is connection-global.
- `ban_mode="until_reset"` keeps the connection banned until reset. `ban_mode="temporary"` keeps it banned until `banned_until_at`; expired temporary bans are cleared on the next runtime attempt.
- Successful upstream responses clear retry-window and ban state for the private connection.

### 2.16 `routing_connection_runtime_leases` (profile-scoped runtime lease table, `UNLOGGED`)

Ephemeral lease rows used for non-stream attempts and streaming heartbeats.

| Column | Type | Constraints | Description |
|---|---|---|---|
| lease_token | VARCHAR(64) | PK | Lease identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| connection_id | INTEGER | FK -> connections.id, NOT NULL | Private connection under Ban Policy tracking |
| lease_kind | VARCHAR(20) | NOT NULL | `stream` or `non_stream` |
| expires_at | DATETIME | NOT NULL | Lease expiry for repair/reconciliation |
| heartbeat_at | DATETIME | NULLABLE | Latest stream heartbeat when relevant |
| created_at | DATETIME | NOT NULL, DEFAULT NOW | Row creation timestamp |
| updated_at | DATETIME | NOT NULL, DEFAULT NOW | Last mutation timestamp |

Constraints:
- `lease_kind` is restricted to `stream` or `non_stream`.

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
CREATE UNIQUE INDEX idx_model_access_targets_source_position ON model_access_targets(source_model_config_id, position);
CREATE UNIQUE INDEX idx_model_access_targets_source_target_model ON model_access_targets(source_model_config_id, target_model_config_id) WHERE target_model_config_id IS NOT NULL;
CREATE UNIQUE INDEX idx_model_access_targets_source_target_connection ON model_access_targets(source_model_config_id, target_connection_id) WHERE target_connection_id IS NOT NULL;
CREATE UNIQUE INDEX uq_model_access_targets_connection_owner ON model_access_targets(target_connection_id) WHERE target_connection_id IS NOT NULL;
CREATE UNIQUE INDEX idx_endpoints_profile_name ON endpoints(profile_id, name);
CREATE UNIQUE INDEX idx_endpoint_fx_profile_model_endpoint ON endpoint_fx_rate_settings(profile_id, model_id, endpoint_id);
CREATE UNIQUE INDEX idx_user_settings_profile_id ON user_settings(profile_id);

-- Performance indexes
CREATE INDEX idx_model_configs_profile_model_enabled ON model_configs(profile_id, model_id, is_enabled);
CREATE INDEX idx_model_access_targets_target_model ON model_access_targets(target_model_config_id);
CREATE INDEX idx_model_access_targets_target_connection ON model_access_targets(target_connection_id);
CREATE INDEX idx_endpoints_profile_position ON endpoints(profile_id, position);
CREATE INDEX idx_connections_profile_family_active_priority ON connections(profile_id, api_family, is_active, priority);
CREATE INDEX idx_connections_endpoint_id ON connections(endpoint_id);
CREATE INDEX idx_connections_pricing_template_id ON connections(pricing_template_id);
CREATE INDEX idx_request_logs_profile_created_at ON request_logs(profile_id, created_at);
CREATE INDEX idx_request_logs_ingress_request_id ON request_logs(ingress_request_id);
CREATE INDEX idx_audit_logs_profile_created_at ON audit_logs(profile_id, created_at);
CREATE INDEX idx_loadbalance_events_profile_created ON loadbalance_events(profile_id, created_at);
CREATE INDEX idx_loadbalance_events_connection ON loadbalance_events(connection_id, created_at);
CREATE INDEX idx_loadbalance_events_event_type ON loadbalance_events(event_type);
CREATE INDEX idx_routing_connection_runtime_state_profile_connection ON routing_connection_runtime_state(profile_id, connection_id);
CREATE INDEX idx_routing_connection_runtime_leases_profile_connection ON routing_connection_runtime_leases(profile_id, connection_id);
CREATE INDEX idx_routing_connection_runtime_leases_expires_at ON routing_connection_runtime_leases(expires_at);
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

Sidecar uniqueness and indexes are part of the baseline schema; they cover active sidecar names and canonical URLs plus per-sidecar snapshot lookups.

## 4. Relationship and Ownership Rules

- `vendors` are global and shared across all profiles.
- `model_configs` reference shared vendor rows without vendor-owned delete cascade semantics; deleting a vendor must not delete profile-scoped model rows.
- `profiles` own all scoped entities: `model_configs`, `endpoints`, `connections`, `user_settings`, `endpoint_fx_rate_settings`, user `header_blocklist_rules`.
- `app_auth_settings` is the singleton auth root for `refresh_tokens`, `proxy_api_keys`, and `password_reset_challenges`; retained `webauthn_credentials` rows remain schema-level historical state rather than an active supported workflow surface.
- `sidecar_instances` is a global control-plane root for sidecar snapshots; it is not owned by a profile.
- `request_logs`, `usage_request_events`, `audit_logs`, and `loadbalance_events` keep immutable `profile_id` attribution and are not rewritten when active profile changes.
- `request_logs.ingress_request_id` is the canonical operator drill-in key for grouped request investigation.
- `routing_connection_runtime_state` and `routing_connection_runtime_leases` are profile-scoped runtime state and intentionally `UNLOGGED`; operators accept reset-on-crash semantics.
- Cross-profile resource lookups are treated as not found (`404`) under effective profile scope.
- Private connection create/update must enforce profile consistency between the connection and endpoint references. The single owner is enforced through `model_access_targets.target_connection_id`.

## 5. Deletion and Retention Semantics

- Routine profile deletion (`DELETE /api/profiles/{id}`) is soft-delete of inactive profile (`deleted_at` set).
- Active profile deletion is rejected.
- Vendor deletion hard-deletes the shared vendor row and nulls `model_configs.vendor_id` plus delete-safe observability vendor foreign keys instead of rejecting the delete or cascading to model rows.
- Profile-scoped config entities are removable through explicit profile-targeted replace/purge workflows.
- Historical telemetry/audit retention is independent; routine profile delete does not erase historical attribution rows.

## 6. Runtime Isolation Notes

- Proxy routing always resolves against the active profile snapshot.
- Runtime routing state is persisted in profile-scoped hot tables and namespaced by `(profile_id, connection_id)` so retry-window, admission, and ban decisions do not leak across profiles.
- Ban Mode state stays with the same per-connection runtime row and tracks retry-cycle attempts, cumulative attempts, next retry timing, ban mode, and temporary-ban expiry together.
- Admission state is persisted in profile-scoped `UNLOGGED` hot tables plus lease rows and remains namespaced by `(profile_id, connection_id)` to avoid cross-profile leakage.
- Runtime current-state rows track `cycle_retry_attempts`, `cumulative_retry_attempts`, `next_retry_at`, `last_retry_delay_ms`, `ban_mode`, `banned_until_at`, `last_failure_kind`, `last_success_at`, QPS window counters, in-flight counters, and optional latency for each `(profile_id, connection_id)` entry.
- Runtime state and lease rows reset after crash or unclean shutdown because the tables are `UNLOGGED`; startup reconciliation recreates or compacts state from fresh traffic and surviving leases.
- Failures are classified as `transient_http`, `connect_error`, or `timeout`; retryable HTTP responses use the same retry-window delay/backoff/jitter policy path as transport failures.
- Ban Mode thresholding uses cumulative retry attempts for the private connection owned by the terminal model path.
- Non-retryable client errors do not force-clear existing persisted current state; successful responses (`2xx`/`3xx`) clear persisted retry and ban state for the connection.
- Resetting current state deletes the row and therefore clears retry-window counters, next retry timing, and ban state together.
- Header blocklist at runtime is resolved as: all enabled system rules + enabled user rules for active profile.

## 7. Config Import/Export Versioning

- Canonical profile export format is Go-era config version `3` with `bundle_kind = profile_config`, top-level `connections` for Terminal Targets, `models[].access_targets[]`, exact-facade model fields (`facade_enabled`, `facade_selection_policy`, `facade_fallback_policy`), nullable `models[].context_overflow_promotion_target_id`, `vendor_refs`, `profile_settings`, encrypted `secret_payload`, nullable model `vendor_key`, and model `api_family`.
- Planner rollout remains bootstrap-owned only: the temporary `runtime.routing.plannerMode` and `runtime.routing.openaiTerminalTranslationMode` fields live in plaintext startup config, not in profile bundle persistence, and Phase 8 does not add a routing graph table or bump the profile bundle version.
- Canonical global vendor export format is Go-era config version `1` with `bundle_kind = vendor_catalog` and authoritative `vendors[]` metadata.
- Profile import accepts version-3 profile bundles only and validates top-level `connections` for Terminal Targets, ordered model access targets, exact Release 1 facade fields, nullable context overflow promotion targets, explicit Ban Policy strategies, optional `vendor_key`, `loadbalance_strategy_name`, connection admission-limit fields, context capability fields, five concrete pricing fields, and encrypted `secret_payload` entries. Version-3 profile import rejects any `connection_ref` used by multiple models or colliding with existing private ownership, rejects regex/capability facade expansion and nested facades, validates promotion targets as enabled same-family non-facade models with larger effective usable windows, normalizes missing facade fields to `facade_enabled = false` with nil policies, normalizes missing model-target `weight` / `target_priority` to `1` / `position`, normalizes missing/null/blank pricing inputs to `"0"`, and serializes effective context defaults explicitly as `default_output_token_reserve = 4096` and `max_context_utilization = 0.90` before validation/export.
- Profile bundles never export plaintext endpoint `api_key`; endpoints with credentials use `api_key_secret_ref` plus encrypted secret entries, and endpoints without credentials use `api_key_secret_ref = null`.
- Vendor `icon_key` remains authoritative only in vendor-catalog bundles and in the global `vendors` table; profile bundles expose non-authoritative `icon_key_hint` through `vendor_refs` only.
- Persisted rows created by import always receive fresh database IDs; the version-3 profile bundle contract omits internal IDs entirely and relies on name-based references.
- Profile import replace semantics are targeted by effective profile context and do not globally delete other profiles.
- Profile import reuses exact global vendor keys when provided, creates missing vendors when the proposed name is unique, and rejects duplicate bundle keys or vendor-name collisions before destructive profile-scoped replacement begins.


## 8. Invariant Notes

- The canonical split-bundle contract version is `3` for profile bundles and `1` for vendor catalog bundles.
- Runtime hot state remains profile-scoped and reset-on-crash by design.
