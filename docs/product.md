# Product Specification: Prism

## 1. Product Overview

Prism is a lightweight, self-hosted application that acts as a unified proxy for multiple LLM API families. It lets a single user configure, route, and load-balance requests through a web-based management UI.

## 2. Problem Statement

Developers and power users working with multiple LLM API families face:
- Managing multiple API keys and base URLs across different tools
- No unified endpoint for switching between API families
- No automatic failover when an API family is down or rate-limited
- Manual configuration changes when rotating keys or endpoints

## 3. Target User

Single operator (developer/power user) running the application locally or on a local network. Prism supports optional operator authentication for management APIs and proxy API keys for runtime traffic. Management configuration is pinned to frozen Default profile id `1` while runtime traffic always resolves to frozen Default profile id `1`; `X-Profile-Id` and profile fields are compatibility/storage attribution only. This is not auth multi-tenancy.

## 4. Core Features

### 4.1 Multi-Family Proxy
- Operation-registered proxy support for explicit OpenAI Chat Completions, Responses, Anthropic Messages/count-token, and Gemini generate/stream/count-token operations
- Supports both streaming (SSE) and non-streaming responses
- Preserves native request/response formats per API family
- Runtime compatibility is fixed by `api_family`
- `GET /v1/models` is local and returns the OpenAI `object`/`data` list for enabled OpenAI models; query parameters do not select an alternate response shape

### 4.2 Model Configuration
- Map each model to a fixed runtime `api_family`
- OpenAI models also carry an `openai_accepted_format` of `responses_only`, `chat_completions_only`, or `dual_native`; strict mode equality requires every access target (model or Terminal Target) to use the identical mode
- OpenAI models independently carry an `openai_image_operations` of `generations`, `edits`, or `generations_and_edits`; an OpenAI model must declare at least one of the two dimensions, so a pure image model such as `gpt-image-2` carries no text mode at all
- Models expose one ordered `access_targets` list whose rows point to same-family, same-mode models (Model Targets) or model-private Terminal Targets; both types share one global `position` and are type-neutral peers of the same mixed order
- Terminal Targets carry endpoint, costing, health, admission-limit, and auth metadata as model-private endpoint bindings owned by one model; an OpenAI Terminal Target's `openai_text_capability` must equal the owner model's `openai_accepted_format`, while its `openai_image_capability` only has to cover the owner's `openai_image_operations` and may serve more
- Select which access targets are enabled for each model; enabled models require at least one enabled target
- CRUD operations for all configurations are available via REST API

### 4.3 Unified Model Access Routing
- Model Target rows and Terminal Target rows are type-neutral peers of the same authored mixed order; `single`, `fill-first`, and `round-robin` run once over the enabled mixed rows, and no target type holds a hidden priority tier
- Model-target entries must stay within the same `api_family`, cannot target themselves, and cannot introduce cycles
- A Model Target row is an atomic parent peer: entering it recursively resolves the child model with the child's own strategy, and the child's attempts stay one contiguous block in the parent result
- A Terminal Target outside its routing window is skipped exactly like any other candidate-local miss: planning moves on to the next peer in effective order rather than failing the request
- Each model owns its reusable load-balance strategy, so nested model targets evaluate strategy and Ban Policy at their own graph level
- Model IDs are unique within a profile; the same model ID can exist in different profiles without collision
- Gateway resolves the access graph before Terminal Target planning: incoming request for a public model -> final target model and Terminal Target -> upstream request
- For Gemini API paths (e.g., `/v1beta/models/{model}:generateContent`), the proxy rewrites the model ID segment in the URL path to the resolved final target model ID when needed
- Gemini streaming is path-native: `/v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`
- Once one final target path is selected for an attempt, retries stay inside that target path's Terminal Target plan for that attempt.

### 4.4 Load Balancing & Failover
- For models with multiple reachable Terminal Targets:
  - **Automatic failover** when an attempt returns a failover-triggering status (`403`, `422`, `429`, `500`, `502`, `503`, `504`, `529` by default) or raises connection/timeout errors
  - Models attach one reusable explicit Ban Policy strategy using `single`, `fill-first`, or `round-robin` routing
  - The three strategies act on the same enabled mixed access-target rows: `single` takes only the first enabled mixed peer, `fill-first` walks the authored mixed order, and `round-robin` rotates the direct mixed rows once per request while each child model keeps its own cursor
  - Two consequences of combining a routing schedule with these strategies are worth stating outright. Under `round-robin`, an out-of-window row still occupies its cursor slot, so the first in-window row after a run of `k` out-of-window rows takes `(k+1)/N` of the first attempts while that run lasts; availability is unaffected because every in-window row is still tried on failure. Under `single`, only the first enabled row is ever considered, so scheduling that row takes the whole model offline outside its window — `single` and routing schedules are effectively incompatible, and a time-based configuration should use `fill-first` or `round-robin`
  - Upstream request timing uses shared backend timeout settings, while Ban Policy owns retry windows, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, `temporary` or `until_reset` bans, and failover status codes
  - Ban Policy thresholds are inclusive: retry-cycle exhaustion uses `cycle_retry_attempts >= cycle_retry_attempt_limit`, and bans use `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`
- Failover-worthy HTTP responses are governed by the attached strategy's configured failure status codes and retry-window settings
  - Non-failover client errors outside the configured failure status set (for example `400` or `404` with default policies) do not force-clear existing Ban Policy state
  - Each Terminal Target can optionally define `qps_limit`, `max_in_flight_non_stream`, and `max_in_flight_stream`; `null` means unlimited
  - Runtime admission, Ban Policy, lease, and round-robin state is process-local and is intentionally ephemeral across process restarts; retained SQL hot-state tables are compatibility schema, not the production hot path
- Proxy request forwarding may apply compatibility normalizations while preserving API-family-native response formats
- Model-owned overflow replay and exact facade routing have been removed. After headers, flush, SSE, or any client-visible bytes commit downstream output, Prism does not switch upstreams for that stream.
- Request-log materialization distinguishes actual upstream attempts from synthetic failures. Telemetry-eligible target-resolution or native-compatibility errors carrying `PlanningFailure`, plus `admission_exhausted`, can produce synthetic rows without endpoint/connection. Malformed bodies, unknown models, API-family mismatches, registry-rejected requests, and the terminal all-transport `502` path do not retain request history.
- Request-log detail preserves requested public model identity, final target identity, selected Terminal Target, endpoint, operation names, and translation mode through flat fields.
- Retry scheduling, retry exhaustion, bans, unbans, recovery, and admission-rejection transitions are persisted as `loadbalance_events` for audit and observability.

### 4.5 Default-Profile Endpoints & Terminal Targets
- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Models** carry fixed `api_family` metadata.
- **Terminal Targets** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile. They can also carry per-target custom HTTP headers, an optional static JSON request-body parameter overlay, and an optional routing schedule that limits which parts of the week the target may be selected (see §4.12, §4.13, and §4.17).
- Endpoints can be reused across multiple models within the same profile; each Terminal Target keeps its own header and request-parameter configuration.
- Deleting an endpoint is blocked if any Terminal Targets in that profile still reference it.

### 4.6 Terminal Target Request Health
- Manual Terminal Target test actions are removed from the management API and UI.
- Backend request-derived stats can expose Terminal Target success-rate and routing-health read models for real runtime traffic. These are API/read-model capabilities, not a current per-target health-badge workflow.
- The Models page renders plain telemetry text for each model: 24-hour success rate, P95 latency, and 24-hour request count, plus a 30-day spend value. Missing success data is shown as `- Success`; there are no colored success-rate thresholds or health badges.
- The current model-detail UI does not render Terminal Target success-rate indicators, and the dashboard does not render the backend `routing_health_map` response field.
- Model detail renders process-local Ban Policy runtime state for each Terminal Target: `state` (available / retry_wait / banned), next-retry or ban expiry, last success time, `last_success_response_headers_latency_ms` (latest successful attempt latency from request start to upstream response headers; a single sample, not a percentile and not TTFT), and in-flight stream/non-stream counts. An absent value is never one thing: the surface separates a failed read, a row observed only in part, a row outside the observation cohort because it does not participate in routing, a cohort cut short by paging, a target whose routing schedule has not been evaluated, and a target the process has genuinely never observed. The routing-window state is reported alongside these and is orthogonal to them: a target can be `available` by every ban measure and still be outside its window, so the two are never collapsed into one verdict. In-flight counters only advance while the matching limit is configured, so an unmetered gauge reads as absent rather than as a measured zero. These are Ban Policy observations, not probe health, success-rate or availability proof.
- Cooldown reset (`POST /api/loadbalance/current-state/{connection_id}/reset`) clears only retry/ban cooldown fields (cycle/cumulative attempts, next-retry time, last retry delay, ban mode/expiry, last failure kind) and preserves QPS window, in-flight counts, last success facts, response-headers latency and round-robin cursors; the response returns the full post-reset state DTO and `cleared=false` when there is nothing to clear.

### 4.7 Web UI (Management Dashboard)
- View all configured models and their reachable Terminal Targets
- Add/edit/delete model configurations with ordered access targets
- Add/edit/delete profile-scoped endpoints
- Model detail renders one mixed access-target list ordered by the shared `position`; Model and Terminal rows share a continuous "位置 N" numbering, adjacent rows of either type can be moved up/down with the same controls, and reloads never restore type grouping
- Add/edit/delete Terminal Targets from model detail; the Terminal Target dialog includes an “高级请求设置” group with request limits, custom headers, and the custom request parameters JSON editor
- Toggle enabled/disabled access targets per model
- Select an explicit load-balance strategy with Ban Policy settings per model
- Dedicated model-detail route (`/models/:id`) for ordered access-target and Terminal Target configuration; current loadbalance state and loadbalance event history live under Ban Policies
- Dedicated request-log browsing and investigation at `/observe/requests`, separate from dashboard analytics
- Dedicated routes for pricing templates and proxy API key lifecycle management
- `/observe` provides three tabs sharing one URL time preset: Overview (Now strip with rolling 30-minute RPM/TPM, Window KPI grid with TTFT P50/P95 headline, request preview and main chart), Analytics (single main chart with metric/group switching and a semantic data table), and Events (loadbalance timeline). Observe fragments load independently and never synthesize zeros on failure. The Window KPI grid shows six cards — requests, HTTP success rate, TTFT P95, output rate, cache-read share, and known cost. The cache-read share card separates genuine zero, no comparable rows, an empty window, a failed read, and partial coverage into distinct states (the share renders behind a clipped badge under partial coverage); it never paints a fabricated `0%` over missing data.
- Model creation supports one-step composite create: an optional `initial_terminal_target` (existing or inline endpoint) is created atomically with the model; the model is enabled by default when the target is present, and cross-mode capability values are rejected with `422 target_openai_mode_mismatch` with zero writes. Terminal Targets can be batch-copied to same-profile same-family same-mode destination models in one transaction (new targets default to not participating in routing).
- Model detail exposes read-only static routing diagnostics (`GET /api/models/{id}/routing-diagnostics`) and the models list embeds a compact `routing_summary`; both share one backend analyzer, which is separate from the runtime planner and never reads Ban/retry/admission state. The analyzer applies the routing strategy to the single mixed peer sequence exactly as the runtime does, so a `single` strategy truncates that one list rather than each target type.
- Dashboard analytics lives under `/observe?tab=analytics` and replaces the old standalone statistics route
- The protected shell renders sidebar navigation and breadcrumbs from local route metadata.
- Settings uses canonical public URLs with **全局** (`scope=global`) and **实例** (`scope=instance`) scopes and a section allowlist; the legacy `tab` query value is dropped during canonicalization. `scope=global` contains billing/reporting currency, timezone, audit/privacy, and config rules; `scope=instance` contains authentication and operator account, automatic retention policy with owner actual coverage, manual cleanup, and the retention job center.

### 4.8 Configuration Persistence
- Runtime and management configuration is stored in PostgreSQL with Go-backend-managed schema migrations applied at startup
- Startup/bootstrap process settings are owned by the plaintext `config.json` bootstrap file; external edits require a Prism restart after R2
- The default profile exists from the first startup and remains editable after initialization
- Database setup is managed by the Go backend runtime and applies the checked-in fresh-install baseline on startup
### 4.9 Request Statistics & Analytics
- Local `GET /v1/models` produces no runtime telemetry. Provider-forwarded operations create retained history only when activity reaches a telemetry handoff: successful `2xx` responses use the durable response-path handoff, captured non-`2xx` responses use scheduled activity handoff, and the narrow `PlanningFailure`/`admission_exhausted` classes use synthetic failure handoff. Registry rejections and the earlier planning errors listed above do not create request history.
- Each retained request-log row captures: profile ID attribution, requested `model_id`, final `resolved_target_model_id` when an access-target path is selected, `api_family`, Terminal Target used through connection attribution (ID, endpoint base URL, description), Prism `ingress_request_id` (UUIDv4), `attempt_number`/`attempt_trigger`/`attempt_result`/`is_winner`, best-effort `provider_correlation_id`, caller and upstream client display, scoped HTTP status (`upstream_status_code`/`gateway_status_code`/`legacy_status_code`), `attempt_duration_ms`/`legacy_duration_ms`, token usage when available from upstream response, whether the request was streamed, selected Terminal Target, operation names, translation mode, timestamp, and a unified failure projection (`error_source`/`error_code`/`failure_stage`/scrubbed `error_detail` plus independent `stream_error_kind`/`stream_error_detail` with redacted/truncated flags). Old un-scoped `status_code` and mixed `response_time_ms` are only retained as nullable legacy projections and are never written by the current runtime writer.
- A normal upstream attempt is represented by one request-log row; telemetry-eligible planning/admission activity adds a diagnostic row with `row_kind=planning|admission` and a gateway status. `ingress_request_id` groups retained rows from one incoming runtime request; the default Requests view is the server-side retained ingress chain (`view=ingress_chains`) with finalized usage evidence, expected/retained counts, routing evidence flags, and signed outer/row cursors. The current detail sheet shows the request path and routing fields, while operation names, translation mode, and upstream path are persisted and returned by the detail API but are not rendered in that sheet.
- A normal upstream attempt is represented by one request-log row; telemetry-eligible `PlanningFailure` or `admission_exhausted` activity may add a synthetic row without endpoint/connection. `ingress_request_id` groups retained attempt rows from one incoming runtime request. The current detail sheet shows the request path and routing fields, while operation names, translation mode, and upstream path are persisted and returned by the detail API but are not rendered in that sheet.

#### 4.9.1 Token Usage Extraction
Token usage is extracted from upstream responses using api-family-aware parsing:
- **OpenAI Chat Completions (non-streaming)**: Extracts from `usage.prompt_tokens`, `usage.completion_tokens`, and detail objects
- **OpenAI Responses and Responses compact (non-streaming)**: Extracts from `usage.input_tokens`, `usage.output_tokens`, `usage.total_tokens`, and detail objects
- **OpenAI Responses input_tokens (non-streaming)**: Extracts `input_tokens` and `total_tokens` from top-level token-count payloads
- **Anthropic Messages (non-streaming)**: Extracts from `usage` object
- **Anthropic count_tokens (non-streaming)**: Extracts `input_tokens` from top-level
- **OpenAI (streaming)**: Accumulated from terminal SSE usage events for native Chat Completions or Responses streams
- **Anthropic (streaming)**: Accumulated from SSE events (`message_start` and `message_delta`)
- **Fallback**: If token data cannot be extracted, all token fields are logged as `null`
- **Null vs zero token semantics**:
  - No upstream usage block: token fields remain `null`
  - Usage block present but special fields absent: special fields logged as `0`

#### 4.9.2 Token Costing
The gateway computes the cost of each request based on the extracted token usage and the connection's assigned pricing template.
- **Pricing Templates**: Pricing is profile-scoped and reusable. Connections reference templates via `pricing_template_id` instead of storing inline price fields.
- **Pricing behavior**: Pricing templates use five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Explicit JSON `null` means an unconfigured specialty price; empty or whitespace-only strings are rejected with `422`. An optional single `tier` card uses the same five components and switches the whole request when disjoint total input strictly exceeds its threshold.
- **Semantic Note**: Explicit `"0"` means configured free pricing. `MISSING_PRICE_DATA` is reserved for absent, unusable, or invalid pricing snapshots, or missing FX data. Token costing uses canonical disjoint components: base input, cache-read input, cache-creation input, base output, and reasoning output; aggregate `cached_tokens` is derived-only for presentation.
- **Cache-read share**: The Window KPI card and the per-request detail row report prompt tokens served from cache as `cache_read / (input + cache_read + cache_creation)`, computed once in the frontend over the disjoint components without re-deriving totals. Upstream ingestion already normalizes to disjoint semantics, so `input_tokens` never includes cache components; `cache_creation` is structurally absent for OpenAI/Gemini and coalesces to zero, while a null `input` or `cache_read` excludes the row (the two are not interchangeable). The window aggregate is computed backend-side in one SQL statement under a single `cache_basis_eligible` predicate that also requires a non-null `operation_name` outside `anthropic.count_tokens`, `gemini.count_tokens`, `openai.images.generations`, and `openai.images.edits` — count_tokens duplicates the total into cache_read and image operations never report cache components, and a null operation name is indeterminate. Real zero, no comparable rows, an empty window, a failed read, and partial coverage are presented as mutually distinguishable states; none is shown as a fabricated `0%`.
- **Historical costing provenance**: Request-log details retain and display report currency, original/source currency, FX rate and source, pricing unit, pricing configuration version, template identity snapshots, reporting-currency epoch, `cost_segment_key`, and all five pricing snapshot components. Lists and CSV use each row's stored report-currency symbol rather than recomputing historical requests from the current settings. Pricing is a four-state classifier (`priced|unpriced|ineligible|unknown`) with canonical `missing_price_components` and `pricing_evidence_trust`; the old `priced_flag`/`billable_flag` boolean surfaces are not part of the current contract. Non-2xx rows are `ineligible`. Legacy-untrusted rows keep canonical cost null and expose raw values only through the exact detail `legacy_pricing_evidence` block.

- Statistics dashboard in the Web UI with:
  - Overview KPI cards for Active Models, Requests 24h, Spending 30d, and Average RPM
  - Overview supporting tiles for average latency, P95 latency, error rate, and streaming share, plus API-family mix, recent activity, quick actions, and top-spending models
  - Dashboard incidents for active bans and recent loadbalance events; the backend `routing_health_map` is returned in the snapshot but is not rendered by the current UI
  - Aggregate endpoint, model, and proxy-key usage views sourced from the unified usage snapshot with endpoint labels read from stored `endpoint_label_snapshot` values
  - Separate recent activity feed that links into request-log investigation without being embedded in the dashboard snapshot
  - Analytics controls: time presets (`1h`, `6h`, `24h`, `7d`, `30d`, `all`) plus model-line selection for usage trend comparison
  - Analytics KPI cards for requests/success rate, total tokens with component breakdown, RPM, TPM, and total spend, followed by usage trends and aggregate endpoint/model/proxy-key tables
  - Summary statistics grouped by model and API family
- Dedicated request investigation UI at `/observe/requests` with server-backed filters (`pricing_status` four-state, caller-only `client_rule_id`, final-target `resolved_target_model_id`), a default retained ingress-chain view with signed cursors, scoped status columns, adaptive table height, a failure-first detail sheet, and a dedicated full audit page with byte-exact raw body downloads
- REST API for querying statistics remains available for API callers and debugging:
  - List request logs with pagination and filters
  - Get the stats-only dashboard snapshot and separate dashboard recent activity feed
  - Get aggregated statistics (counts, averages, totals) with grouping
  - Get the usage snapshot and endpoint model statistics directly when needed
- Dashboard overview polls REST stats for the aggregate snapshot and separate recent activity feed.
- Dashboard Analytics polls the REST usage snapshot for the selected preset and treats each accepted snapshot as a full replacement; endpoint model statistics load through REST drilldown endpoints.

#### 4.9.3 Retention Coverage And Analysis Windows

The analysis window presets and the retention policies are independent surfaces. Long windows stay available regardless of how little history an instance keeps, and the mismatch is reported by the coverage contract instead of by removing options.

- The window presets (`1h`, `6h`, `24h`, `7d`, `30d`, `all`) are a fixed product surface. They are never narrowed, hidden, or rewritten to match the configured retention.
- Retention length is an instance-level configuration choice, not a product default. All four managed policies (`request_logs`, `audit_logs`, `statistics`, `loadbalance_events`) are `NULL` at the database level, which means no scheduled cleanup at all; setting a policy to N days (minimum 1) is what enables day-based logical cleanup through the partitioned-log maintenance worker.
- Every observability query answers with a coverage projection alongside the data: the requested preset, the effective window, the retention floor that applies, whether the answer is served from raw or rollup sources, a `complete` flag, and the concrete gaps with their reasons. When coverage is not complete, the Observe surfaces raise a retention-coverage warning that links directly to the instance retention settings, and the loadbalance events timeline raises its own callouts for incomplete source coverage and for recorded gaps.
- Deleted history is never backfilled with zeros. Results are trimmed to the published retention floor and labeled as trimmed, so a short series reads as "this is what is retained" rather than as a drop in traffic.
- Pricing coverage (`complete|partial|no_trusted_cost|no_eligible`) is a separate axis reported next to the spend figures it qualifies; it describes cost trust, not data retention, and the two are not merged.
- Consequence, and the expected behavior: on an instance that keeps one day of statistics, the `7d`, `30d`, and `all` windows legitimately show only the retained range. This is not a defect and not a reason to remove the presets; the retention settings page states the same consequence, namely that a retention policy governs future visible coverage and never restores history that has already been cleaned up.

### 4.10 Request Audit Logging
Full HTTP request/response recording for proxied requests, stored in the database for auditing and debugging.

#### 4.10.1 Request-Time Audit Flags
- Audit rows store `audit_enabled_at_request` and `audit_capture_bodies_at_request` as request-time provenance
- Audit behavior does not derive runtime compatibility from catalog metadata
- Toggling audit settings affects new requests only

#### 4.10.2 What Gets Recorded
For each audited upstream attempt that is materialized:
- **Request**: HTTP method, scrubbed upstream URL, canonical sorted scrubbed request-header entries with provenance, and a BYTEA byte-exact stored request-body prefix.
- **Response**: Scoped HTTP status and response-header snapshot with provenance; captured response bytes when enabled. For a multi-attempt request, response body capture is associated with the final attempt rather than every failed attempt.
- **Metadata**: model and API family, connection identity (connection ID, endpoint base URL, description), scoped status/duration, stream flag, timestamp, link to `request_log`, `row_kind`, attempt facts, ingress byte counters and capture-limit reasons.

#### 4.10.3 Sensitive Data Redaction
Audit redaction applies the fixed safe-diagnostic bottom line (Bearer/Basic/JWT/API-key-like/key=value/URL-secret redaction from `safediag`) plus request-time Header Blocklist additions before the ordinary backend outbox. Sensitive header names and credential-shaped values are stored as `[REDACTED]`; legacy rows are marked `legacy_all_values_redacted`/`legacy_rescrubbed`. Captured request or response bodies are the authorized raw-body exception and may contain prompt/PII/secrets; that exception never extends to failure diagnostics, ordinary telemetry, or headers. Body capture is bounded at 4 MiB per body, with a 12 MiB shared request budget and 4 MiB response reservation per ingress, and raw downloads serve exact stored bytes with typed capture/truncation callouts.

Failure diagnostics reuse the same redaction rules without the raw-body exception. The persisted `error_detail` and the independent `stream_error_detail` are produced by scrubbing upstream failure text through the shared `safediag` rules plus the request-time Header Blocklist, then truncating on UTF-8 code-point boundaries; each row records whether the value was redacted and whether it was truncated. Routing failures and admission failures persist the same scrubbed projection, so a diagnostic row never carries unscrubbed upstream text. Provider-supplied error codes are adopted only after they survive the same trim/scrub pass and match the stable code grammar.

#### 4.10.4 Routing And Delivery Boundaries
Audit policy does not change model selection, Terminal Target selection, or client-facing response handling:
- A successful provider-forwarded `2xx` response requires the applicable durable telemetry handoff before Prism commits or first flushes the response. If that handoff fails before client-visible output, Prism returns a runtime observability error instead of the successful response.
- After handoff, background materialization and non-required side-effect failures are isolated from the proxied response path.
- Unsupported or wrong-method runtime registry rejections do not enter telemetry or audit handling.

#### 4.10.5 Audit Inspection (Frontend)
- Audit detail is opened from the request investigation flow on `/observe/requests/:requestId/audit` rather than a standalone `/audit` page
- Every successfully loaded request detail sheet provides an entry point to the dedicated audit page; the sheet itself remains overview-only and does not fetch audit payloads
- The dedicated page first loads the request detail, then shows request-time audit state as disabled, metadata-only, or full capture and resolves audit rows with `request_log_id`

#### 4.10.6 Body Size Limits And Raw Download
- When body capture is enabled, Prism stores captured request bodies for audited attempts and the captured response body for the final attempt only.
- Capture is bounded: per-body 4 MiB cap; per ingress request copies 12 MiB and final response 4 MiB; scrubbed header blocks 64 KiB with 1 MiB per direction (response reserves 64 KiB for the final winner). Allocation follows immutable launch order; budget exhaustion only stops extra audit storage, never proxy traffic.
- Failure diagnostics are bounded independently of body capture and apply whether or not audit is enabled: the raw upstream error sample is capped at 32 KiB per attempt and stays in memory for diagnosis only — it never enters the outbox or any table — while the persisted `error_detail` and `stream_error_detail` are capped at 4 KiB each after scrubbing. These caps are code-fixed constants rather than settings; changing one requires syncing this document, the API metadata, and the runtime tests.
- Each row records `ingress_audit_bytes_observed/stored/truncated`, per-direction header byte counters, and capture/header limit reasons (`none|body_cap|ingress_budget|both|handoff_budget|omitted_ingress_budget|omitted_handoff_budget|block_cap`); truncated rows keep a permanent prefix-only callout, never a fake empty.
- Bodies persist as BYTEA byte-exact stored prefixes; the raw download (`GET /api/audit/logs/{id}/raw-body?direction=request|response`) returns the exact stored prefix as `attachment` / `application/octet-stream` / `nosniff` / `private, no-store` with a `.txt`/`.bin` filename by UTF-8 validity. Binary bodies show byte metadata only (no text preview), and valid-UTF-8 bodies provide message/structured/raw text views plus copy.
- Audit list/detail/preview responses are `private, no-store`; the audit list also carries a non-null `known|legacy_unknown` coverage projection, and an anchored row outside the first page arrives exactly once as `anchor_item`.

### 4.11 Batch Data Deletion
Provide flexible bulk deletion of historical logs and statistics data to manage database growth.
- Supported Data Types: `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`
- The Settings UI offers only `1`, `7`, `30`, `90` days, or all data. The management API accepts an explicit cutoff timestamp for callers that need a custom range.
- Deletion requests create durable management jobs; the Settings retention job center lists and polls the global v2 queue, opens durable checkpoint details, and cancels only queued manual jobs. The same list/get/cancel routes remain available as API contracts.
- Deleting `request_logs` does NOT delete linked `audit_logs`; audit rows retain weak request-log metadata, and audit reads expose `request_log_missing=true` only when both `request_log_id` and `request_log_created_at` are present but their profile-scoped tuple no longer resolves

### 4.12 Custom HTTP Headers per Terminal Target
Allow users to configure custom HTTP headers on individual Terminal Targets. These headers are appended to upstream proxy requests.
- Custom headers are configured during Terminal Target creation or editing
- Headers are stored as a JSON object
- Custom headers can override ordinary forwarded headers, but they cannot override Prism-controlled authentication or provider-version headers and cannot bypass the final Header Blocklist

### 4.13 Custom Request Parameters per Terminal Target
Allow operators to attach an optional static top-level JSON object (`custom_request_parameters`) to each Terminal Target. Prism applies the object as a top-level shallow overlay on the provider-native upstream request body of every actual attempt that selects that Connection, after the provider adapter completes model/path rewrite.
- Configured during Terminal Target creation or editing under “高级请求设置 → 自定义请求参数（JSON）”, with a full-width JSON editor, format/clear actions, a top-level count summary, and field-level validation that mirrors the backend validator
- Overlay rules: non-conflicting client top-level fields are preserved; matching top-level keys are replaced wholesale (nested objects are never recursively merged); configured `null` values are sent as literal JSON null; there is no delete-member syntax
- The same Connection configuration applies to all nine provider-forwarded POST operations (OpenAI Chat Completions/Responses/input-tokens/compact, Anthropic Messages/count_tokens, Gemini generateContent/streamGenerateContent/countTokens); local `GET /v1/models` never applies it
- `model`, `models`, `stream`, `messages`, `input`, `contents`, `instructions`, `system`, and `systemInstruction` are protected top-level fields and are rejected at save time; the management API returns 422 with a locatable `field`/`path`/`reason`/`limit` envelope
- Constraints: root must be an object, compact encoding ≤ 64 KiB, nesting depth ≤ 16, total object members ≤ 256, integers within the ECMAScript safe-integer range, no non-finite numbers, no duplicate or blank keys
- Unconfigured, explicit `null`, and `{}` all mean “no parameters” and keep the existing request bytes, fast paths, headers, auth, pricing, audit, and response behavior unchanged
- When any planned candidate carries a configuration, Prism buffers and validates the ingress body as a JSON object, materializes a per-attempt merged body (also for Gemini path-streaming, which switches from the request-body streaming fast path to the buffered path), and re-extracts generation-parameter telemetry from each attempt's final body
- Body-dependent headers (`Content-Encoding`, `Content-MD5`, `Digest`, `Content-Digest`) are stripped from client, auth-extra, and `custom_headers` sources whenever an overlay re-encodes the body; `Content-Length` is recomputed
- Error boundaries: non-object ingress fails with 400 before provider transport; Gemini path-bound operations with a configured candidate reject non-identity `Content-Encoding` with 415; merged bodies over the 20 MiB runtime limit fail with 413; planning-snapshot compilation fails closed on invalid persisted data (cold start fails, hot refresh keeps the last-good snapshot)
- This is plaintext, non-secret configuration: the management API echoes it, and enabling request-body audit capture stores the final merged upstream body (which includes the injected parameters); validation errors and logs never echo the configured values
- Prism does not guess vendors, download provider catalogs, or verify provider slugs; whether an upstream accepts or honors a parameter is the operator's and upstream's responsibility (for example OpenRouter `provider.only` / `provider.order` / `allow_fallbacks`)

### 4.14 Supported API Families
- The application exclusively supports the shipped OpenAI, Anthropic, and Gemini `api_family` values

### 4.15 Configurable Header Blocklist
Database-backed header blocklist with CRUD API. Supports exact and prefix match types. System defaults for Cloudflare tunnel metadata, tracing headers, and standard proxy headers. Applied by the Go runtime on every request.

### 4.16 Frozen Profile Scope
- Prism preserves the `profiles` table and all `profile_id` storage columns for historical attribution and a future unfreeze path.
- Profile-scoped management APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.
- Global management routes stay outside profile scoping. Global routes include auth, auth-setting flows, `GET/PUT /api/settings/log-retention`, destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Profile lifecycle APIs are not exposed in the current management surface.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and always resolves against frozen Default profile id `1`; `X-Profile-Id` and profile fields remain compatibility/storage attribution only.
- Observability rows (`request_logs`, `audit_logs`) carry immutable `profile_id` attribution for historical correctness.


### 4.17 Routing Schedule per Terminal Target

- A Terminal Target may carry a routing schedule: a set of recurring weekly windows during which it is allowed to be selected. Outside every window it is not a routing candidate at all.
- Having no schedule means no restriction. Existing Terminal Targets migrated with none, so their routing behaviour is unchanged down to the byte.
- Each schedule carries its own IANA timezone. This is the target's routing clock and is unrelated to the reporting timezone preference in Settings, which only affects how timestamps are displayed and never changes which upstream serves traffic.
- A window names the weekdays it opens on, a start time, and an end time. An end time earlier in the day than the start means the window continues into the next day; the weekday selection still refers to the day it opens.
- Windows are half-open: a window ending at 18:00 does not include 18:00. Adjacent windows therefore join without overlapping and without a gap.
- A target may carry up to 32 windows, and their union is what counts. A configuration whose windows together cover the whole week is refused: "always available" is expressed by having no schedule, not by describing one that never closes.
- Eligibility is decided once per incoming request, at the instant planning starts, and that decision governs the whole request including every retry and failover attempt. A request already in flight is never interrupted because a window closed.
- The window check runs after ban filtering, so a target skipped for its schedule is one that was otherwise usable, and the reopen time shown to an operator is not contradicted by a ban the operator cannot see.
- When every evaluated target was excluded solely by its schedule, the request fails with a dedicated code and the earliest reopen instant. When the exclusions were mixed with other causes, the ordinary failure is returned with a sentence recording how many targets were out of window, so the mixed case stays searchable without the dedicated code overstating what happened.
- A target whose timezone cannot be resolved is excluded rather than allowed through. Routing is the act of releasing traffic, so a broken configuration must not release it at an unintended hour. The failure is confined to that one target and is reported as its own condition, distinct from being outside a window.
- Daylight-saving transitions are handled by comparing local wall-clock time. A window loses an hour on a spring-forward day and gains one on a fall-back day, which matches what "every day from 09:00 to 18:00 local time" means to an operator.
- Static routing diagnostics report the configured windows and whether they cover the week, never whether a window is open at this moment: diagnostics are a pure function of configuration so that one analysis generation always yields one answer. The live open/closed state is delivered separately, computed by the server, and never recomputed by the browser.
- The model detail target list shows each target's current schedule state, and the state carries the boundary at which it stops being true so a page left open downgrades itself instead of asserting a stale verdict.

## 5. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Deployment | Root Compose self-hosted bundle uses one Prism app image plus PostgreSQL; the app image runs the Go backend behind Nginx, and the local launcher runs PostgreSQL, backend, and optional Vite frontend. The runtime image must provide `/usr/share/zoneinfo`: the backend builds with `CGO_ENABLED=0`, so `time.LoadLocation` reads that directory only and has no libc fallback, and a missing database would push every connection with a routing schedule into `terminal_target_schedule_unresolvable`. The current base image ships tzdata, which makes this an implicit dependency that must be re-verified whenever the base image changes |
| Authentication | Optional operator auth for `/api/*`; optional proxy API keys for `/v1/*` and `/v1beta/*` |
| Latency overhead | < 50ms added to proxy requests |
| Concurrent requests | Support 10+ simultaneous proxy requests |
| Database | PostgreSQL (Go-managed startup migrations) |
| API standard | Markdown API contract maintained in `docs/architecture.md` (section 14, API Reference) |
| CORS | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL |

## 6. Tech Stack

| Component | Technology |
|---|---|
| Backend | Go 1.26.6, chi, pgx |
| HTTP Client | Go `net/http` streaming transport |
| Database | PostgreSQL via pgx |
| Frontend | React 19, Vite 8, TypeScript, Tailwind CSS 4, shadcn/ui, TanStack Router |
| API Contract | Markdown reference in `docs/architecture.md` (section 14, API Reference) |
| Communication | REST API with JSON and SSE for streaming proxy responses |

## 7. Out of Scope (v1)

- Auth-based multi-tenancy, multi-operator RBAC, and per-user data isolation beyond the single-operator auth surface and profile namespace isolation
- Usage-based billing/accounting integrations beyond Prism's built-in telemetry and costing reports
- Global runtime rate limiting outside per-Terminal Target `qps_limit` and in-flight limits
- Routing-schedule extensions beyond recurring weekly windows: calendar date ranges, holiday calendars, and cron expressions; a separate enable switch that keeps the windows while suspending them (clearing the schedule deletes the windows); an audit trail for schedule changes
- External secret-manager integrations beyond Prism's built-in endpoint-secret encryption at rest
- Proxy API key scoping. A key is an instance-wide credential: it can call every enabled model and every registered operation, and models enabled later are automatically in scope. v1 deliberately ships this single scope instead of per-model, per-Endpoint, per-API-family, per-operation, rate, token, or spend limits on an individual key, and the Proxy API Keys page states the limitation in the UI rather than leaving it implicit. Introducing scopes later would touch the key data model and its migration, the runtime authorization path, the management API and its UI, and would require deciding the default scope for keys that already exist.

## 8. Requests Page Specification

**Scope:** Frontend route `/observe/requests` and its request-investigation helper cluster

### 1. Overview

The Requests page is Prism's dedicated request-browser and investigation surface for proxied traffic. It is mounted at `/observe/requests`. It provides a Default-profile-pinned view for browsing request history through server-backed filters, a retained-ingress chain view as the default investigation unit, and an overview detail sheet with a unified failure projection. Full audit payloads live on a dedicated audit page.

The backend request-log and audit APIs remain the source of truth. The frontend route is responsible for presenting that data in an operator-friendly investigation workflow without changing runtime proxy semantics. The canonical URL filter set keeps `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `pricing_status`, `unpriced_reason`, `time_range`, `view`, `sort_by`, `sort_order`, and `chain_cursor`, while exact single-request investigation uses `request_id`.

The request-log route now uses split HTTP contracts: a slim v2 row payload for attempt browsing, a chain envelope for the default ingress-chain view, and a v2 detail payload for the sheet. The `priced` boolean alias is not accepted anywhere; pricing filtering uses the four-state `pricing_status` (`priced|unpriced|ineligible|unknown`) and the strict backend rejects unknown query keys with `422 unknown_query_key`.

### 2. Goals

- Provide a dedicated request-history route at `/observe/requests`.
- Support deep investigation of a single request through URL-addressable state.
- Keep the retained browse filters server-backed and URL-addressable.
- Default to the server-side retained ingress chain as the investigation unit.
- Expose linked audit payloads only when needed.
- Support implemented drill-down entry points from dashboard overview and dashboard recent activity.
- Show requested model identity separately from the final target model chosen by unified access-target resolution.
- Show requested model identity separately from final target, selected Terminal Target, and endpoint.

### 3. Non-Goals

- Replace dashboard or statistics summaries.
- Re-derive retained chains client-side from the current page; the chain grouping is server-owned and paged by ingress with signed cursors.
- Change frozen Default-profile runtime routing behavior for `/v1/*` and `/v1beta/*`.

### 4. Route Responsibilities

The page route should act as a thin orchestration shell with four primary responsibilities:

1. Render page chrome through `PageHeader`.
2. Own URL-backed state through `useRequestLogPageState()`.
3. Load request data and filter options through `useRequestLogsPageData()`.
4. Compose the investigation UI through `RequestFocusBanner`, `FiltersBar`, `RequestLogsTable`, and `RequestLogDetailSheet`.

The route should also integrate shared application services:

- Default profile id `1` is frozen for management reads; `X-Profile-Id` may still be sent by shared API code but is ignored.
- `useTimezone()` plus the shared frontend locale boundary for locale-aware timestamp formatting.
- `useLocale()` for route-shell, filter, empty-state, and detail-sheet copy.
- `TooltipProvider` for table and filter affordances.

### 5. URL State Contract

`useRequestLogPageState()` should own the complete search-parameter contract and update query state with `replace: true` semantics so frequent filter changes do not spam browser history.

Supported canonical query parameters:

- Browse filters: `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `pricing_status`, `unpriced_reason`, `time_range`
- View: `view` (`ingress_chains` default | `attempts`), `chain_cursor` for chain outer-page continuation
- Sorting: `sort_by`, `sort_order` (chain view restricts `sort_by` to `created_at`)
- Pagination: `limit`, `cursor` (attempt view)
- Exact-investigation flow: `request_id`
- Row selection without exact mode: `selected_request_id`

Accepted legacy aliases are parsed and canonicalized away: `model_id`, `endpoint_id`, `status_family`, and `offset`. `status=client_error` maps to backend `status_family=4xx`; `status=error` maps to backend `status_family=5xx`. The old `priced=true|false` parameter is not parsed and is rejected by the backend as an unknown key.

Behavioral requirements:

- Default values should be omitted from the URL.
- Any filter mutation that changes the result set must reset pagination (attempt `cursor`/chain cursor) to the first page.
- `request_id` must switch the page into exact-request investigation mode.
- `ingress_request_id` must support grouped investigation of all per-attempt rows created by one incoming runtime request.
- Stale `detail_tab` parameters must be ignored and canonicalized away.
- Chain continuation must never split an ingress identity across pages; the server returns a signed opaque chain cursor.

### 6. Data And API Requirements

#### 6.1 Request Log Fetch

Primary APIs:

- `api.stats.requests()` -> `/api/stats/requests` (attempt view)
- `api.stats.chains()` -> `/api/stats/requests?view=ingress_chains` (default)
- dedicated detail fetch -> `/api/stats/requests/{request_id}` (v2 detail with scoped statuses and failure projection)
- full filtered export -> `/api/stats/requests/export` (server-side CSV)

Required behavior:

- Debounce fetches by 300 ms.
- Send server-supported browse filters for model, ingress request grouping, endpoint, caller client rule, final target model, status family, exact status code, error text, pricing status, unpriced reason, and time window.
- Translate canonical URL state to backend request parameters: `model` -> `model_id`, `endpoint` -> `endpoint_id`, `status` -> `status_family`, and `cursor` -> `offset` (attempt view only).
- Send `unpriced_reason` only when `pricing_status=unpriced`; other pricing states omit it from backend params.
- Send `ingress_request_id` as an exact server-backed grouping filter when present.
- Keep list browsing on the slim v2 row schema and fetch exact-request sheet data from the dedicated v2 detail endpoint.
- Track fetch ordering so stale responses cannot overwrite newer state.
- CSV export downloads the server-produced full filtered file; the frontend never assembles CSV from the current page.

#### 6.2 Filter Option Bootstrap

The page derives model, endpoint, caller client, and final-target filter options from the paginated `/api/stats/requests` response: `filter_options.models`, `filter_options.endpoints`, `filter_options.clients`, and `filter_options.resolved_target_models`.

Response-owned filter options should become ready when the current list response arrives. `filter_options.clients` entries use `{ client_rule_id, client_label }` and represent enabled User-Agent Client Rules. Selecting one sends `client_rule_id` back to the backend, where matching is caller-only against `caller_user_agent`.

#### 6.3 Dedicated Audit Resolution

Detailed audit payloads load only on `/observe/requests/:requestId/audit`. The audit route is request-focused: it first loads `/api/stats/requests/{request_id}`. If that request detail is missing or invalid, the page stops and does not issue audit-list or audit-detail calls. The current UI therefore has no standalone orphan-audit browser even though backend audit rows can retain `request_log_missing` metadata after request-log deletion.

Audit APIs:

- request detail: `api.stats.requestDetail()` -> `/api/stats/requests/{request_id}`
- request-scoped audit list: `api.audit.listForRequestLog()` -> `/api/audit/logs?request_log_id=...`
- selected audit detail: `api.audit.get()` -> `/api/audit/logs/{id}`
- raw body downloads: `GET /api/audit/logs/{log_id}/body/request` and `/body/response` (byte-exact BYTEA prefix, attachment, no-store)

Required behavior:

- Avoid audit fetches during normal table browsing and the overview sheet.
- Skip the audit-list and audit-detail calls when request-time `audit_enabled_at_request` is `false`.
- Treat `audit_capture_bodies_at_request` as the request-time provenance flag: enabled plus false means metadata-only; enabled plus true means full capture. Do not infer capture mode from whether a body happens to be present.
- Derive `from` and `to` as a UTC window of 12 hours before through 12 hours after the request's `created_at`.
- Request at most 20 audit rows per page. `audit_id` selects a row from the current page; when it is absent, the first returned row is selected. An unknown `audit_id` shows a missing-audit state without fetching a detail row.
- Preserve `cursor` in the audit-page URL. Next uses `next_cursor`; Previous clears `cursor` and returns to the first page.
- Keep audit loading isolated from the request-list and sheet detail-fetch lifecycle.

### 7. UX Workflow Requirements

#### 7.1 Filter And Triage Workflow

The page should use only the retained browse filters in URL state and send them directly to the backend list route. The current canonical URL contract keeps `request_id`, `selected_request_id`, `ingress_request_id`, `model`, `endpoint`, `client_rule_id`, `resolved_target_model_id`, `status`, `status_code`, `error_text`, `pricing_status`, `unpriced_reason`, `time_range`, `view`, `sort_by`, `sort_order`, and `chain_cursor`, and removes the old client-side search, token, latency, stream, outcome, and triage refinement layer. The Client dropdown must not expose regex, `client_scope`, or upstream matching language.

Triage chips are canonical-query shortcuts only:

- **全部**: clears `ingress_final_result`/`confirmed_failover`/`pricing_status` shortcut-owned conditions.
- **最终失败**: writes `ingress_final_result=failed` (finalized ingress cohort).
- **确认故障转移**: writes `confirmed_failover=true`, matched against persisted `attempt_trigger=failover` evidence only.
- **未定价**: writes `pricing_status=unpriced`; `ineligible` and `unknown` never match this chip.

Chip changes must not delete unrelated More Filters conditions. Contradictory conditions return typed `422`; a legal filter with no rows returns a true empty state.

#### 7.2 Exact-Request Investigation Workflow

When the route opens with `request_id`, it should stop behaving like a normal paginated browser.

Required behavior:

- Fetch only the targeted request.
- Show `RequestFocusBanner` with an exit action.
- Render a dedicated empty state with a return action when the request is missing.
- Ignore stale `detail_tab` parameters and keep exact-request investigation on the overview-only sheet.

Grouped request-tracking workflow:

- `request_id` remains a one-row deep link for exact attempt investigation.
- `ingress_request_id` groups multiple attempt rows from one incoming runtime request without changing `request_id` semantics.
- Grouped rows show all attempts for one incoming runtime request together.
- The overview sheet should surface `ingress_request_id`, `attempt_number`, `attempt_trigger`, `provider_correlation_id`, requested model, final target model, selected Terminal Target, and endpoint so operators can distinguish Prism grouping from upstream correlation and final response ownership.

#### 7.3 Table Workflow

`RequestLogsTable` should support dense browsing at high row counts.

Required behavior:

- Virtualized rows with `45px` row height.
- `10` rows of overscan.
- Adaptive table height: the body fills the remaining shell viewport and is the single scroll container (no fixed component-owned pixel height).
- Sticky headers in all views.
- Default column set (nine core columns plus pricing state): time, `pricing_state`, status, latency, TTFT, token rate, requested model, final target model, endpoint; additional columns (client, reasoning effort, API family, tokens, spend, stream) remain selectable/orderable.
- Scoped row statuses: upstream rows show `upstream_status_code`, planning/admission rows show `gateway_status_code`, legacy rows show `legacy_status_code`. No COALESCE across scopes.
- Page-size controls limited to `100`, `300`, and `500`, with `100` as the route default.
- Footer controls for page size plus previous and next pagination (attempt view); chain view paginates by signed chain cursor.
- Show `api_family`, requested model, final target model, endpoint, and caller/upstream client display fields without adding browser-side post-filtering.
- CSV export is server-side over the full filtered result set; the frontend downloads the produced file and never assembles CSV from the current page. `GET /api/stats/requests/export` uses RFC 4180 quoting with formula-neutralizing cells, `X-Prism-Export-Row-Count` + `Digest` + exact `Content-Length`, bounded by 100k rows / 128 MiB / 31 days, and `attachment`/`private, no-store`.
- The successful list response carries a non-null `coverage` projection (`known|legacy_unknown`) from the owning actual-coverage snapshot; Requests attempts/chains and CSV export resolve `time_range=1h|6h|24h|7d|30d|all|custom` on the server. When the requested window reaches before the retention floor or actual retained intersection, the page shows an explicit gap instead of pretending the result is complete, and `legacy_unknown` never enters true-empty. A page-size count is not coverage precision.

#### 7.4 Detail Sheet Workflow

`RequestLogDetailSheet` exposes an overview-only inspection sheet with request metadata, requested model vs final target model identity, a unified failure projection, token and cost breakdowns, and routing context.

Every successfully loaded request detail provides a link to the dedicated full audit page. The sheet does not conditionally hide that entry and does not fetch audit payloads. The target page then renders one of three request-time states: disabled, metadata-only, or full capture.

Failed rows use a dedicated failure summary: category/source/stage/code/detail with redacted/truncated/evidence-state flags and lifecycle facts (`upstream_request_started`, `response_headers_received`, `first_body_or_stream_event_seen`), plus stream fields when the failure came from a stream. Success rows do not render an empty six-cell grid.

Dense overview requirements:

- Keep the same logical groups: `Request details`, `Routing context`, `Token usage`, and `Cost breakdown`.
- Render a compact summary strip for latency, token, cost, and timestamp context above the grouped sections.
- Pricing is layered: the pricing projection shows status/reason/resolution/components/trust, canonical cost and currency, template identity snapshots, and `cost_segment_key`; the old flat `priced`/`billable` flags are not part of the contract. Legacy-untrusted rows show `legacy_pricing_evidence` in a separate disclosure and never promote raw values into canonical cost.
- Operation name, upstream operation name, translation mode, and upstream path are returned by the backend detail API, but the current frontend detail type/sheet displays the request path and does not render those operation/translation fields.
- Keep audit payload loading out of the sheet and scoped to the dedicated full audit page.

#### 7.5 Payload Views And Copy

The dedicated audit page renders request and response headers plus request and response bodies. For each non-empty payload block:

- `Rendered` shows the structured document view when Prism recognizes the payload. Header rendering additionally masks `authorization`, `proxy-authorization`, `cookie`, `set-cookie`, and header names containing `api-key`, `token`, `secret`, or `credential` (case-insensitive).
- `Raw JSON` pretty-prints stored body payloads. For header blocks, it shows a browser-normalized header representation with the same additional masking rather than the unmodified stored text.
- Streaming responses offer three distinct views — 消息 (operation-aware message reassembly with tool cards and terminal state), JSON 事件 (per-event virtualized parsed JSON), and 原始 SSE (byte-exact stored text) — with SSE framing that handles LF/CRLF/CR-only, BOM, multi-line `data:`, `event:` names, comments, `[DONE]`, bad-JSON event isolation, and incomplete tails (a truncated capture keeps the tail unflushed with a visible notice). Non-streaming JSON bodies offer 消息 (operation-aware sectioned document including tool-call/tool-result sections) and JSON views. Only valid-UTF-8 stored prefixes are eligible for these views; binary/invalid-UTF-8 bodies show an unparseable state with byte metadata.
- Copying in raw mode copies the transformed text currently shown. Copying in rendered mode copies the underlying stored text, not the browser-masked header display; the three request auth-header values redacted by the backend at write time remain redacted because the persisted values are `[REDACTED]`.
- Empty bodies disable the copy control. Clipboard API failure or absence falls back to a temporary local textarea mounted under the page or sheet's `[data-clipboard-fallback-root]`.
- Body downloads return the byte-exact stored BYTEA prefix with attachment/octet-stream/nosniff/no-store headers and a safe filename; invalid UTF-8/binary bodies use `.bin` and show an unparseable state while remaining downloadable.

### 8. Module Boundaries

- `queryParams.ts` owns the canonical URL filter/view/sort contract, the `pricing_status` enum, and the triage cohort filters (`ingress_final_result`, `confirmed_failover`).
- `useRequestLogsPageData()` owns fetch orchestration for both views plus the chain cursor paging.
- `columns.tsx` owns the default column set and the `pricing_state` cell; scoped status/duration helpers live here too.
- `requestLogColumnPreferences.ts` owns versioned column-visibility preferences (localStorage).
- `requestLogSavedViews.ts` owns versioned saved canonical queries (localStorage); `FiltersBar.tsx` renders the triage chips and the saved-views dropdown.
- `requestLogsCsv.ts` only downloads the server-produced export file with the current filters.
- `detail/RequestLogOverviewTab.tsx` renders the v2 detail including the failure projection and pricing layers.
- `useRequestLogChain.ts` loads the retained ingress chain for the detail sheet; `RequestLogDetailSheet.tsx` renders the chain section (attempt order, triggers, winner/current markers, completeness).
- `useDedicatedRequestLogAudit.ts` owns the audit-page state machine (detail-first, disabled/metadata-only/full).
- `detail/sseFraming.ts` owns SSE framing; `detail/streamTranscript.ts` owns operation-aware stream accumulation with tool calls; `detail/payloadDocumentViewModel.ts` owns the content-aware view model; `detail/RequestLogPayloadBlock.tsx` renders the three-view toggle and tool cards.

### 9. Cross-Route Integrations

- Dashboard overview and recent activity deep-link into `/observe/requests` with `request_id` or `ingress_request_id`.
- The detail sheet links to `/observe/requests/:requestId/audit`.
- Observe Events and Requests can cross-link with absolute ±15-minute windows and verified objects; neither direction claims a unique trigger relationship, and retention cropping is surfaced.
- Pricing Templates / Model detail CTAs come only from the pricing action matrix; usage-only unpriced reasons never show a Pricing CTA.

### 10. Required Contracts

The Requests page must remain compatible with the following backend-facing and shared frontend contracts:

- `RequestLogListItem` (v2 slim row: `request_log_id` string, `row_kind`, scoped statuses, attempt facts, failure preview) for the browse table
- `ChainResponse`/`ChainIngressItem`/`FinalizedSummary` for the default chain view
- `RequestLogDetail` for the detail sheet only
- `api.stats.requests()`/`api.stats.chains()` for browsing and `/api/stats/requests/{request_id}` for exact detail
- `api.stats.exportCsv()` for the server-side full filtered export
- audit API client methods plus raw body download helpers
- dashboard flows that consume request-derived backend responses
- caller-client and final-target observability fields such as `client_rule_id`, `filter_options.clients`, and `resolved_target_model_id`

### 11. Acceptance Criteria

1. Visiting `/observe/requests` loads the ingress-chain view plus filter-reference data for Default profile id `1`.
2. Server-backed filter changes update URL state with `replace: true` semantics and reset pagination to the first page.
3. The retained browse filters update URL state with `replace: true` semantics and drive refreshed list requests directly, without a client-side search or triage refinement layer. `client_rule_id` filters caller user agents only, and `resolved_target_model_id` filters final target models. `pricing_status` is the only pricing filter; `priced` is never generated or accepted.
4. Visiting `/observe/requests?request_id=<id>` opens exact-request investigation mode with the focus banner and detail-sheet support.
5. Visiting `/observe/requests?ingress_request_id=<id>` filters the request list to all per-attempt rows for that incoming runtime request without breaking numeric `request_id` deep links.
6. Opening the dedicated full audit page loads request detail first, then queries `/api/audit/logs` with `request_log_id`, ±12-hour bounds, `limit=20`, and optional `cursor`; disabled audit makes no audit API call.
7. The table remains usable at large result counts through virtualization, sticky headers, adaptive height, and explicit pagination controls; the body is the single scroll container.
8. The list view stays on the slim v2 row payload, the chain view on the chain envelope, and exact-request investigation uses the dedicated v2 detail payload without re-expanding the table schema.
9. Dashboard overview and recent activity can emit deep links into `/observe/requests` without inventing route-local state outside the documented query contract.
10. The overview sheet renders `ingress_request_id`, `attempt_number`, `attempt_trigger`, and `provider_correlation_id` when present so operators can distinguish incoming request grouping from per-attempt row identity.
11. The request-log table and detail sheet render requested model vs final target model separately, falling back to the requested model when `resolved_target_model_id` matches `model_id`.
12. CSV export contains the full filtered result set as produced by the server (`/api/stats/requests/export`), not the currently loaded page.
13. Failed rows open a failure-first summary (category/source/stage/code/detail) instead of an empty metric grid; stream failures carry the stream fields in the same projection.
14. Route-shell, filter, empty-state, and detail-sheet labels follow the active frontend locale while timestamp rendering stays aligned to the selected timezone and locale-aware formatting helpers.
15. TTFT is the primary latency with neutral coloring; total duration stays neutral and is never painted as an error state.
16. The detail sheet renders each piece of primary information exactly once; pricing provenance is layered/collapsible rather than a flat internal-field dump.
17. Audit rows show a top summary when a single row is selected (no empty left rail); header blocks render at readable density with grouping, search, copy, and download.
18. Request-log IDs stay decimal strings end-to-end (URL, JSON, TS); the frontend never converts BIGINT row IDs to JS numbers.
19. The filter bar shows only search (request ID), time range, the triage chips, and a More Filters toggle at 1200px; the remaining filters collapse under More Filters.
20. Operators can save, apply, and delete named views of the canonical query state (versioned localStorage); column visibility is adjustable through the table's Columns menu with a reset-to-defaults action.
21. The detail sheet renders the server-owned retained ingress chain for the selected row: attempt order, triggers, winner/current markers, retained counts, and chain completeness.
22. Streaming audit bodies show three genuinely distinct views (message reassembly, JSON events, raw SSE) with tool-call cards; the same stored text never impersonates two modes.

## 9. Workflows Reference


This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in `frontend/src/app/router/appRouter.tsx`, `frontend/src/app/router/rewriteRoutes.ts`, the live Go backend API surface, and the markdown API reference.

Validated again against current repo surfaces on 2026-08-13:
- `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` are the four version surfaces and are always equal; `release.sh` is what keeps them aligned. The value itself is not restated here, because a copy of it in prose drifts the moment a release moves the files.
- The protected frontend route shell mounts observe, request-log, model, route, settings, proxy-key, and pricing workflows; analytics lives under `/observe`.

### Evidence Sources

- Frontend route surface: `frontend/src/app/router/appRouter.tsx` and `frontend/src/app/router/rewriteRoutes.ts`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/useShellNavigation.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Default-profile scoping: `frontend/src/lib/api/core.ts`, `frontend/src/lib/api/profileScope.ts`
- Backend router assembly: `backend/internal/httpapi/management/`, `backend/internal/httpapi/runtime/`, and `backend/internal/platform/http/server.go`
- Backend API reference: `docs/architecture.md` (section 14, API Reference)
- Request-log details: `docs/product.md` (section 8, Requests Page Specification)

### Runtime URLs

- Frontend: `http://localhost:5173`
- Backend: `http://localhost:8000` for a fresh repo-local bootstrap seed; existing selected bootstrap files can choose another backend port
- Health: `http://localhost:8000/health` for that fresh seed

### Shared Scope Rules

- Public auth routes are `/auth/login`.
- Protected shell routes cover `/observe`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/models`, `/models/:id`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, and `/system/proxy-keys`; analytics is under `/observe?tab=analytics`.
- Profile-scoped management requests are pinned to Default profile id `1`. `X-Profile-Id` is still accepted for compatibility, but the backend ignores its value.
- Global management routes omit `X-Profile-Id` and include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and resolves against frozen Default profile id `1`.

### 1. Sign In And Session Bootstrap

**User entrypoints**

- `/auth/login` (login form, auth-disabled explainer, or fail-closed blocker)

**Frontend flow**

1. `AuthProvider` chooses public bootstrap mode for auth-only routes; the process-local session coordinator (see `frontend/src/context/auth/sessionCoordinator.ts`) owns the phase machine (`BOOTSTRAPPING`, `AUTH_DISABLED`, `AUTH_DISABLED_VERIFYING`, `ANONYMOUS`, `AUTHENTICATED`, `REFRESHING`, `SESSION_EXPIRED`, `LOGGING_OUT`, `AUTH_TRANSITION_FAIL_CLOSED`, `AUTH_UNAVAILABLE`) and the session epoch.
2. Auth bootstrap failures (network/timeout/401/403/429/5xx/invalid/unexpected) stop at a source-labeled retryable blocking state; they are never inferred as disabled, anonymous, or business empty.
3. The login page offers `session`, `7_days` (the backend default), and `30_days` session durations. Wrong credentials mark both username and password `aria-invalid` with a localized generalized error, keep the username, clear and focus the password. Lockout renders the stable code and a same-source countdown from `auth_login_locked` details (`retry_at`/`retry_after_seconds`) with a disabled form until expiry; network/5xx failures stay distinguishable from invalid-credentials/locked with an in-form retry.
4. Successful login returns to the safe local `redirect` path captured from the full protected pathname, query string, and hash; without a valid redirect it falls back to `/observe`. Login success immediately refetches the current page.
5. Concurrent protected-management 401s share exactly one refresh flight (singleflight); each original request replays at most once after a same-subject refresh success. Refresh failure with network/timeout/403/429/5xx/invalid payload enters `AUTH_UNAVAILABLE` (retryable, no logout); refresh 401, `200 + enabled+unauthenticated`, or replay 401 enter `SESSION_EXPIRED` exactly once with one cross-tab signal and a non-dismissible blocker per tab. Only the exact `200 + disabled+unauthenticated` clears old identity data into `AUTH_DISABLED`.
6. Session epoch changes immediately cancel and purge React Query and shared reference data at the boundary; late 200s, polling, or mutation responses from a stale epoch never refill caches. Cross-tab sync uses the non-secret `prism.authSessionGeneration` (rotated only on new identity or auth-mode change): an expiry or auth-mode change in one tab propagates to the others without waiting for the 12-minute refresh timer or the ~30-second poll.
7. Explicit logout starts by purging caches and keeping the logout intent across phases: a strict 204 confirms it, logout-aware bootstrap (enabled + anonymous) settles to `ANONYMOUS`, a stable disabled confirms `AUTH_DISABLED`, and any other outcome keeps “退出结果未知” with manual retry only; the old protected shell/cache is never restored.
8. When auth is disabled, the shell always shows that management and supported runtime operations are currently unauthenticated; issued proxy keys are never presented as “访问已保护”. An anomalous management 401 while disabled never refreshes, never redirects to login, and never loops: the coordinator advances/aborts/purges once, singleflies a tagged public status, then issues the unique fixed `GET /api/models` probe through ordinary auth middleware; outcomes settle into `AUTH_DISABLED_VERIFYING` or a generation-bound exhausted incident.
9. Root `/` makes exactly one decision: anonymous + enabled goes straight to `/auth/login` (no canonical shell mount, no business requests, no “会话已过期”); authenticated or disabled redirects exactly once to the route-metadata canonical `/observe`. Auth-disabled `/auth/login` renders the open-access explainer with “进入控制台 / 前往身份验证设置” instead of silently skipping.
10. Persisted fail-closed auth transitions (`enabling_fail_closed` / `rollback_required`) map to the global `AUTH_TRANSITION_FAIL_CLOSED` blocker for ordinary management, refresh, and login with the registered typed 503; the coordinator polls transition status with a bounded budget (≤12 checks or 60s per epoch/generation/operation) and never creates a second mutation from a lost response.

**UI-driven backend touchpoints**

- `GET /api/auth/status` (tagged `PublicAuthStatus` union)
- `GET /api/auth/public-bootstrap`
- `POST /api/auth/login` (typed 401/429 flat envelope)
- `POST /api/auth/logout` (idempotent strict 204)
- `POST /api/auth/refresh`
- `GET /api/auth/session`
- `GET /api/auth/operations/{operation_id}/status`

### 2. Shell Bootstrap

**User entrypoints**

- Any protected route

**Frontend flow**

1. `AuthProvider` confirms the operator session through the process-local coordinator; every blocking phase (`BOOTSTRAPPING`, `REFRESHING`, `SESSION_EXPIRED`, `LOGGING_OUT`, `AUTH_TRANSITION_FAIL_CLOSED`, `AUTH_UNAVAILABLE`, `AUTH_DISABLED_VERIFYING`) renders the global access layer instead of stale authenticated UI.
2. The shell renders sidebar groups, breadcrumbs, language/theme controls, and the version label. The sidebar footer splits auth/account state (with sign-out reachable when auth is enabled), theme control (discoverable without opening the auth/identity menu), and the read-only version label.
3. Default-profile pages send the pinned compatibility `X-Profile-Id: 1` header from the shared API client.
4. Auth-disabled shells show the open-access state in the footer and explainer surfaces; issued proxy keys are never labeled as protecting access.

**UI-driven backend touchpoints**

- `GET /api/auth/status`
- Default-profile-scoped `/api/*` routes with accepted-but-ignored `X-Profile-Id`

### 3. Dashboard And Statistics

**User entrypoints**

- `/observe` (canonical dashboard; the root `/` landing gate redirects here for authenticated/disabled sessions)
- `/observe?tab=analytics`
- `/observe?tab=events` (routing-health events timeline)

**Frontend flow**

1. Dashboard overview bootstrap loads KPI cards, spending summaries, and average RPM/metric snapshots from the stats-only aggregate snapshot; a metric snapshot the backend cannot produce stays `null` — the page renders loading, error-with-retry, or `—` for missing values, never a fabricated `0`/`$0`/`0%`/empty chart.
2. Dashboard overview bootstrap loads recent activity from the separate recent-activity feed.
3. The dashboard also loads current loadbalance incident state for active-ban and recent-event alerts. The backend snapshot's `routing_health_map` is a response field only and is not rendered by the current dashboard.
4. The dashboard polls REST stats endpoints every 30 seconds for aggregate and recent-activity reconciliation, but any retryable data error that succeeds retries immediately on the next manual retry — recovery never waits for the next poll. Session failure, request error, retention gap, real empty, and real zero stay distinguishable across every protected page.
5. The canonical overview hosts the first-run setup wizard: seven rows covering Endpoint, Pricing, routing policy, Model, enabled Terminal Target, and Proxy Key configuration facts plus a persistent “验证接入” action; the main progress counts only the four routing configuration items (`4/4`), settled strictly on the same `route_witness_generation` from existing-owner reads. Fresh, unknown, degraded, and empty-instance states are distinct; a fresh `4/4` auto-collapses exactly once per 4/4 cycle only after focus leaves the card, and the disclosure entry stays.
6. Quick actions send operators into the analytics tab or `/observe/requests` for deeper analysis.
7. The analytics tab stays aggregate-focused and uses its own snapshot presets rather than request-level drill-down.
8. The events tab renders the routing-health global current state and the loadbalance events timeline with typed summaries, coverage, and freshness from Observe's query context — partial/missing retention coverage is shown as a gap, never as a complete window.

**Backend touchpoints**

- `GET /api/stats/dashboard` for the overview aggregate snapshot; its backend-computed `routing_health_map` response field is not rendered by the current dashboard
- `GET /api/stats/dashboard/recent-activity` for bounded request-history-backed dashboard activity
- `GET /api/loadbalance/incidents` for active bans and recent loadbalance incidents
- `GET /api/stats/usage-snapshot` for API callers, debugging, analytics polling, and manual refresh
- `GET /api/stats/endpoints/{endpoint_id}/models` for analytics endpoint drilldown rows
- Setup wizard existing-owner reads: `GET /api/endpoints`, `GET /api/pricing-templates` (cost-readiness aggregate), `GET /api/config/routing-policies`, `GET /api/models` (configuration × application summaries on one witness generation), model detail target read, and `GET /api/settings/auth/proxy-keys?include=setup_readiness&expected_route_witness_generation=…`

### 4. Model Management And Model Detail

**User entrypoints**

- `/models`
- `/models/:id`

**Frontend flow**

1. Operators list, search, create, edit, and delete model configs.
2. Model create and edit dialogs manage model metadata, OpenAI accepted format, loadbalance strategy, and enabled state.
3. Model detail owns access-target authoring as one mixed list: same-family Model Targets and Terminal Targets share the global `position` order, cross-type adjacent moves use the same controls, and Terminal Target management covers the model's private endpoint bindings.
4. The Terminal Target dialog's “高级请求设置” group lets operators configure request limits, custom request headers, and the optional custom request parameters JSON overlay (format/clear actions, top-level count summary, field-level validation, and server 422 mapping back to the editor). The routing schedule is edited in its own sibling section rather than inside that group, because it governs routing eligibility rather than request content.
5. Request logs preserve the requested model while final-target fields show the terminal model reached through the access graph.

**UI-driven backend touchpoints**

- `GET /api/models`
- `POST /api/models`
- `GET /api/models/{model_config_id}`
- `PUT /api/models/{model_config_id}`
- `DELETE /api/models/{model_config_id}`
- `POST /api/models/by-endpoints`
- `GET /api/models/{model_config_id}/targets`
- `POST /api/models/{model_config_id}/targets`
- `PATCH /api/models/{model_config_id}/targets/{target_id}`
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position`
- `DELETE /api/models/{model_config_id}/targets/{target_id}`
- `GET /api/models/{model_config_id}/connections`
- `POST /api/models/{model_config_id}/connections`
- `PATCH /api/models/{model_config_id}/connections/{connection_id}`
- `DELETE /api/models/{model_config_id}/connections/{connection_id}`

`POST /api/models/by-endpoints` is used by the Endpoints page to hydrate model references. `GET /api/models/by-endpoint/{endpoint_id}` and `POST /api/models/connections/batch` remain backend/API-client surfaces without a current production frontend caller.

### 5. Endpoints, Loadbalance Strategies, And Pricing Templates

**User entrypoints**

- `/route/endpoints`
- `/route/ban-policies`
- `/route/pricing`

**Frontend flow**

1. Endpoints define reusable upstream credentials and base URLs that Terminal Targets can share.
2. The Ban Policies page exposes only `Strategies`, `Current State`, and `Events`; it does not render incidents. The dashboard consumes `/api/loadbalance/incidents` for its incident banner and recent-event alerts.
3. Pricing templates define reusable cost models attached to Terminal Targets with five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. A template may additionally define one threshold-plus-five-price `tier` card; a request that exceeds the threshold uses that complete card for all five components rather than marginal billing.
4. Pricing-template management saves explicit strings for every component. Missing/null/blank inputs normalize to `"0"`; explicit `"0"` is configured free pricing, not missing pricing data.
5. Pricing supports JSON file or pasted-text import with `upsert_by_name` or `create_only`, a connection-usage lookup, and delete protection when Terminal Targets still depend on a template.
6. Request logs and cost math consume canonical disjoint token components: base input, cache-read input, cache-creation input, base output, and reasoning output. Aggregate `cached_tokens` is derived-only for presentation.
7. These resources are Default-profile-scoped and are usually managed before or alongside model-detail work.
8. The defaults action creates the canonical loadbalance strategy rows for Default profile id `1`.

**UI-driven backend touchpoints**

- `GET /api/endpoints`
- `POST /api/endpoints`
- `PUT /api/endpoints/{endpoint_id}`
- `DELETE /api/endpoints/{endpoint_id}`
- `POST /api/endpoints/{endpoint_id}/duplicate`
- `GET /api/loadbalance/strategies`
- `POST /api/loadbalance/strategies/defaults`
- `POST /api/loadbalance/strategies`
- `GET /api/loadbalance/strategies/{strategy_id}`
- `PUT /api/loadbalance/strategies/{strategy_id}`
- `DELETE /api/loadbalance/strategies/{strategy_id}`
- `GET /api/loadbalance/current-state`
- `POST /api/loadbalance/current-state/{connection_id}/reset`
- `GET /api/loadbalance/events`
- `GET /api/loadbalance/events/{event_id}`

`GET /api/endpoints/connections` remains a shared API-client/reference-data catalog surface without a current production frontend consumer.

The loadbalance strategy routes are pinned to Default profile id `1`, and the defaults action is a no-body POST that returns the created/current canonical rows plus creation metadata.
- `GET /api/pricing-templates`
- `POST /api/pricing-templates`
- `POST /api/pricing-templates/import`
- `GET /api/pricing-templates/{template_id}`
- `PUT /api/pricing-templates/{template_id}`
- `DELETE /api/pricing-templates/{template_id}`
- `GET /api/pricing-templates/{template_id}/connections`

### 6. Request Investigation

**User entrypoints**

- `/observe/requests`
- Dashboard deep links into `/observe/requests`

**Frontend flow**

1. Operators browse request history with server-backed filters; the default view is the retained ingress chain (`view=ingress_chains`) paged by signed chain cursors.
2. Exact request investigation opens the overview detail sheet through `request_id`; failed rows show the failure-first summary.
3. `ingress_request_id` groups all upstream attempts for one incoming proxy request; the chain envelope carries finalized usage evidence, expected/retained counts, and routing evidence flags.
4. The request-log UI keeps the requested `model_id` separate from the final `resolved_target_model_id` so operators can see authoring intent and execution target at the same time.
5. The Client filter sends `client_rule_id` to the backend and matches caller User-Agent Client Rules against `caller_user_agent` only.
6. Pricing filters use the four-state `pricing_status`; the old `priced` boolean alias is rejected by the backend.
7. CSV export is server-side over the full filtered result set (`/api/stats/requests/export`), never assembled from the current page.
8. Audit payloads load only on the dedicated `/observe/requests/:requestId/audit` page; the detail sheet remains overview-only.

**Backend touchpoints**

- `GET /api/stats/requests` (attempt view and `view=ingress_chains` chain view)
- `GET /api/stats/requests/export` (server-side full filtered CSV)
- `GET /api/stats/requests/{request_id}` (v2 detail: scoped statuses, failure projection, pricing layers)
- `GET /api/stats/cost-segments` and `/api/stats/cost-segments/{segment_key}/symbols`
- `GET /api/audit/logs`
- `GET /api/audit/logs/{log_id}`
- `GET /api/audit/logs/{log_id}/body/request` and `/body/response` (byte-exact raw downloads)
- `GET /api/settings/costing` / `PUT /api/settings/costing` (timezone merged into the costing CAS)

All Requests/Audit list, detail, chain, and export responses send `Cache-Control: private, no-store` and carry profile-sensitive `Vary`.

For the page-specific query contract and UI behavior, see section 8 (Requests Page Specification).

### 7. Settings And Access Control

**User entrypoints**

- `/system/settings`
- `/system/proxy-keys`

**Frontend flow**

1. Settings uses canonical public URLs with `scope=global|instance` and a section allowlist; the legacy `tab` query value is dropped during canonicalization. `scope=global` covers reporting currency, timezone, audit/privacy, and config rules; `scope=instance` covers operator authentication, automatic retention policy with owner actual coverage, manual cleanup, and the retention job center.
2. Reporting currency is a single active epoch contract: the UI shows the active epoch/symbol and, once an epoch exists, direct currency-code authoring is locked and any change goes through the currency migration preflight; historical snapshots stay read-only.
3. Timezone is part of the costing CAS (no standalone timezone route); preview uses the current instant and current IANA offset, and changing the preference only affects display and Custom input interpretation.
4. Destructive retention changes (enable `null -> N`, shorten `M -> N`, or one-time cleanup) always run through a fresh server preflight dialog with exact/estimated impact, owner coverage after, and keyword confirmation, then create a server-persisted job; queued manual jobs are cancellable from the job center, running manual purges are not.
5. Startup settings remain in the plaintext bootstrap file selected by `PRISM_CONFIG_PATH`; edit `config.json` directly and restart Prism to apply changes.
6. Saving a transition from disabled to enabled authentication broadcasts a localStorage auth-state update, refreshes local auth state, and redirects the current tab to `/auth/login`; other open tabs re-bootstrap from the broadcast. Enabling/disabling requires explicit acknowledgements (proxy-key readiness, permissive attribution when disabling, session invalidation).
7. Proxy API keys are managed on their own route and stay global rather than profile-scoped. They can be issued while auth is disabled, but runtime proxy-key authentication is enforced only after operator auth is enabled. Auth enablement uses the Proxy owner's one-instant readiness count and 30-second safe-active horizon; a zero-safe-key enable requires an explicit acknowledgement, and key secrets are shown once only.

Mail bootstrap fields remain parse-compatible for existing `config.json` files, but Prism no longer sends mail. Fresh bootstrap seeds use backend `8000`, frontend `5173`, and PostgreSQL `15432`, but `./start.sh` follows the existing bootstrap file's configured `server.port` when one already exists. `runtime.sideEffects.attemptTimeout` is seeded as `"10s"`; the `runtime.transport` section was removed outright, and outbound provider requests are no longer subject to any connection or timeout limits (a leftover `runtime.transport` block is rejected with a readable migration error). Direct external `config.json` edits are not watched automatically, and existing valid files are not rewritten by the launcher. To reset startup defaults, stop Prism, remove or relocate the bootstrap file, and restart.

OpenAI image routing is a separate dimension with its own rule. Operators set `openai_image_operations` on a model and `openai_image_capability` on each Terminal Target; an omitted target capability is derived from the owner model. Coverage is containment rather than equality, because generations and edits are additive capabilities on one protocol rather than mutually exclusive wire formats: a target serving both can back a model that only accepts one, and a narrower target is rejected with `422 target_openai_image_uncovered`. Image requests never consult the text dimension and text requests never consult the image dimension. Image usage tokens flow through the ordinary `PER_1M` pricing pipeline; the template has no separate slot for the text/image input split, so an image template's input price must be authored as a weighted rate.

OpenAI text routing is native-only and mode-strict. Operators set runtime support on each Terminal Target through `openai_text_capability`, using `responses_only`, `chat_completions_only`, or `dual_native`; each mode may connect only to the identical mode (3×3 equality matrix, diagonal only). The management UI filters target-model candidates and locks connection capabilities to the owner model's mode, and the backend rejects cross-mode authoring with `422 target_openai_mode_mismatch` and mode changes that break existing relations with `409`. Mode-incompatible Chat Completions/Responses attempts are skipped rather than translated, and startup fails fast on persisted violations; a read-only preflight (`PRISM_OPENAI_MODE_PREFLIGHT=1`) reports violations with deterministic exit codes before upgrade.

**Backend touchpoints**

- `GET /api/settings/costing` / `PUT /api/settings/costing` (reporting currency + timezone in one CAS; currency-code change with an existing epoch goes through the migration preview/commit)
- `GET /api/settings/audit` / `PUT /api/settings/audit` (three-state `disabled|metadata_only|body_capture` per family)
- `GET /api/settings/audit/storage-summary` (bounded daily logical-storage facts + owner projections)
- `GET /api/config/header-blocklist-rules`
- `GET /api/config/header-blocklist-rules/{rule_id}`
- `PATCH /api/config/header-blocklist-rules/{rule_id}`
- `DELETE /api/config/header-blocklist-rules/{rule_id}`
- `POST /api/config/header-blocklist-rules`
- `GET /api/config/user-agent-client-rules`
- `GET /api/config/user-agent-client-rules/{rule_id}`
- `POST /api/config/user-agent-client-rules`
- `PATCH /api/config/user-agent-client-rules/{rule_id}`
- `DELETE /api/config/user-agent-client-rules/{rule_id}`
- `GET /api/settings/auth` / `PUT /api/settings/auth` (desired/effective mode, operator account, proxy-key readiness, acknowledgements; operations polled via `/api/settings/auth/operations/{id}` and public `/api/auth/operations/{id}/status`)
- `GET /api/settings/auth/proxy-keys`
- `POST /api/settings/auth/proxy-keys`
- `PATCH /api/settings/auth/proxy-keys/{key_id}`
- `POST /api/settings/auth/proxy-keys/{key_id}/rotate`
- `DELETE /api/settings/auth/proxy-keys/{key_id}`
- `GET /api/settings/auth/proxy-keys?include=setup_readiness&expected_route_witness_generation={generation}` (Proxy readiness + route-witness owner handoff)
- `GET /api/settings/log-retention` / `PUT /api/settings/log-retention` (revision CAS, destructive classifier, owner-drift lineage)
- `POST /api/maintenance/log-retention/preflights` (fresh destructive preview; `policy_change` | `manual_cleanup`)
- `POST /api/maintenance/log-retention/jobs` (sealed manual job: operation_id + preflight token + keyword)
- `POST /api/settings/log-retention/owner-drift-archive`
- `POST /api/settings/costing/currency-migration-drafts` and its bounded chunk/item/preview pages
- `POST /api/settings/costing/currency-migrations/preview` / `POST /api/settings/costing/currency-migrations/commit` (cutover, same-currency repair, or `archive_unused_fx`)
- `GET /api/settings/costing/pricing-migration-inventories/{inventory_id}/templates`
- `GET /api/settings/costing/pricing-migration-inventories/{inventory_id}/fx-evidence`

Global log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.

The Settings UI exposes fixed deletion presets of `1`, `7`, `30`, `90` days, or all data, runs a fresh manual-cleanup preflight with impact counts and owner actual coverage, then creates a durable sealed job. The retention job center (`scope=instance&section=retention-jobs`) lists/polls/filters durable v2 jobs, opens detail checkpoints and partition evidence, and cancels queued manual jobs (running manual purges return `409 purge_not_cancellable`; running automatic jobs accept `cancel_requested`). The audit & privacy card shows the three-state family policy plus the bounded storage summary (retained rows/bytes with exact/estimated/unavailable states, never zero defaults) and the fixed 4 MiB/body + 12+4 MiB ingress capture limits. Currency migration pages are bounded and preserve pending/null evidence; the archive-only FX action records a ledger without changing the active currency epoch or template prices.

### 8. Runtime Proxy Traffic

Runtime auth follows the latest proxy-key snapshot immediately after auth and proxy-key management writes: rotated, disabled, or expired keys stop authorizing new supported `/v1` and `/v1beta` runtime operations. Create and rotate return a secret shown once in a copyable dialog; rotation replaces the secret on the same ledger row and records when and how many times it has rotated, while DELETE removes the row.

**User entrypoints**

- External clients calling one of the operation-registered runtime routes listed below

**Runtime flow**

1. Global CORS runs first. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the exact operation registry in that order. Once inside the registry, unsupported routes and wrong methods reject before body reads, provider transport, telemetry, audit, feedback, or runtime side effects.
2. Provider adapters parse provider-specific payloads, build upstream requests, adapt responses, classify streams, extract usage, and own pure OpenAI Chat/Responses conversion.
3. Planning evaluates the model's enabled mixed access-target rows in authored `position` order once: `single` keeps only the first enabled row, `fill-first` walks the mixed order, and `round-robin` rotates the direct mixed rows. A Model Target row resolves recursively through the child model's own strategy and contributes one contiguous block; candidate-local misses (zero-leaf child, operation incompatibility, unavailable connection, routing window closed) skip to the next peer in effective order, while cycle/depth and missing-strategy errors fail closed. The routing-window check runs after Ban filtering, so a target excluded by its schedule is one already known to be otherwise usable, and the reopen instant reported to the operator is not contradicted by a ban.
4. Connection planning applies the attached explicit Ban Policy strategy and per-connection limits.
5. When any planned candidate carries custom request parameters, Prism buffers and validates the ingress body as a JSON object, applies the per-Connection top-level shallow overlay after provider-native model/path rewrite, and materializes an immutable merged body per attempt (failover/hedge candidates never share mutable body storage). Gemini path-streaming switches from the request-body streaming fast path to the buffered path in this case.
6. The shared runtime/gateway owns operation registration, admission, routing, SSE lifecycle, accounting, pricing, request-log metadata, and durable handoff. Telemetry/audit rows are materialized by background workers from the runtime outbox; non-accepted side effects use their own in-memory or worker queues.
7. After the first downstream byte or event on a stream, no retry, redirect, or hedge replay can start.
8. Missing pricing stays visibly degraded or unpriced, it never silently looks complete.

**Backend touchpoints**

- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/responses/input_tokens`
- `POST /v1/responses/compact`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1beta/models/{model}:streamGenerateContent`
- `POST /v1beta/models/{model}:countTokens`

These 12 allowlisted runtime routes are defined in `backend/internal/httpapi/runtime/operations.go` and are intentionally separate from `/api/*` management routes. Prism does not treat `/v1` or `/v1beta` as catch-all prefixes.
`GET /v1/models` remains local and always returns the OpenAI-shaped list of enabled OpenAI models. Query parameters, including the retired `client_version`, do not select an alternate response shape.

### 9. Priority Operations Runbook

Before shipping priority-sensitive backend changes, run the standard priority regression tests from the backend tree:

```bash
cd backend && go test ./tests/priority/...
```

The expected pass signal is exit code `0`. Failures should be treated as regressions in the priority classification, admission, scheduler, or lane-isolation behavior covered by the checked-in backend suite.

Operational triage by symptom:

- Lane budget pressure: identify the labeled DB lane first. `runtime_execution` protects proxy work; `management`, `cache_refresh`, `runtime_telemetry`, `runtime_feedback`, and `background_jobs` have separate budgets and should be remediated at their owning workload instead of increasing unrelated pools.
- Overload or `Retry-After`: honor the retry delay and reduce client concurrency. M3 reporting and maintenance routes are expected to shed before M2/M1 management work, and management/background pressure should not affect proxy execution capacity.
- Scheduler lag: expect delayed, coalesced, retried, or dropped background work according to worker policy. Do not add ad hoc goroutines or timers; register new recurring, retrying, or delayed work with the scheduler.
- Outbox failures: inspect the relevant durable store state. Management side-effect outbox rows retry or become permanent failures without rolling back committed primary state.
- Runtime telemetry loss: accepted runtime activity intents should drain to the telemetry outbox unless terminal validation or forced shutdown prevents completion. Treat lost accepted telemetry as a durability incident.
- Runtime feedback loss: feedback is best effort and may drop on queue full, invalid event, closed pipeline, or store failure. Drops should be accounted for, but they must not delay or fail proxy responses.
- Audit or stat lag: raw audit reads remain bounded by time window and keyset cursor. Dashboard overview reads come from the canonical `/api/stats/dashboard` aggregate snapshot; it returns backend-computed Routing Health Map data, but the current dashboard does not render that field. Broad deletes run as durable management jobs.
- Cache generation lag: management mutations advance durable runtime-cache generations before commit. Cache warming may lag, but runtime reads compare generation vectors and refresh or fail closed for stale, missing, or unverifiable auth-sensitive snapshots.

### Cross-References

- Product scope: `docs/product.md`
- API contracts: `docs/architecture.md` (section 14, API Reference)
- Request investigation details: `docs/product.md` (section 8, Requests Page Specification)
