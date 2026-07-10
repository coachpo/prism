# Data Model Document: Prism

Scope: profile-isolated runtime and management model with pricing templates, profile-scoped explicit Ban Policy routing, retained compatibility hot-state schema, process-local runtime state, endpoint label snapshots, and user-agent client rules.

## 1. Entity Relationship Diagram

```
model_configs (profile-scoped)
  id PK
  profile_id FK -> profiles.id
  api_family (fixed enum)
  model_id
  display_name
  loadbalance_strategy_id FK -> loadbalance_strategies.id
  openai_accepted_format
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
  is_enabled
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
  qps_limit, max_in_flight_non_stream, max_in_flight_stream
  is_active, priority
  name, auth_type, custom_headers, openai_text_capability
  retained compatibility health/probe columns
  created_at, updated_at
  INDEX(profile_id, api_family, is_active, priority)
  INDEX(endpoint_id)
  INDEX(pricing_template_id)

routing_connection_runtime_state (retained compatibility schema, UNLOGGED)
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

routing_connection_runtime_leases (retained compatibility schema, UNLOGGED)
  lease_token PK
  profile_id FK -> profiles.id
  connection_id FK -> connections.id
  lease_kind (stream|non_stream)
  expires_at, heartbeat_at
  created_at, updated_at
  INDEX(profile_id, connection_id)
  INDEX(expires_at)

loadbalance_round_robin_state (retained compatibility schema)
  id PK
  profile_id compatibility scope value
  model_config_id FK -> model_configs.id
  next_cursor
  created_at, updated_at
  UNIQUE(profile_id, model_config_id)

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
  operation_name, upstream_operation_name, operation_translation_mode, upstream_request_path
  ingress_request_id, attempt_number, provider_correlation_id
  endpoint_id, connection_id, endpoint_base_url, endpoint_description
  status_code, response_time_ms, is_stream
  stream_outcome, stream_error_kind, stream_error_detail
  usage token fields
  costing snapshot fields
  request_path, error_detail
  created_at partition key

usage_request_events (partitioned immutable usage attribution)
  PK (created_at, id)
  profile_id FK -> profiles.id
  ingress_request_id indexed grouping id
  model_id, resolved_target_model_id, api_family
  operation_name, upstream_operation_name, operation_translation_mode, upstream_request_path
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
  model_id, connection_id, endpoint_base_url, endpoint_description
  request/response payload fields
  is_stream, duration_ms
  created_at partition key

loadbalance_events (partitioned immutable attribution)
  PK (created_at, id)
  profile_id FK -> profiles.id
  connection_id
  event_type (retry_scheduled|retry_exhausted|banned|unbanned|recovered|admission_rejected)
  failure_kind (transient_http|connect_error|timeout)
  cycle_retry_attempts, cumulative_retry_attempts
  next_retry_at, last_retry_delay_ms
  ban_mode, banned_until_at, last_success_at
  model_id, endpoint_id
  created_at

management_jobs (durable management work queue)
  id PK
  type (audit_delete|log_retention)
  state (queued|running|cancel_requested|cancelled|succeeded|failed)
  profile_id (profile-owned jobs or 0 for global jobs)
  scope_json, reason
  rows_deleted, batches_completed, progress_json
  attempt/lease/error fields
  created_at, updated_at
      |
      v
management_job_events
  id PK
  job_id FK -> management_jobs.id ON DELETE CASCADE
  event_type, message, rows_deleted
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

proxy_api_keys
  id PK
  name, key_prefix UNIQUE, key_hash, last_four
  is_active, expires_at, last_used_at, last_used_ip
  created_by_auth_subject_id FK -> app_auth_settings.id, notes, rotated_from_id
  created_at, updated_at

```

## 2. Table Definitions

### 2.1 `profiles`

Profiles are retained storage namespaces. Multi-profile management is frozen: management reads and writes are pinned to Default profile id `1`, while runtime loads the published Default profile id `1` snapshot.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| name | VARCHAR(200) | NOT NULL, UNIQUE | Profile name |
| description | TEXT | NULLABLE | Optional description |
| is_active | BOOLEAN | NOT NULL | Runtime-active marker; application-managed seed value |
| is_default | BOOLEAN | NOT NULL | Seeded default marker; application-managed seed value |
| is_editable | BOOLEAN | NOT NULL | Editable flag; current startup invariants keep the system default profile editable |
| version | INTEGER | NOT NULL | Retained concurrency token; application-managed value |
| deleted_at | TIMESTAMPTZ | NULLABLE | Soft-delete marker for inactive rows |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints and lifecycle rules:
- At most one row can have `is_active = true`; the partial unique index is not scoped by `deleted_at`.
- Startup invariants ensure Default profile id `1` exists and remains editable.
- Profile create, update, activate, and delete management routes are not exposed while multi-profile management is frozen.

### 2.2 `model_configs` (profile-scoped)

Maps a model ID to fixed api family and routing behavior within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| model_id | VARCHAR(200) | NOT NULL | Model identifier (scoped by profile) |
| display_name | VARCHAR(200) | NULLABLE | Human-readable name |
| loadbalance_strategy_id | INTEGER | NULLABLE, FK -> loadbalance_strategies.id | Strategy used while planning this model's targets |
| openai_accepted_format | TEXT | NULLABLE | OpenAI model ingress contract: `responses_only`, `chat_completions_only`, or `dual_native`; non-OpenAI models persist `NULL` |
| is_enabled | BOOLEAN | NOT NULL | Runtime availability; create defaults omitted values to `false` in application code |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints:
- `UNIQUE(profile_id, model_id)`.
- OpenAI models require `openai_accepted_format` in `responses_only`, `chat_completions_only`, or `dual_native`; non-OpenAI models must keep it `NULL`.
- Public model authoring uses ordered rows in `model_access_targets` to reach same-family model targets. Internal connection target rows own and route to Terminal Targets, Prism's product-facing model-private endpoint bindings.
- Runtime compatibility is checked against `api_family`.
- Exact facade routing, model-owned context capability, and overflow-promotion authoring fields are retired.

### 2.3 `model_access_targets` (profile-scoped model access metadata)

Ordered access targets. Public authoring creates same-family model targets only. Internal connection targets are terminal ownership and routing edges from one source model to one Terminal Target, while model targets may chain until a Terminal Target is reached.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| source_model_config_id | INTEGER | FK -> model_configs.id, NOT NULL, ON DELETE CASCADE | Model owning the target list |
| target_type | VARCHAR(20) | NOT NULL, CHECK IN (`model`, `connection`) | Target discriminator |
| target_model_config_id | INTEGER | FK -> model_configs.id, NULLABLE, ON DELETE RESTRICT | Optional model target |
| target_connection_id | INTEGER | FK -> connections.id, NULLABLE, ON DELETE RESTRICT | Optional Terminal Target ownership and routing edge |
| position | INTEGER | NOT NULL, CHECK >= 0 | Zero-based contiguous authoring order |
| is_enabled | BOOLEAN | NOT NULL | Whether this ordered peer participates in routing; application-managed value |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints:
- `UNIQUE(source_model_config_id, position)` is the deferrable `uq_model_access_targets_source_position` constraint.
- `target_type` is `model` or `connection`, and each row references exactly one matching target model or target connection.
- Source and target rows must stay in the same profile and same `api_family`.
- Positions are normalized and validated as contiguous `0..N-1` in management contracts.
- Position is an ordering key only, not priority, tier, or weight. Duplicate positions reject before write.
- Obsolete public payload keys `weight` and `target_priority` reject in management model APIs. The fresh schema has no columns for those values.
- Runtime routing evaluates enabled same-family model targets by flat `position` and stable IDs. Connection-owner targets remain terminal routing edges, not public model-target candidates.
- Go management validation rejects self-reference, cross-profile targets, cross-api-family targets, and cycles; these relationship semantics are not enforced by database triggers.

### 2.4 `loadbalance_strategies` (profile-scoped reusable routing behavior)

Reusable explicit Ban Policy strategy objects attached by models within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Strategy name (profile-unique) |
| legacy_strategy_type | VARCHAR(32) | NOT NULL, CHECK IN (`single`, `fill-first`, `round-robin`) | Routing subtype |
| failure_status_codes | INTEGER[] | NOT NULL | Status codes that count as retry-window failures |
| ban_mode | VARCHAR(20) | NOT NULL | `off`, `temporary`, or `until_reset` |
| retry_base_delay_ms | INTEGER | NOT NULL | First retry-window delay in milliseconds |
| retry_backoff_multiplier | DOUBLE PRECISION | NOT NULL | Backoff multiplier |
| retry_jitter_ratio | DOUBLE PRECISION | NOT NULL | Retry-window jitter ratio |
| retry_max_delay_ms | INTEGER | NOT NULL | Maximum retry-window delay in milliseconds |
| cycle_retry_attempt_limit | INTEGER | NOT NULL | Inclusive retry-cycle exhaustion limit |
| ban_cumulative_retry_attempt_threshold | INTEGER | NOT NULL | Inclusive cumulative retry threshold for Ban Policy bans, or zero when `ban_mode = off` |
| ban_duration_seconds | INTEGER | NOT NULL | Temporary ban duration, or zero when mode requires no duration |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints and lifecycle rules:
- `UNIQUE(profile_id, name)`.
- Effective runtime policy resolves once per request from the attached strategy row.
- Supported routing families are `single`, `fill-first`, and `round-robin`.
- Ban Policy fields carry failure status codes, retry-window delay/backoff/jitter tuning, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, and ban duration semantics.
- Retry-cycle exhaustion is inclusive at `cycle_retry_attempts >= cycle_retry_attempt_limit`.
- Ban creation is inclusive at `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; Prism does not derive the ban threshold from the cycle limit.
- `ban_mode = off` requires threshold and duration `0`; `temporary` requires threshold `>= cycle_retry_attempt_limit` plus positive duration; `until_reset` requires threshold `>= cycle_retry_attempt_limit` plus duration `0`.
- The loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default single routing`, `Default fill-first routing`, and `Default round-robin routing` for Default profile id `1`.
- Strategies cannot be deleted while attached to one or more models.

### 2.5 `endpoints` (profile-scoped credentials)

Reusable credential objects scoped to one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Endpoint label |
| base_url | VARCHAR(500) | NOT NULL | Upstream base URL |
| api_key | VARCHAR(500) | NOT NULL | Prism-at-rest encrypted endpoint secret |
| position | INTEGER | NOT NULL | Zero-based contiguous ordering index within profile |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints and indexes:
- `UNIQUE(profile_id, name)`.
- `INDEX(profile_id, position)` for ordered reads.

### 2.6 `connections` (profile-scoped Terminal Target storage)

Terminal Targets are represented as `connections` / `connection_id` in the compatibility API and database schema. Each compatibility connection row is owned by exactly one model through `model_access_targets.target_connection_id`, while endpoints remain reusable across many Terminal Targets.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| api_family | VARCHAR(50) | NOT NULL | Runtime compatibility family used for same-family target validation |
| endpoint_id | INTEGER | FK -> endpoints.id, NOT NULL | Referenced endpoint |
| pricing_template_id | INTEGER | FK -> pricing_templates.id, NULLABLE, ON DELETE RESTRICT | Assigned pricing template |
| qps_limit | INTEGER | NULLABLE | Per-Terminal Target QPS cap; `NULL` means unlimited |
| max_in_flight_non_stream | INTEGER | NULLABLE | Concurrent non-stream request cap; `NULL` means unlimited |
| max_in_flight_stream | INTEGER | NULLABLE | Concurrent stream request cap; `NULL` means unlimited |
| is_active | BOOLEAN | NOT NULL | Active routing candidate; application-managed value |
| priority | INTEGER | NOT NULL | Legacy fallback ordering hint for family-level reads; model routing order comes from access-target `position` |
| name | TEXT | NULLABLE | Optional Terminal Target label |
| auth_type | VARCHAR(50) | NULLABLE | Optional auth behavior metadata |
| custom_headers | TEXT | NULLABLE | JSON headers applied before blocklist filtering |
| health_status | VARCHAR(20) | NOT NULL | `unknown`, `healthy`, `unhealthy`; application-managed compatibility value |
| health_detail | TEXT | NULLABLE | Retained compatibility health detail |
| last_health_check | TIMESTAMPTZ | NULLABLE | Retained compatibility health timestamp |
| openai_probe_endpoint_variant | VARCHAR(40) | NULLABLE | Retained schema field for existing rows; the live UI no longer writes this metadata |
| openai_text_capability | TEXT | NULLABLE | OpenAI Terminal Target text runtime capability: `responses_only`, `chat_completions_only`, or `dual_native`; non-OpenAI Terminal Targets persist `NULL` |
| monitoring_probe_interval_seconds | INTEGER | NOT NULL, DEFAULT 300 | Reserved monitoring cadence field |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Indexes include `idx_connections_profile_family_active_priority` for family-scoped active candidate reads, `idx_connections_endpoint_id` for endpoint dependency checks, and `idx_connections_pricing_template_id` for template dependency checks.

Connection invariants:
- `api_family` is the compatibility source for access-target validation and runtime planning.
- Product-facing routing surfaces present these rows as Terminal Targets while persisted compatibility remains `connections` and `target_type = "connection"`.
- A connection can be referenced by exactly one model access target in the same profile.
- The partial unique index `uq_model_access_targets_connection_owner` enforces one owner for every non-null `target_connection_id`.
- Public model target authoring cannot attach Terminal Targets by ID. Model detail creates, updates, reorders, and deletes Terminal Targets through model-scoped routes.
- Deleting a Terminal Target removes its owning `model_access_targets.target_connection_id` row in the same operation.
- Connection create/update contracts do not allow client-written `priority`; model-specific ordering changes flow through `/api/models/{model_config_id}/targets/{target_id}/position`.
- OpenAI Terminal Targets require `openai_text_capability` in `responses_only`, `chat_completions_only`, or `dual_native`; non-OpenAI Terminal Targets must keep it `NULL`.
- `openai_text_capability` is the OpenAI text runtime capability source of truth for planning. `responses_only` supports native Responses generation and Responses adjunct operations, `chat_completions_only` supports native Chat Completions, and `dual_native` supports both native text generation shapes. Sibling translation can run only for adapter-approved Chat Completions and Responses shapes when a terminal target is not native for the ingress operation; authored access-target and terminal-target order still decides which compatible native or translated attempt is tried first.
- `openai_probe_endpoint_variant` is retained for existing rows; live Terminal Target authoring uses `openai_text_capability` for OpenAI runtime planning.

### 2.7 `pricing_templates` (profile-scoped reusable token pricing)

Reusable token pricing definitions that can be attached to many Terminal Targets within a profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| name | VARCHAR(200) | NOT NULL | Template name (profile-unique) |
| description | TEXT | NULLABLE | Optional notes |
| pricing_unit | VARCHAR(20) | NOT NULL | Billing unit; application writes `PER_1M` in current flows |
| pricing_currency_code | VARCHAR(3) | NOT NULL | Template currency code |
| input_price | VARCHAR(20) | NOT NULL | Base input token price string |
| output_price | VARCHAR(20) | NOT NULL | Base output token price string |
| cached_input_price | VARCHAR(20) | NOT NULL, DEFAULT '0' | Cache-read input token price string |
| cache_creation_price | VARCHAR(20) | NOT NULL, DEFAULT '0' | Cache-creation input token price string |
| reasoning_price | VARCHAR(20) | NOT NULL, DEFAULT '0' | Reasoning output token price string |
| version | INTEGER | NOT NULL | Auto-incremented on pricing-impacting changes; application-managed value |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraint: `UNIQUE(profile_id, name)`.

Pricing templates use five concrete pricing strings in steady state. Management API writes normalize missing, null, or blank pricing inputs for any of the five pricing fields to `"0"` before decimal validation. Explicit `"0"` means configured free pricing. `MISSING_PRICE_DATA` applies only when a pricing template or runtime pricing snapshot is absent, unusable, or invalid, or when required FX data cannot be applied.

Token costing consumes canonical disjoint token components: base input, cache-read input, cache-creation input, base output, reasoning output, and provider or derived total. `cached_tokens` is derived-only for aggregate and presentation surfaces from cache-read plus cache-creation input tokens.


### 2.8 `header_blocklist_rules` (mixed scope)

Header blocklist is split between global system rules and profile-scoped user rules.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NULLABLE | NULL for system rules; profile FK for user rules |
| name | VARCHAR(200) | NOT NULL | Rule label |
| match_type | VARCHAR(20) | NOT NULL | `exact` or `prefix` |
| pattern | VARCHAR(200) | NOT NULL | Header match token (case-insensitive) |
| enabled | BOOLEAN | NOT NULL | Rule enabled flag; application-managed value |
| is_system | BOOLEAN | NOT NULL | Protected global rule; application-managed value |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints:
- System rule: `is_system = TRUE` implies `profile_id IS NULL`.
- User rule: `is_system = FALSE` implies `profile_id IS NOT NULL`.
- User rule uniqueness: `UNIQUE(profile_id, match_type, pattern)`.

### 2.9 `user_settings` (profile-scoped singleton)

Per-profile costing/report display preferences.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL, UNIQUE | One row per profile |
| report_currency_code | VARCHAR(3) | NOT NULL | Spending report currency; application-managed seed value |
| report_currency_symbol | VARCHAR(5) | NOT NULL | Currency symbol; application-managed seed value |
| timezone_preference | VARCHAR(100) | NULLABLE | Preferred timezone for UI/report rendering |
| request_logs_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Retained legacy per-profile retention field, ignored by current settings APIs |
| statistics_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Retained legacy per-profile retention field, ignored by current settings APIs |
| audit_logs_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Retained legacy per-profile retention field, ignored by current settings APIs |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

### 2.10 `endpoint_fx_rate_settings` (profile-scoped)

Custom FX mappings used by costing within one profile.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| model_id | VARCHAR(200) | NOT NULL | Model identifier in profile scope |
| endpoint_id | INTEGER | NOT NULL | Endpoint reference in profile scope |
| fx_rate | VARCHAR(20) | NOT NULL | Decimal exchange rate |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraint: `UNIQUE(profile_id, model_id, endpoint_id)`.

### 2.11 `request_logs` (partitioned immutable profile attribution)

Telemetry rows have immutable profile attribution captured at request start. Captured upstream attempts in materialized execution envelopes produce one row each. Telemetry-eligible target-resolution/translation planning failures carrying `PlanningFailure`, plus execution failures accepted by the runtime telemetry path, produce synthetic failure rows without an endpoint or connection. The table is range-partitioned by UTC `created_at` day. The partition-compatible primary key is `(created_at, id)`, with `id` still sequence-backed for lookup convenience.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | NOT NULL, sequence-backed, part of PK `(created_at, id)` | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| model_id | VARCHAR(200) | NOT NULL | Model ID used for attempt |
| resolved_target_model_id | VARCHAR(200) | NULLABLE | Final target model selected for the attempt |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| ingress_request_id | VARCHAR(36) | NULLABLE | Prism-generated incoming request grouping ID |
| attempt_number | INTEGER | NULLABLE | Per-ingress attempt order, starting at 1 |
| operation_name | VARCHAR(120) | NULLABLE | Ingress canonical operation name; runtime writers populate it for supported operations |
| upstream_operation_name | VARCHAR(120) | NULLABLE | Provider-facing operation name used for the attempt |
| operation_translation_mode | VARCHAR(80) | NULLABLE | `none`, `openai_responses_to_chat_completions`, or `openai_chat_completions_to_responses` |
| upstream_request_path | VARCHAR(500) | NULLABLE | Sanitized provider-facing operation path |
| provider_correlation_id | VARCHAR(255) | NULLABLE | Best-effort provider-visible correlation ID |
| endpoint_id | INTEGER | NULLABLE | Endpoint snapshot |
| connection_id | INTEGER | NULLABLE | Executed connection snapshot |
| selected_terminal_target_id | INTEGER | NULLABLE | Planner-selected terminal target before execution or no-fit rejection |
| proxy_api_key_id | INTEGER | NULLABLE | Proxy API key snapshot used for the request |
| proxy_api_key_name_snapshot | VARCHAR(200) | NULLABLE | Display-name snapshot for the proxy key at request time |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Endpoint base URL snapshot |
| endpoint_description | TEXT | NULLABLE | Compatibility endpoint-name snapshot text |
| status_code | INTEGER | NOT NULL | Upstream attempt status code, or Prism HTTP status code for a synthetic runtime failure row |
| response_time_ms | INTEGER | NOT NULL | Latency in ms |
| is_stream | BOOLEAN | NOT NULL | Streaming flag |
| input_tokens | INTEGER | NULLABLE | Base input tokens |
| output_tokens | INTEGER | NULLABLE | Base output tokens |
| total_tokens | INTEGER | NULLABLE | Provider total or derived total when available |
| success_flag | BOOLEAN | NULLABLE | Success classification |
| billable_flag | BOOLEAN | NULLABLE | Billing eligibility classification |
| priced_flag | BOOLEAN | NULLABLE | Costing completeness flag |
| unpriced_reason | VARCHAR(50) | NULLABLE | Missing price or token-usage reason |
| reasoning_tokens | INTEGER | NULLABLE | Reasoning output tokens |
| cache_read_input_tokens | INTEGER | NULLABLE | Cache-read input tokens |
| cache_creation_input_tokens | INTEGER | NULLABLE | Cache-creation input tokens |
| input_cost_micros | BIGINT | NULLABLE | Input component cost |
| output_cost_micros | BIGINT | NULLABLE | Output component cost |
| reasoning_cost_micros | BIGINT | NULLABLE | Reasoning component cost |
| cache_read_input_cost_micros | BIGINT | NULLABLE | Cache-read component cost |
| cache_creation_input_cost_micros | BIGINT | NULLABLE | Cache-creation component cost |
| total_cost_original_micros | BIGINT | NULLABLE | Total cost in original pricing currency |
| total_cost_user_currency_micros | BIGINT | NULLABLE | Total cost in reporting currency |
| currency_code_original | VARCHAR(3) | NULLABLE | Pricing currency code |
| report_currency_code | VARCHAR(3) | NULLABLE | Reporting currency code |
| report_currency_symbol | VARCHAR(5) | NULLABLE | Reporting currency symbol |
| fx_rate_used | VARCHAR(20) | NULLABLE | FX rate snapshot |
| fx_rate_source | VARCHAR(30) | NULLABLE | FX rate source |
| pricing_snapshot_unit | VARCHAR(10) | NULLABLE | Pricing unit snapshot |
| pricing_snapshot_input | VARCHAR(20) | NULLABLE | Input price snapshot |
| pricing_snapshot_output | VARCHAR(20) | NULLABLE | Output price snapshot |
| pricing_snapshot_reasoning | VARCHAR(20) | NULLABLE | Reasoning price snapshot |
| pricing_snapshot_cache_read_input | VARCHAR(20) | NULLABLE | Cache-read price snapshot |
| pricing_snapshot_cache_creation_input | VARCHAR(20) | NULLABLE | Cache-creation price snapshot |
| pricing_config_version_used | INTEGER | NULLABLE | Pricing config version used for costing |
| stream_outcome | VARCHAR(50) | NOT NULL, DEFAULT `not_streaming` | Stream classification: `not_streaming`, `completed`, `provider_incomplete`, `client_disconnected`, `upstream_read_error`, `upstream_ended_without_terminal`, or `unknown` |
| stream_error_kind | VARCHAR(50) | NULLABLE | Stream diagnostic kind: `client_write_failed`, `request_context_canceled`, `upstream_read_failed`, or `missing_terminal_event` |
| stream_error_detail | TEXT | NULLABLE | Sanitized request-log-detail-only diagnostic text for stream failures |
| request_path | VARCHAR(500) | NOT NULL | Requested route path |
| error_detail | TEXT | NULLABLE | Error details for failed attempts |
| caller_user_agent | TEXT | NULLABLE | Original caller user agent |
| upstream_user_agent | TEXT | NULLABLE | User-Agent sent upstream |
| completion_duration_ms | INTEGER | NULLABLE | Completion duration after first token/byte when available |
| ttft_ms | INTEGER | NULLABLE | Time to first token/byte when available |
| audit_enabled_at_request | BOOLEAN | NOT NULL, DEFAULT FALSE | Request-time audit enablement snapshot |
| audit_capture_bodies_at_request | BOOLEAN | NOT NULL, DEFAULT FALSE | Request-time body-capture snapshot |
| request_generation_params | JSONB | NULLABLE | Captured request-generation parameter summary |
| request_generation_params_status | VARCHAR(40) | NULLABLE | Generation-parameter capture status |
| created_at | TIMESTAMPTZ | NOT NULL, part of PK `(created_at, id)` | Attempt timestamp and partition key |

Request-log semantics:
- Each captured upstream attempt in a materialized execution envelope writes one row, not one row per incoming runtime request.
- Target-resolution errors attach `PlanningFailure` only for HTTP `503` or `openai_request_translation_unsupported`; those telemetry-eligible planning failures, plus execution failures that enter the runtime failure telemetry path (currently `admission_exhausted`), can write a synthetic row with no `endpoint_id` or `connection_id`.
- Earlier errors such as malformed request bodies, unknown models, and API-family mismatches do not carry `PlanningFailure` and do not write synthetic history.
- When all launched transport attempts fail and execution returns its terminal `502`, the current executor drops its captured attempt list and does not materialize request or usage history for that failure.
- Unsupported or wrong-method requests rejected by the operation registry write no request log, audit log, usage event, or telemetry-outbox row.
- `ingress_request_id` groups the rows created by one incoming runtime request.
- `attempt_number` preserves retry/failover ordering within that group.
- `model_id` records the requested model ID while `resolved_target_model_id` records the final target model ID selected for that attempt.
- `operation_name` is nullable in the schema for compatibility, but materialized rows for registered operations, including synthetic failures, carry a non-empty canonical operation name. Registry rejection creates no row and therefore has no persisted operation name.
- `operation_name` and `request_path` remain ingress-led. `upstream_operation_name`, `operation_translation_mode`, and `upstream_request_path` are additive upstream attribution for native or translated attempts.
- `selected_terminal_target_id` can differ from `connection_id` when the planner selected one terminal target but execution later failed over to another attempt.
- `stream_error_detail` is exposed only by exact request-log detail reads. List and dashboard recent-activity payloads expose `stream_outcome` and `stream_error_kind` without detail text.
- Prism prices only observed usage. `STREAM_USAGE_UNAVAILABLE` marks interrupted or no-terminal stream rows where required tokens are absent; completed streams missing required usage keep `MISSING_TOKEN_USAGE`.
- Token usage fields are canonical disjoint components. `input_tokens` is base input only, `output_tokens` is base output only, and cache-read input, cache-creation input, and reasoning output stay in their split fields.
- Pricing snapshots persist the five concrete pricing strings used for the attempt. Explicit `"0"` prices mean configured free pricing, while absent or invalid pricing snapshots and missing FX data stay unpriced with `MISSING_PRICE_DATA`.

### 2.12 `usage_request_events` (partitioned immutable usage attribution)

Usage-event rows are the finalized source for the unified statistics snapshot. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | NOT NULL, sequence-backed, part of PK `(created_at, id)` | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| ingress_request_id | VARCHAR(36) | NOT NULL, indexed with `profile_id` | Incoming request grouping ID preserved for aggregate attribution and cross-table correlation |
| model_id | VARCHAR(200) | NOT NULL | Requested model ID |
| resolved_target_model_id | VARCHAR(200) | NULLABLE | Final target model selected for the request |
| api_family | VARCHAR(50) | NOT NULL | Fixed runtime compatibility family |
| operation_name | VARCHAR(120) | NULLABLE | Ingress canonical operation name; runtime writers populate it for supported operations |
| upstream_operation_name | VARCHAR(120) | NULLABLE | Provider-facing operation name for finalized attribution |
| operation_translation_mode | VARCHAR(80) | NULLABLE | Translation mode copied from the finalized attempt |
| upstream_request_path | VARCHAR(500) | NULLABLE | Sanitized provider-facing operation path |
| request_path | VARCHAR(500) | NOT NULL | Ingress route path that finalized the event |
| endpoint_id | INTEGER | NULLABLE | Endpoint snapshot |
| endpoint_label_snapshot | TEXT | NOT NULL | Endpoint label captured at runtime for retained aggregate display |
| connection_id | INTEGER | NULLABLE | Executed connection snapshot |
| selected_terminal_target_id | INTEGER | NULLABLE | Planner-selected terminal target for the finalized request |
| proxy_api_key_id | INTEGER | NULLABLE | Proxy API key snapshot |
| proxy_api_key_name_snapshot | VARCHAR(200) | NULLABLE | Proxy key name at event time |
| attempt_count | INTEGER | NOT NULL, CHECK `attempt_count >= 1` | Number of upstream attempts that contributed to the finalized event |
| status_code | INTEGER | NOT NULL | HTTP status code |
| success_flag | BOOLEAN | NOT NULL | Success indicator |
| billable_flag | BOOLEAN | NULLABLE | Billing eligibility classification |
| priced_flag | BOOLEAN | NULLABLE | Costing completeness flag |
| unpriced_reason | VARCHAR(50) | NULLABLE | Missing price or token-usage reason |
| input_tokens | INTEGER | NULLABLE | Base input tokens |
| output_tokens | INTEGER | NULLABLE | Base output tokens |
| total_tokens | INTEGER | NULLABLE | Provider total or derived total when available |
| cache_read_input_tokens | INTEGER | NULLABLE | Cache-read input tokens |
| cache_creation_input_tokens | INTEGER | NULLABLE | Cache-creation input tokens |
| reasoning_tokens | INTEGER | NULLABLE | Reasoning output tokens |
| input_cost_micros | BIGINT | NULLABLE | Input component cost |
| output_cost_micros | BIGINT | NULLABLE | Output component cost |
| cache_read_input_cost_micros | BIGINT | NULLABLE | Cache-read component cost |
| cache_creation_input_cost_micros | BIGINT | NULLABLE | Cache-creation component cost |
| reasoning_cost_micros | BIGINT | NULLABLE | Reasoning component cost |
| total_cost_original_micros | BIGINT | NULLABLE | Total cost in original pricing currency |
| total_cost_user_currency_micros | BIGINT | NULLABLE | Total cost in reporting currency |
| currency_code_original | VARCHAR(3) | NULLABLE | Pricing currency code |
| report_currency_code | VARCHAR(3) | NULLABLE | Reporting currency code |
| report_currency_symbol | VARCHAR(5) | NULLABLE | Reporting currency symbol |
| fx_rate_used | VARCHAR(20) | NULLABLE | FX rate snapshot |
| fx_rate_source | VARCHAR(30) | NULLABLE | FX rate source |
| pricing_snapshot_unit | VARCHAR(10) | NULLABLE | Pricing unit snapshot |
| pricing_snapshot_input | VARCHAR(20) | NULLABLE | Input price snapshot |
| pricing_snapshot_output | VARCHAR(20) | NULLABLE | Output price snapshot |
| pricing_snapshot_cache_read_input | VARCHAR(20) | NULLABLE | Cache-read price snapshot |
| pricing_snapshot_cache_creation_input | VARCHAR(20) | NULLABLE | Cache-creation price snapshot |
| pricing_snapshot_reasoning | VARCHAR(20) | NULLABLE | Reasoning price snapshot |
| pricing_config_version_used | INTEGER | NULLABLE | Pricing config version used for costing |
| response_time_ms | INTEGER | NULLABLE | Final attempt latency in ms |
| completion_duration_ms | INTEGER | NULLABLE | Completion duration after first token/byte when available |
| ttft_ms | INTEGER | NULLABLE | Time to first token/byte when available |
| stream_outcome | VARCHAR(50) | NOT NULL, DEFAULT `not_streaming` | Finalized stream classification copied from the contributing request-log attempt |
| stream_error_kind | VARCHAR(50) | NULLABLE | Finalized stream diagnostic kind without detail text |
| created_at | TIMESTAMPTZ | NOT NULL, part of PK `(created_at, id)` | Event timestamp and partition key |

Usage-event semantics:
- One row captures the finalized usage event for each materialized telemetry envelope and feeds the statistics snapshot.
- `ingress_request_id` preserves the stable request-group identifier shared with the attempt-level `request_logs` rows for the same incoming runtime request.
- `operation_name` is nullable in the schema for compatibility, but registered-operation envelopes materialize a non-empty canonical operation name. Operation-registry rejection creates no usage event.
- `proxy_api_key_name_snapshot` preserves display intent even if the key name later changes.
- Runtime label capture uses the endpoint name, then base URL, then `Endpoint N`, then `Unknown Endpoint`. Synthetic failures use `Unknown Endpoint`.
- `endpoint_label_snapshot` preserves the endpoint display label used by usage snapshots, spending, and Top Endpoints, even if the endpoint is later renamed or deleted. Public stats payloads expose this stored value as `endpoint_label`.
- Upgrade backfill prefers the latest matching request-log endpoint description, then that request log's base URL, then the current endpoint name, current endpoint base URL, `Endpoint N`, and finally `Unknown Endpoint`.
- Request-log list/detail display does not use this usage snapshot. It prefers the current endpoint name, current endpoint base URL, the request log's historical base URL, `Endpoint N`, then `Unknown Endpoint`.
- Usage events keep the final stream outcome and error kind for aggregate explanation, but not `stream_error_detail`.
- Usage events copy canonical disjoint token totals, runtime pricing results, selected-terminal-target metadata, and additive ingress/upstream operation attribution. Aggregate `cached_tokens` is derived from cache-read plus cache-creation input tokens rather than stored as its own runtime component.
- Explicit `"0"` pricing contributes zero-cost component micros on priced events. Rows with absent or invalid pricing snapshots, or missing FX data, remain unpriced with `MISSING_PRICE_DATA`.

Telemetry materialization:
- Runtime handlers hand telemetry to `runtime_telemetry_outbox` for durable or scheduled background processing; the request path does not directly insert the historical tables.
- The materializer transaction inserts `request_logs`, matching `audit_logs`, one `usage_request_events` row, and proxy-key usage together, then deletes the processed outbox row.
- Each audit row receives its linked request-log timestamp. The usage event timestamp aligns with the last request-log attempt timestamp; proxy-key `last_used_at` and `last_used_ip` updates are monotonic within that transaction.

### 2.13 `audit_logs` (partitioned immutable profile attribution)

Audit rows for upstream attempts with immutable profile attribution. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key. Audit-to-request linkage is weak so audit rows can outlive request-log partitions.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | NOT NULL, sequence-backed, part of PK `(created_at, id)` | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| request_log_id | BIGINT | NULLABLE | Weak request-log identifier retained for historical linking |
| request_log_created_at | TIMESTAMPTZ | NULLABLE | Weak request-log partition key retained for historical linking |
| ingress_request_id | VARCHAR(36) | NULLABLE | Weak incoming request grouping ID retained for correlation |
| model_id | VARCHAR(200) | NOT NULL | Model ID |
| endpoint_id | INTEGER | NULLABLE | Endpoint snapshot |
| connection_id | INTEGER | NULLABLE | Connection snapshot |
| endpoint_base_url | VARCHAR(500) | NULLABLE | Endpoint base URL snapshot |
| endpoint_description | TEXT | NULLABLE | Compatibility endpoint-name snapshot text |
| request_method | VARCHAR(10) | NOT NULL | Upstream request method |
| request_url | VARCHAR(2000) | NOT NULL | Upstream request URL |
| request_headers | TEXT | NOT NULL | Upstream request headers; only `authorization`, `x-api-key`, and `x-goog-api-key` values are replaced with `[REDACTED]` |
| request_body | TEXT | NULLABLE | Captured upstream request body |
| response_status | INTEGER | NOT NULL | Upstream response status |
| response_headers | TEXT | NULLABLE | Upstream response headers serialized as captured, without header redaction |
| response_body | TEXT | NULLABLE | Captured final-attempt upstream response body |
| audit_enabled_at_request | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether audit was enabled when the request started |
| audit_capture_bodies_at_request | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether body capture was enabled when the request started |
| request_body_stored | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether request body content was stored |
| response_body_stored | BOOLEAN | NOT NULL, DEFAULT FALSE | Whether response body content was stored |
| is_stream | BOOLEAN | NOT NULL | Streaming flag |
| duration_ms | INTEGER | NOT NULL | Request duration |
| created_at | TIMESTAMPTZ | NOT NULL, part of PK `(created_at, id)` | Audit timestamp and partition key |

Audit-link semantics:
- `request_log_id`, `request_log_created_at`, and `ingress_request_id` are retained as weak metadata.
- Request-log retention does not clear weak-link metadata. Audit list/detail responses expose `request_log_missing=true` only when `request_log_id` and `request_log_created_at` are both non-null and the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves.
- Audit retention and request-log retention are independent global jobs.
- When body capture is enabled, every audit-enabled attempt can store its upstream request body. Only the final attempt can store the captured upstream response body.
- Translated OpenAI audit capture uses upstream-native request and response bodies, never the translated client-facing shape.
- Request and response bodies are not redacted. Other request-header values and all response-header values can also contain sensitive data.

### 2.14 `profile_api_family_audit_settings` (profile-scoped audit policy)

One row per profile and API family controls whether runtime attempts create audit metadata and whether bodies may be stored.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL, ON DELETE CASCADE | Owning profile |
| api_family | VARCHAR(50) | NOT NULL, CHECK IN (`openai`, `anthropic`, `gemini`) | Runtime compatibility family |
| audit_enabled | BOOLEAN | NOT NULL | Whether attempts for this profile/family create audit rows |
| audit_capture_bodies | BOOLEAN | NOT NULL | Whether request and response bodies may be stored |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Constraints:
- `UNIQUE(profile_id, api_family)`.
- `audit_capture_bodies` requires `audit_enabled`.
- Management `PUT /api/settings/audit` full-replaces the three supported family rows for Default profile id `1`.
- Runtime snapshots load policy by profile and model `api_family`; request-time booleans are copied into existing request-log and audit-log provenance fields.

### 2.15 `loadbalance_events` (partitioned immutable profile attribution)

Persistent record of retry-window, ban, recovery, and admission transitions. The table is range-partitioned by UTC `created_at` day and uses `(created_at, id)` as its partition-compatible primary key.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | NOT NULL, sequence-backed, part of PK `(created_at, id)` | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Immutable profile attribution |
| connection_id | INTEGER | NOT NULL | Private connection ID |
| event_type | VARCHAR(32) | NOT NULL | `retry_scheduled`, `retry_exhausted`, `banned`, `unbanned`, `recovered`, `admission_rejected` |
| failure_kind | VARCHAR(20) | NULLABLE | `transient_http`, `connect_error`, `timeout` |
| cycle_retry_attempts | INTEGER | NOT NULL | Retry attempts in the current retry cycle |
| cumulative_retry_attempts | INTEGER | NOT NULL | Retry attempts accumulated for Ban Policy thresholding |
| policy_cycle_retry_attempt_limit | INTEGER | NULLABLE | Strategy cycle limit snapshot for events produced by Ban Policy evaluation |
| policy_ban_cumulative_retry_attempt_threshold | INTEGER | NULLABLE | Strategy cumulative ban threshold snapshot for events produced by Ban Policy evaluation |
| next_retry_at | TIMESTAMPTZ | NULLABLE | Wall-clock time when the next retry cycle can run |
| last_retry_delay_ms | INTEGER | NOT NULL | Last resolved retry-window delay in milliseconds |
| model_id | VARCHAR(200) | NULLABLE | Model ID snapshot |
| endpoint_id | INTEGER | NULLABLE | Endpoint ID snapshot |
| ban_mode | VARCHAR(20) | NULLABLE | `off`, `temporary`, or `until_reset` when relevant |
| banned_until_at | TIMESTAMPTZ | NULLABLE | Temporary-ban expiry when relevant |
| last_success_at | TIMESTAMPTZ | NULLABLE | Successful response time that cleared retry state when relevant |
| created_at | TIMESTAMPTZ | NOT NULL, part of PK `(created_at, id)` | Event timestamp and partition key |

Event snapshot semantics:
- Ban Policy event rows keep immutable SQL storage snapshots in `policy_cycle_retry_attempt_limit` and `policy_ban_cumulative_retry_attempt_threshold` from the strategy evaluated at event time.
- Event list/detail APIs expose those snapshots as `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` so the public payload matches the strategy contract.
- `cycle_retry_attempts`, `cumulative_retry_attempts`, and `last_retry_delay_ms` are constrained non-negative. A policy cycle limit is `1..50`; a policy ban threshold is `0..500` and, when nonzero alongside a cycle limit, is not lower than that limit.
- The runtime ensures the daily `loadbalance_events` partition before inserting an event.
- Event lists are scoped by `profile_id` and `model_id`. Incident lists include only `banned`, `unbanned`, `recovered`, and `retry_exhausted` history, while active bans are supplied from process-local runtime state.
- Current-state records do not store strategy threshold fields; policy thresholds belong to immutable event snapshots from the owner model's strategy.
- Historical events can explain inclusive threshold behavior even after a strategy changes later.

### 2.16 `log_retention_settings` (global singleton)

Global normal-retention policy for partitioned log tables.

| Column | Type | Constraints | Description |
|---|---|---|---|
| singleton_key | VARCHAR(20) | PK, CHECK = `global` | Singleton row key |
| request_logs_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Global request-log retention window |
| audit_logs_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Global audit-log retention window |
| statistics_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Global `usage_request_events` retention window |
| loadbalance_events_retention_days | INTEGER | NULLABLE, CHECK >= 1 | Global load-balance event retention window |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Creation timestamp |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Last update timestamp |

Retention semantics:
- Normal retention is global across all profiles and implemented by durable `log_retention` jobs with `profile_id = 0`.
- `PUT /api/settings/log-retention` is a full replacement: omitted nullable policy fields are written as `NULL`. The database constrains all four day values to `NULL` or at least `1`; the current Go-layer request validator explicitly checks the request, statistics, and audit values.
- `backend/internal/platform/logretention` maintains exactly 15 UTC daily partitions for each managed table: today through today plus 14 days. Startup ensures the horizon, and the low-priority maintenance worker refreshes it hourly.
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

### 2.17 `management_jobs` (durable management work queue)

Durable queue for broad management operations. Log-retention jobs are global and use `profile_id = 0`. Audit-delete jobs retain the requesting profile ID for ownership and API lookup, but execution delegates to global `audit_logs` partition retention without a profile predicate.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | TEXT | PK | Job identifier |
| type | TEXT | NOT NULL, CHECK IN (`audit_delete`, `log_retention`) | Job kind |
| state | TEXT | NOT NULL, CHECK IN (`queued`, `running`, `cancel_requested`, `cancelled`, `succeeded`, `failed`) | Job lifecycle state |
| requested_by | TEXT | NOT NULL | Requesting principal or scope label |
| requested_at | TIMESTAMPTZ | NOT NULL | Request timestamp |
| started_at | TIMESTAMPTZ | NULLABLE | First worker start time |
| finished_at | TIMESTAMPTZ | NULLABLE | Terminal-state timestamp |
| priority | TEXT | NOT NULL, DEFAULT `maintenance` | Worker priority lane |
| idempotency_key | TEXT | NULLABLE | Optional dedupe key with partial unique index by `type` and `requested_by` |
| profile_id | INTEGER | NOT NULL | Requesting-profile ownership for `audit_delete`; `0` sentinel for global `log_retention` |
| scope_json | JSONB | NOT NULL | Job-specific delete or retention scope |
| reason | TEXT | NOT NULL | Operator reason or default retention reason |
| rows_matched_estimate | BIGINT | NULLABLE | Optional estimated matched rows |
| rows_deleted | BIGINT | NOT NULL, DEFAULT 0 | Accumulated boundary-delete rows; dropped-partition rows are not counted |
| batches_completed | BIGINT | NOT NULL, DEFAULT 0 | Completed worker batches |
| progress_json | JSONB | NOT NULL, DEFAULT `{}` | Worker progress cursor/state |
| cancel_requested | BOOLEAN | NOT NULL, DEFAULT FALSE | Cancellation flag |
| attempt_count | INTEGER | NOT NULL, DEFAULT 0 | Worker attempt count |
| max_attempts | INTEGER | NOT NULL, DEFAULT 8 | Retry ceiling |
| next_attempt_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Next claim time |
| locked_by | TEXT | NULLABLE | Worker lease owner |
| locked_until | TIMESTAMPTZ | NULLABLE | Worker lease expiry |
| last_heartbeat_at | TIMESTAMPTZ | NULLABLE | Last worker heartbeat |
| error_code | TEXT | NULLABLE | Terminal or retry error code |
| error_message | TEXT | NULLABLE | Sanitized error detail |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Creation timestamp |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Last update timestamp |

Job execution semantics:
- The low-priority management-jobs worker checks configured retention policies every five seconds and creates table/day-idempotent global retention jobs.
- `audit_delete` stores a requesting profile ID but rewrites its execution scope to `audit_logs` partition retention without applying that profile ID as a row predicate.
- `rows_deleted` and `management_job_events.rows_deleted` count only rows removed from the cutoff-overlapping boundary partition. Rows removed by dropping whole partitions are represented in `progress_json.dropped_partitions`, not in the row count.

### 2.18 `management_job_events`

Append-only event stream for management job status and progress.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | PK, sequence-backed | Unique identifier |
| job_id | TEXT | FK -> management_jobs.id, NOT NULL, ON DELETE CASCADE | Owning job |
| event_type | TEXT | NOT NULL | Event kind such as `created` or `cancel_requested` |
| message | TEXT | NOT NULL, DEFAULT empty string | Safe operator-facing event message |
| rows_deleted | BIGINT | NOT NULL, DEFAULT 0 | Rows deleted by the event batch |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Event timestamp |

### 2.19 `routing_connection_runtime_state` (retained compatibility schema, `UNLOGGED`)

Retained compatibility schema for historical runtime-state rows. The production hot path does not read or write this table. It remains `UNLOGGED` in the baseline migration, but live admission, retry, ban, and latency state is held by `LocalRuntimeStateStore` in the backend process.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| connection_id | INTEGER | FK -> connections.id, NOT NULL | Private connection under Ban Policy tracking |
| window_started_at | TIMESTAMPTZ | NULLABLE | Current QPS window start |
| window_request_count | INTEGER | NOT NULL | Requests admitted in current one-second window; application-managed zero value |
| in_flight_non_stream | INTEGER | NOT NULL | Current non-stream reservations; application-managed zero value |
| in_flight_stream | INTEGER | NOT NULL | Current stream reservations; application-managed zero value |
| cycle_retry_attempts | INTEGER | NOT NULL | Retry attempts in the current retry cycle |
| cumulative_retry_attempts | INTEGER | NOT NULL | Retry attempts accumulated for Ban Policy thresholding |
| next_retry_at | TIMESTAMPTZ | NULLABLE | Wall-clock time when the next retry cycle can run |
| last_retry_delay_ms | INTEGER | NOT NULL | Last resolved retry-window delay in milliseconds |
| ban_mode | VARCHAR(20) | NOT NULL | `off`, `temporary`, or `until_reset` |
| banned_until_at | TIMESTAMPTZ | NULLABLE | Temporary-ban expiry when relevant |
| last_failure_kind | VARCHAR(20) | NULLABLE | Latest retryable failure kind: `transient_http`, `connect_error`, or `timeout` |
| last_success_at | TIMESTAMPTZ | NULLABLE | Successful response time that cleared retry state when relevant |
| live_p95_latency_ms | INTEGER | NULLABLE | Passive-request latency signal |
| created_at | TIMESTAMPTZ | NOT NULL | Row creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last mutation timestamp; application-managed |

Constraints:
- `UNIQUE(profile_id, connection_id)`.
- Admission and retry counters are non-negative.
- `ban_mode` is restricted to `off`, `temporary`, or `until_reset`.
- `last_failure_kind` is restricted to `transient_http`, `connect_error`, or `timeout` when present.

The columns document the retained schema only. They do not describe the current production state source.

### 2.20 `routing_connection_runtime_leases` (retained compatibility schema, `UNLOGGED`)

Retained compatibility schema for historical runtime leases. The production hot path does not read or write lease rows; live in-flight accounting is process-local.

| Column | Type | Constraints | Description |
|---|---|---|---|
| lease_token | VARCHAR(64) | PK | Lease identifier |
| profile_id | INTEGER | FK -> profiles.id, NOT NULL | Owning profile |
| connection_id | INTEGER | FK -> connections.id, NOT NULL | Private connection under Ban Policy tracking |
| lease_kind | VARCHAR(20) | NOT NULL | `stream` or `non_stream` |
| expires_at | TIMESTAMPTZ | NOT NULL | Historical lease expiry |
| heartbeat_at | TIMESTAMPTZ | NULLABLE | Historical stream heartbeat |
| created_at | TIMESTAMPTZ | NOT NULL | Row creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last mutation timestamp; application-managed |

Constraints:
- `lease_kind` is restricted to `stream` or `non_stream`.

### 2.21 `app_auth_settings` (singleton)

Global operator authentication settings and credentials.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| singleton_key | VARCHAR(20) | NOT NULL, UNIQUE | `app` |
| auth_enabled | BOOLEAN | NOT NULL | Auth toggle; application-managed value |
| username | VARCHAR(200) | NULLABLE | Operator username |
| email | VARCHAR(320) | NULLABLE | Retained legacy email column, unused by current auth responses |
| pending_email | VARCHAR(320) | NULLABLE | Retained legacy pending email column, unused by current auth responses |
| password_hash | TEXT | NULLABLE | Argon2 password hash |
| email_bound_at | TIMESTAMPTZ | NULLABLE | Retained legacy email timestamp |
| email_verification_code_hash | VARCHAR(64) | NULLABLE | Retained legacy email-code hash |
| email_verification_expires_at | TIMESTAMPTZ | NULLABLE | Retained legacy email-code expiry |
| email_verification_attempt_count | INTEGER | NOT NULL | Retained legacy email-code attempt count; application-managed zero value |
| must_change_password | BOOLEAN | NOT NULL | First-login follow-up flag; application-managed value |
| last_login_at | TIMESTAMPTZ | NULLABLE | Most recent successful login |
| token_version | INTEGER | NOT NULL | Global token revocation version; application-managed zero value |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

### 2.22 `refresh_tokens`

Cookie-backed management sessions with family rotation and revocation.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| auth_subject_id | INTEGER | FK -> app_auth_settings.id, NOT NULL | Singleton operator auth subject |
| token_hash | VARCHAR(64) | NOT NULL, UNIQUE | SHA-256 hash of the refresh token |
| session_duration | VARCHAR(20) | NOT NULL | Requested session lifetime bucket; application-managed default is `7_days` |
| expires_at | TIMESTAMPTZ | NOT NULL | Refresh-token expiry |
| rotated_from_id | INTEGER | FK -> refresh_tokens.id, NULLABLE | Previous token in the family |
| revoked_at | TIMESTAMPTZ | NULLABLE | Revocation timestamp |
| last_used_at | TIMESTAMPTZ | NULLABLE | Most recent redemption time |
| user_agent | TEXT | NULLABLE | Client user-agent snapshot |
| ip_address | VARCHAR(100) | NULLABLE | Client IP snapshot |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |

### 2.23 `proxy_api_keys`

Runtime data-plane credentials.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | INTEGER | PK, sequence-backed | Unique identifier |
| name | VARCHAR(200) | NOT NULL | Key label |
| key_prefix | VARCHAR(200) | NOT NULL, UNIQUE | Public prefix |
| key_hash | VARCHAR(64) | NOT NULL | SHA-256 hash |
| last_four | VARCHAR(4) | NOT NULL | Display suffix |
| is_active | BOOLEAN | NOT NULL | Active flag; application-managed value |
| expires_at | TIMESTAMPTZ | NULLABLE | Expiration timestamp |
| last_used_at | TIMESTAMPTZ | NULLABLE | Most recent proxy use |
| last_used_ip | VARCHAR(100) | NULLABLE | Most recent proxy client IP |
| created_by_auth_subject_id | INTEGER | FK -> app_auth_settings.id, NULLABLE | Operator who created the key |
| notes | TEXT | NULLABLE | Operator notes |
| rotated_from_id | INTEGER | FK -> proxy_api_keys.id, NULLABLE | Previous key in a rotation chain |
| created_at | TIMESTAMPTZ | NOT NULL | Creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last update timestamp; application-managed |

Rotation and expiry semantics:
- Creation is limited to 100 unexpired rows. Inactive but unexpired rows still count; expired rows do not.
- Rotation creates a successor row and preserves the predecessor link, name, notes, creator, active state, and any future expiry. It immediately sets the predecessor inactive and expired. An already expired key cannot rotate.
- Update can disable a key or set its expiry. Runtime publication includes only keys that are both active and unexpired.
- Delete is a hard delete. `request_logs.proxy_api_key_id` and `usage_request_events.proxy_api_key_id` become `NULL` through `ON DELETE SET NULL`, while their name snapshots remain. A successor's `rotated_from_id` is also set to `NULL` if its predecessor is deleted.
- Proxy-key use updates `last_used_at`, `last_used_ip`, and `updated_at` monotonically, so an older telemetry event cannot overwrite a newer usage observation.

### 2.24 Additional Live Platform Tables

These live tables are internal platform state rather than primary product configuration surfaces. They remain part of the active schema and are owned by their platform packages.

| Table | Scope | Key columns and purpose |
|---|---|---|
| `user_agent_client_rules` | system global or profile-scoped | `id`, nullable `profile_id`, `name`, `pattern`, `enabled`, `is_system`, `created_at`, `updated_at`; scope is constrained so system rows have `profile_id IS NULL` and user rows have `profile_id IS NOT NULL`. User patterns may repeat; system patterns are unique. Patterns are validated regular expressions and evaluated case-insensitively. Enabled user rules sort before enabled system rules, then by id, for display classification. System rules may only change `enabled` and cannot be deleted; user rules are mutable and deletable. `client_rule_id` resolves an enabled in-scope rule and filters only non-empty caller User-Agent values, never upstream User-Agent values. |
| `login_throttle_ledger` | auth singleton support | Composite PK `(subject_key, remote_address)`, `failure_count`, failure timestamps, `locked_until`, timestamps; tracks login throttling state |
| `management_outbox` | management side effects | `id`, `operation_id`, `event_type`, aggregate identity/version, unique `dedupe_key`, `payload`, status `pending|processing|retry|succeeded|failed_permanent`, attempt/lock fields, actor/trace metadata, timestamps |
| `runtime_cache_generations` | runtime cache freshness | Composite PK `(domain, scope_type, scope_id)`, `version >= 0`, `updated_at`, `updated_by`, and `reason`; generation vectors make runtime snapshots fail closed or refresh when management mutations advance cache state |
| `runtime_telemetry_outbox` | profile-scoped runtime side-effect handoff | `id`, `profile_id`, `ingress_request_id`, `payload`, `created_at`; durable runtime telemetry handoff rows are materialized by background workers and then deleted |
| `alert_webhook_outbox` | durable failover incident webhook delivery | `id`, `event_type`, `payload_json`, unique `idempotency_key`, status `queued|sending|sent|dead`, attempt count, max attempts, next attempt, lock fields, sent/dead-letter timestamps, last error, timestamps; payloads carry `event_type`, `connection_id`, `endpoint_id`, `model_id`, optional `banned_until_at`, and `occurred_at` |
| `loadbalance_round_robin_state` | retained compatibility schema | `id`, `profile_id`, `model_config_id`, `next_cursor`, timestamps, `next_cursor >= 0`, and unique `(profile_id, model_config_id)`. Production round-robin cursors are process-local and this table is not used by the hot path. |

## 3. Selected Indexes, Constraints, and Foreign Keys

`backend/migrations/000001_initial_schema.sql` is the complete and exact schema source. The following DDL is a selected set of high-centrality constraints and indexes; it is intentionally not a complete index or foreign-key listing. The baseline declares the shown partition-root indexes with `ON ONLY`; inspect the live child partitions when diagnosing per-partition indexes.

```sql
-- Profiles
CREATE UNIQUE INDEX uq_profiles_single_active ON profiles(is_active) WHERE is_active = TRUE;
CREATE UNIQUE INDEX uq_profiles_single_default ON profiles(is_default) WHERE is_default = TRUE;
ALTER TABLE profiles ADD CONSTRAINT profiles_name_key UNIQUE(name);
CREATE INDEX idx_profiles_deleted_at ON profiles(deleted_at);

-- Scoped uniqueness
ALTER TABLE model_configs ADD CONSTRAINT uq_model_configs_profile_model_id UNIQUE(profile_id, model_id);
ALTER TABLE model_access_targets ADD CONSTRAINT uq_model_access_targets_source_position UNIQUE(source_model_config_id, "position") DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX uq_model_access_targets_source_target_model ON model_access_targets(source_model_config_id, target_model_config_id) WHERE target_model_config_id IS NOT NULL;
CREATE UNIQUE INDEX uq_model_access_targets_source_target_connection ON model_access_targets(source_model_config_id, target_connection_id) WHERE target_connection_id IS NOT NULL;
CREATE UNIQUE INDEX uq_model_access_targets_connection_owner ON model_access_targets(target_connection_id) WHERE target_connection_id IS NOT NULL;
ALTER TABLE endpoints ADD CONSTRAINT uq_endpoints_profile_name UNIQUE(profile_id, name);
ALTER TABLE endpoint_fx_rate_settings ADD CONSTRAINT uq_fx_profile_model_endpoint UNIQUE(profile_id, model_id, endpoint_id);
ALTER TABLE user_settings ADD CONSTRAINT uq_user_settings_profile_id UNIQUE(profile_id);
ALTER TABLE profile_api_family_audit_settings ADD CONSTRAINT uq_profile_api_family_audit_settings_profile_family UNIQUE(profile_id, api_family);

-- Performance indexes
CREATE INDEX idx_model_configs_profile_model_enabled ON model_configs(profile_id, model_id, is_enabled);
CREATE INDEX idx_model_access_targets_profile_source_position ON model_access_targets(profile_id, source_model_config_id, "position");
CREATE INDEX idx_model_access_targets_target_model ON model_access_targets(target_model_config_id) WHERE target_model_config_id IS NOT NULL;
CREATE INDEX idx_model_access_targets_connection ON model_access_targets(target_connection_id) WHERE target_connection_id IS NOT NULL;
CREATE INDEX idx_endpoints_profile_position ON endpoints(profile_id, "position");
CREATE INDEX idx_connections_profile_family_active_priority ON connections(profile_id, api_family, is_active, priority);
CREATE INDEX idx_connections_endpoint_id ON connections(endpoint_id);
CREATE INDEX idx_connections_pricing_template_id ON connections(pricing_template_id);
CREATE INDEX idx_fx_profile_model_endpoint ON endpoint_fx_rate_settings(profile_id, model_id, endpoint_id);
CREATE INDEX idx_request_logs_profile_created_at ON ONLY request_logs(profile_id, created_at);
CREATE INDEX idx_request_logs_ingress_request_id ON ONLY request_logs(ingress_request_id);
CREATE INDEX idx_request_logs_billable_flag ON ONLY request_logs(billable_flag);
CREATE INDEX idx_request_logs_priced_flag ON ONLY request_logs(priced_flag);
CREATE INDEX ix_request_logs_api_family ON ONLY request_logs(api_family);
CREATE INDEX ix_request_logs_connection_id ON ONLY request_logs(connection_id);
CREATE INDEX ix_request_logs_endpoint_id ON ONLY request_logs(endpoint_id);
CREATE INDEX ix_request_logs_id ON ONLY request_logs(id);
CREATE INDEX ix_request_logs_model_id ON ONLY request_logs(model_id);
CREATE INDEX ix_request_logs_proxy_api_key_id ON ONLY request_logs(proxy_api_key_id);
CREATE INDEX ix_request_logs_status_code ON ONLY request_logs(status_code);
CREATE INDEX idx_usage_request_events_profile_created_at ON ONLY usage_request_events(profile_id, created_at);
CREATE INDEX idx_usage_request_events_profile_ingress_request ON ONLY usage_request_events(profile_id, ingress_request_id);
CREATE INDEX idx_usage_request_events_ingress_request_id ON ONLY usage_request_events(ingress_request_id);
CREATE INDEX ix_usage_request_events_api_family ON ONLY usage_request_events(api_family);
CREATE INDEX ix_usage_request_events_connection_id ON ONLY usage_request_events(connection_id);
CREATE INDEX ix_usage_request_events_endpoint_id ON ONLY usage_request_events(endpoint_id);
CREATE INDEX ix_usage_request_events_id ON ONLY usage_request_events(id);
CREATE INDEX ix_usage_request_events_model_id ON ONLY usage_request_events(model_id);
CREATE INDEX ix_usage_request_events_proxy_api_key_id ON ONLY usage_request_events(proxy_api_key_id);
CREATE INDEX idx_audit_logs_profile_created_at ON ONLY audit_logs(profile_id, created_at);
CREATE INDEX idx_loadbalance_events_profile_created ON ONLY loadbalance_events(profile_id, created_at);
CREATE INDEX idx_loadbalance_events_connection ON ONLY loadbalance_events(connection_id, created_at);
CREATE INDEX idx_loadbalance_events_event_type ON ONLY loadbalance_events(event_type);
CREATE UNIQUE INDEX idx_alert_webhook_outbox_idempotency_key ON alert_webhook_outbox(idempotency_key);
CREATE INDEX idx_alert_webhook_outbox_due ON alert_webhook_outbox(next_attempt_at, created_at, id) WHERE status = 'queued';
CREATE INDEX idx_alert_webhook_outbox_stale_locks ON alert_webhook_outbox(locked_until) WHERE status = 'sending';
CREATE INDEX idx_alert_webhook_outbox_dead_letters ON alert_webhook_outbox(dead_lettered_at DESC) WHERE status = 'dead';
CREATE INDEX idx_routing_connection_runtime_state_profile_connection ON routing_connection_runtime_state(profile_id, connection_id);
CREATE INDEX idx_routing_connection_runtime_leases_profile_connection ON routing_connection_runtime_leases(profile_id, connection_id);
CREATE INDEX idx_routing_connection_runtime_leases_expires_at ON routing_connection_runtime_leases(expires_at);
CREATE INDEX idx_runtime_cache_generations_domain_scope ON runtime_cache_generations(domain, scope_type, scope_id, version);
CREATE UNIQUE INDEX uq_hbr_system_match_pattern ON header_blocklist_rules(match_type, pattern) WHERE is_system = TRUE;
CREATE UNIQUE INDEX uq_uacr_system_pattern ON user_agent_client_rules(pattern) WHERE is_system = TRUE;

-- Auth tables
CREATE INDEX idx_refresh_tokens_revoked_at ON refresh_tokens(revoked_at);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
CREATE INDEX idx_proxy_api_keys_is_active ON proxy_api_keys(is_active);
```

Selected foreign-key deletion boundaries:

| Child reference | Parent | `ON DELETE` behavior |
|---|---|---|
| profile-owned configuration rows | `profiles(id)` | Generally `CASCADE`; historical `request_logs`, `usage_request_events`, `audit_logs`, and `loadbalance_events` use `RESTRICT` |
| `connections.endpoint_id`, `connections.pricing_template_id` | `endpoints(id)`, `pricing_templates(id)` | `RESTRICT` |
| `model_access_targets(source_model_config_id, profile_id)` | `model_configs(id, profile_id)` | `CASCADE` |
| `model_access_targets(target_model_config_id, profile_id)` | `model_configs(id, profile_id)` | `RESTRICT` |
| `model_access_targets(target_connection_id, profile_id)` | `connections(id, profile_id)` | `RESTRICT` |
| `request_logs.proxy_api_key_id`, `usage_request_events.proxy_api_key_id` | `proxy_api_keys(id)` | `SET NULL` |
| `proxy_api_keys.created_by_auth_subject_id`, `proxy_api_keys.rotated_from_id` | auth subject or predecessor key | `SET NULL` |
| `refresh_tokens.auth_subject_id` | `app_auth_settings(id)` | `CASCADE` |
| `refresh_tokens.rotated_from_id` | `refresh_tokens(id)` | `SET NULL` |
| retained runtime-state and lease connection/profile references | `connections(id)`, `profiles(id)` | `CASCADE` |
| `loadbalance_round_robin_state.model_config_id` | `model_configs(id)` | `CASCADE`; its stored `profile_id` has no separate FK in the baseline |

## 4. Relationship and Ownership Rules

- Profile-scoped entities include `model_configs`, `model_access_targets`, `loadbalance_strategies`, `endpoints`, `connections`, `pricing_templates`, `user_settings`, `endpoint_fx_rate_settings`, `profile_api_family_audit_settings`, `runtime_telemetry_outbox`, requesting-profile `audit_delete` jobs, user `header_blocklist_rules`, and user `user_agent_client_rules`.
- `routing_connection_runtime_state`, `routing_connection_runtime_leases`, and `loadbalance_round_robin_state` retain profile identifiers as compatibility schema, but they are not the production runtime-state source.
- `app_auth_settings` is the singleton auth root for `refresh_tokens` and `proxy_api_keys`.
- `request_logs`, `usage_request_events`, `audit_logs`, and `loadbalance_events` keep immutable `profile_id` attribution and are not rewritten when the runtime profile snapshot changes.
- `request_logs.ingress_request_id` is the canonical operator drill-in key for grouped request investigation.
- `audit_logs` intentionally has no foreign key to partitioned `request_logs`; its request identifiers are weak historical metadata.
- Cross-profile resource lookups are treated as not found (`404`) because management scope is pinned to Default profile id `1`.
- Private connection create/update must enforce profile consistency between the connection and endpoint references. The single owner is enforced through `model_access_targets.target_connection_id`.

## 5. Deletion and Retention Semantics

- Profile deletion routes are not exposed while multi-profile management is frozen.
- Historical telemetry/audit retention is independent; routine profile delete does not erase historical attribution rows.
- Proxy-key hard deletion clears foreign-key IDs from request and usage history but leaves stored name snapshots intact.
- Partition retention can drop whole UTC-day child tables and delete only the cutoff-overlapping boundary rows; deletion counts do not estimate rows removed by partition drops.

## 6. Runtime Isolation Notes

- Proxy routing always resolves against frozen Default profile id `1`.
- Production creates a fresh `LocalRuntimeStateStore` on process startup. Connection admission counters, Ban Policy state, latency signals, connection round-robin cursors, and access-target round-robin cursors live in process memory.
- The process-local store scopes connection state by profile and connection, and round-robin state by profile/model or profile/source-model/strategy/target-set. A normal restart, crash, or replacement process loses all of this state.
- Production does not reload, reconcile, compact, or repair process-local state from `routing_connection_runtime_state`, `routing_connection_runtime_leases`, or `loadbalance_round_robin_state`.
- Management current-state and active-ban reads query the process-local provider. Reset operations delete process-local connection state and the associated model round-robin cursor.
- Failures are classified as `transient_http`, `connect_error`, or `timeout`; retryable HTTP responses use the same retry-window delay/backoff/jitter policy path as transport failures.
- Ban Mode thresholding uses cumulative retry attempts for the private connection owned by the terminal model path.
- Non-retryable client errors do not force-clear existing process-local current state; successful `2xx` responses clear local retry and ban state for the connection.
- Header blocklist at runtime is resolved as: all enabled system rules + enabled user rules for frozen Default profile id `1`.

## 7. Invariant Notes

- Runtime hot state is process-local and is reset on every process start.
- The baseline migration remains the exact source for PostgreSQL column types, sequences, constraints, indexes, and foreign keys.
