# Architecture Document: Prism

## 1. System Overview

```
┌─────────────┐     ┌──────────────────────────────────────────────┐     ┌──────────────┐
│   Client    │     │                    Prism                     │     │   Providers  │
│             │     │  ┌────────────┐  ┌──────────┐               │     │              │
│ Port 15173* │◀────│  │ Management │  │  Proxy   │          │◀────│  OpenAI API  │
│             │     │  │   APIs     │  │  Engine  │          │     │  Anthropic   │
│             │     │  └─────┬──────┘  └────┬─────┘          │     │  Gemini API  │
└─────────────┘     │        │              │                │     └──────────────┘
                    │  ┌─────▼──────────────▼─────┐          │
                    │  │    PostgreSQL Database    │          │
                    │  │ (profiles, models,        │          │
                    │  │  endpoints, connections,  │          │
                    │  │  settings, request_logs,  │          │
                    │  │  audit_logs)              │          │
                    │  └───────────────────────────┘          │
                     │             Port 18000*                 │
                    └──────────────────────────────────────────┘
```

*Local `./start.sh` defaults are backend `18000`, frontend `15173`, and PostgreSQL `5432`. Standalone frontend containers commonly expose `3000`.

## 2. Component Architecture

### 2.1 Backend (Go runtime)

```
backend/
├── cmd/prism-backend/          # Go process entrypoint
├── internal/
│   ├── httpapi/
│   │   ├── management/         # /api/* management handlers
│   │   ├── runtime/            # /v1/* and /v1beta/* proxy handlers
│   │   ├── realtime/           # WebSocket room management and publishing
│   │   └── openapi/            # checked-in OpenAPI loader and docs handlers
│   ├── platform/
│   │   ├── config/             # environment and runtime settings
│   │   ├── http/               # server assembly and route mounting
│   │   ├── migrate/            # SQL migration runner and schema helpers
│   │   ├── startup/            # startup sequencing and default seeding
│   │   └── version/            # VERSION loader
│   ├── domain/
│   │   ├── audit/              # audit persistence and redaction helpers
│   │   ├── loadbalance/        # routing, recovery, and state logic
│   │   └── stats/              # request-log and aggregate query logic
│   ├── endpointdomain/         # endpoint and connection helpers
│   ├── profiledomain/          # selected vs active profile helpers
│   └── vendordomain/           # shared vendor catalog helpers
├── migrations/                 # SQL migration chain applied at startup
├── testdata/                   # checked-in OpenAPI, bundle, and realtime fixtures
├── tests/                      # Go contract, integration, and runtime regressions
├── Dockerfile                  # live Go backend image build
├── docker-compose.yml          # local PostgreSQL helper on host port 5432
└── VERSION                     # backend version surface
```

### 2.2 Frontend (React + Vite)

```
frontend/
├── src/
│   ├── main.tsx                # Entry point
│   ├── App.tsx                 # BrowserRouter + AppLayout + public auth routes + protected shell routes
│   ├── context/
│   │   ├── ProfileContext.tsx  # Selected profile vs active profile state bootstrapped from /api/profiles/bootstrap
│   │   └── AuthContext.tsx     # Operator auth bootstrap, refresh, and session state
│   ├── lib/
│   │   ├── api.ts              # Typed API client + /api scoped X-Profile-Id injection
│   │   ├── types.ts            # TypeScript contracts aligned with backend schemas
│   │   ├── costing.ts          # Micros and currency formatting helpers
│   │   ├── timezone.ts         # Shared timezone formatting helpers
│   │   └── configImportValidation.ts # Config import validation for the current configuration format
│   ├── hooks/
│   │   ├── useConnectionNavigation.ts # Resolve connection owner + navigate to model detail
│   │   ├── useRealtimeData.ts  # WebSocket-backed live refresh helper
│   │   └── useTimezone.ts      # Shared timezone formatting helper
│   ├── components/
│   │   ├── layout/AppLayout.tsx # Provider-based sidebar shell + route-metadata breadcrumb chrome
│   │   ├── statistics/         # Spending and token visualization helpers
│   │   └── ui/                 # shadcn/ui components
│   └── pages/
│       ├── DashboardPage.tsx
│       ├── ModelsPage.tsx
│       ├── ModelDetailPage.tsx     # Model detail shell + loadbalance events tab
│       ├── EndpointsPage.tsx
│       ├── dashboard/DashboardPage.tsx # Dashboard shell with analytics tab and shared statistics content
│       ├── ProxyModelDetailPage.tsx # Proxy-model detail shell and target metadata editing
│       ├── RequestLogsPage.tsx     # Request-log investigation with lazy audit lookup
│       ├── ProxyApiKeysPage.tsx
│       ├── SettingsPage.tsx        # Profile-scoped settings shell + global auth/vendor management
│       ├── PricingTemplatesPage.tsx
│       └── LoadbalanceStrategiesPage.tsx

├── components.json             # shadcn config
├── package.json
├── vite.config.ts
└── tsconfig.json
```

### 2.3 Local Tooling and Build Workflow

- Prism is a monorepo: `backend/` and `frontend/` are root-owned directories that share the root launcher, release helper, and CI wiring.
- Root local orchestration lives in `start.sh`: it loads the root `.env`, starts PostgreSQL from `backend/docker-compose.yml`, validates that the selected bootstrap config still matches the fixed local launcher contract, and launches the Go backend service on `18000`.
- `./start.sh full` launches the frontend on `15173`, unsets `VITE_API_BASE`, and enables a launcher-local Vite proxy via `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET=http://localhost:18000` so browser traffic stays same-origin.
- Canonical startup config lives in a plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; the only optional startup env vars are `PRISM_CONFIG_PATH` and `DATABASE_URL`, and the default database URL is `postgres://prism:prism@localhost:5432/prism?sslmode=disable`.
- Plaintext bootstrap startup reads that bootstrap file directly through `PRISM_CONFIG_PATH`; old encrypted bootstrap files must be replaced before boot, and there is no compatibility mode for older bootstrap file shapes.
- The Startup tab and `PUT /api/config/bootstrap` are the only supported hot publication paths for file-backed startup edits. External edits to `config.json` are not watched automatically.
- Profile backup/restore, vendor catalog export/import, and other settings-page state flows remain PostgreSQL-backed state transport instead of bootstrap ownership.
- The current implementation keeps the split-bundle contract canonical, with `profile_config` and `vendor_catalog` both on one `version: 1` story and no surviving older bundle narrative.
- `backend/Dockerfile` is the live Go backend image build path and copies `migrations/` plus `docs/openapi.json` into the image.
- `.github/workflows/docker-images.yml` builds Docker images only (no backend pytest or frontend lint/typecheck jobs) and currently targets `linux/arm64`.

### 2.4 Priority Enforcement And Operator-Facing Failure Modes

Prism assigns trusted backend priority metadata before work touches shared resources. Runtime proxy traffic is `proxy`, management routes are `management` with an explicit `M1`, `M2`, or `M3` tier, and scheduler-owned workers are `background` with a declared subclass, budget, coalescing policy, retry policy, and drain policy. Priority-sensitive backend changes should stay covered by the standard priority regression tests, including `go test ./tests/priority/...`.

PostgreSQL capacity is split into finite named lanes: `runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `realtime`, `cache_refresh`, and `background_jobs`. Operators should treat lane saturation by owner: proxy execution pressure is separate from management UI pressure, telemetry drain pressure, lossy feedback drain pressure, realtime fanout, cache refresh, and generic background jobs. Background or management saturation must not consume protected proxy capacity.

Management overload is reported as typed admission failure with retry metadata. Lower-priority M3 reporting and maintenance routes shed before M2 and M1 management work, and proxy traffic remains isolated from management/background saturation. When overload appears, retry after the advertised delay rather than increasing client concurrency.

Scheduler lag means background workers are queued, coalesced, delayed, retried, or dropped according to their worker policy. Lag can delay dashboard fanout, telemetry materialization, email delivery, management side-effect dispatch, cache warming, and proxy-key usage flushing, but it must not make request-path handlers borrow direct goroutines, direct DB handles, or unmanaged timers.

Durable outboxes expose failure as queued, retry, sent/succeeded, dead-letter, or permanent-failure state depending on the store. Email provider failures retry and eventually dead-letter without exposing OTPs or SMTP credentials. Management side-effect dispatch failures retry or become visibly permanent failures without rolling back the already committed primary management mutation.

Runtime telemetry and runtime feedback have different loss semantics. Accepted runtime activity intents are required-durable background work until the telemetry outbox transaction commits, terminal validation fails, or shutdown prevents completion. Runtime feedback is intentionally lossy under pressure; queue-full, invalid, closed, or store-failure cases drop feedback with accounting and never block proxy responses.

Audit and statistics reads are bounded. Raw audit lists require backend-enforced time windows and keyset cursors, dashboard stats read materialized rollups, and broad deletes run as durable management jobs. Operators may see audit or stat freshness lag while background rollups or delete jobs catch up; Prism does not fall back to unbounded live aggregation to hide that lag.

Runtime cache correctness is generation-based. Management mutations advance durable runtime-cache generations in the same transaction as the primary state change, runtime reads validate generation vectors and refresh or fail closed when stale, and post-response cache warming is non-authoritative. Cache generation lag may delay warm snapshots, but auth-sensitive runtime reads reject stale or unverifiable snapshots instead of accepting old state.

## 3. Request Flow

Prism is proxy-first. It forwards the supported provider-native generation routes it owns and is not a full OpenAI API emulator.

### 3.1 Proxy Request (Non-Streaming, Native Model)

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Router captures active profile snapshot at request start
  -> Gateway assigns one Prism `ingress_request_id` for the incoming runtime request
  -> Request setup resolves the native model, attached adaptive routing policy, and current candidate set in active profile scope
  -> Planner ranks candidates from live runtime state, admission counters, and current circuit state
  -> Executor claims the primary attempt lease and forwards the request to the selected endpoint
  -> Upstream responds with JSON
  -> Gateway returns JSON to client, releases any non-stream lease, persists one `request_logs` row for the attempt, and feeds the outcome back into runtime routing state
```

### 3.2 Proxy Request (Proxy Model With Target Selector)

```
Client -> POST /v1/messages {model: "claude-sonnet-4-5"}
  -> Router captures active profile snapshot
  -> LoadBalancer looks up model config in active profile scope
  -> Model is proxy -> load proxy_selection_strategy and explicit proxy_targets metadata
  -> Evaluate same-profile, same-api-family native targets using the configured selector
  -> Select connection from that chosen native target model only
  -> ProxyService forwards request
  -> Upstream responds; request log keeps model_id=requested proxy and resolved_target_model_id=chosen native target
  -> Gateway returns response to client
```

### 3.3 Proxy Request (Streaming)

```
Client -> POST /v1/chat/completions {model: "gpt-4o", stream: true}
  -> Router captures active profile snapshot
  -> Gateway assigns one Prism `ingress_request_id`
  -> Proxy target resolution (if needed) finishes before native adaptive routing begins
  -> Planner resolves the live candidate set and executor claims a streaming lease before opening the upstream stream
  -> ProxyService opens streaming connection to the selected upstream endpoint
  -> SSE chunks stream directly back to the client from the Go runtime transport layer
  -> Streaming heartbeats keep the lease fresh while the stream is open
  -> On upstream error: release the stream lease, classify the failure, and continue only if another candidate and hedge rules still allow another attempt
  -> On stream finalization or cancellation: release the stream lease, persist the per-attempt request log, and record runtime feedback
```

### 3.4 Vendor and api_family Routing

| Vendor                | Proxy Path                                    | Upstream Path                                      | Auth Header                                          |
| --------------------- | --------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                | `POST /v1/chat/completions`, `POST /v1/responses` | `{base_url}/v1/chat/completions`, `{base_url}/v1/responses` | `Authorization: Bearer {key}`                        |
| Anthropic             | `POST /v1/messages`                           | `{base_url}/v1/messages`                           | `x-api-key: {key}` + `anthropic-version: 2023-06-01` |
| Gemini                | `POST /v1beta/models/{model}:generateContent` / `POST /v1beta/models/{model}:streamGenerateContent` | `{base_url}/v1beta/models/{model}:generateContent` / `{base_url}/v1beta/models/{model}:streamGenerateContent` | `Authorization: Bearer {key}`                        |

OpenAI runtime support is limited to generation proxying for `POST /v1/chat/completions` and `POST /v1/responses`. Stored Responses object lifecycle APIs, including retrieve, list, delete, cancel, and compact routes, are outside Prism's supported contract.

Note: Gemini requests use native `/v1beta/models/{model}:...` paths only. When a Gemini proxy model resolves to a different native model ID, the proxy rewrites the model ID segment in the URL path to the resolved native target model ID before forwarding upstream.
For Gemini, the `:streamGenerateContent` path is authoritative for stream classification even when the request body omits `stream: true`.

Runtime upstream requests capture an immutable bootstrap runtime snapshot at request start. The snapshot includes proxy buffering mode and an HTTP client built from startup bootstrap transport settings. The raw `runtime.transport.requestTimeout` Go duration is applied as `http.Client.Timeout`, which makes it the whole-request timeout for outbound provider calls. Existing bootstrap files must include this field; `"60s"` keeps the prior timeout behavior, and a missing value fails startup validation by design. Raw `runtime.sideEffects.attemptTimeout` is a separate per-attempt background side-effect enqueue budget, defaults to `"10s"` in newly seeded configs, fails startup validation when missing from existing configs, and is restart-required rather than hot-applied.

Hot bootstrap projection builds a new aggregate snapshot, validates it, then atomically publishes it for future work. CORS origin checks, auth TTL and cookie metadata, mail delivery settings, runtime buffering, runtime transport, and M2/M3 management admission limits are hot-apply boundaries. New requests and new email sends read the current snapshot; in-flight proxy requests keep the HTTP client they captured, and a retired runtime transport only has idle connections closed.

Restart-required boundaries are structural process resources: listener host and port, docs route enablement, PostgreSQL URL and pool budgets, runtime side-effects attempt timeout, runtime secret encryption key, auth JWT signing key, and the state-transfer bundle key. Those values can be written through the bootstrap API, but they do not change the running process until Prism restarts.

Vendor rows are global publisher metadata. Models may keep `vendor_id = null` and `vendor = null`, while runtime compatibility and redirect checks still use the model's required `api_family`, not the vendor row. The frontend owns vendor icon rendering through a locally vendored registry sourced from pinned `cc-switch` presets, and it falls back to a monogram or placeholder only at render time when icon data or vendor metadata is missing or unknown. The Models page still renders each row's `api_family` metadata even when vendor identity is absent.

### 3.5 Management API Profile Scoping
- Prism keeps one route-class matrix:
  - Global management routes omit `X-Profile-Id`.
  - Profile-scoped management routes require `X-Profile-Id` and resolve against the selected profile.
  - Runtime proxy routes (`/v1/*`, `/v1beta/*`) ignore management overrides and always use the active profile.
- Profile-scoped config bundle routes live under `/api/config/profile/*`, and `POST /api/config/profile/import/preview` is also profile-scoped and requires `X-Profile-Id`.
- Global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/config/vendors/*`, `/api/auth/*`, `/api/realtime/*`, and the auth/email/proxy-key settings routes under `/api/settings/auth*`.
- Selected profile (UI management context) and active profile (runtime routing context) are intentionally distinct states.
- Scope-control errors return stable `code` values plus human-readable `detail` text.
- Runtime proxy routes (`/v1/*`, `/v1beta/*`) always use active profile and ignore override headers.

The protected frontend shell now boots profile state from `GET /api/profiles/bootstrap`, derives sidebar destinations and breadcrumbs from the route metadata registry in `frontend/src/components/layout/app-layout/navigationProfileConfig.ts`, and persists only the desktop sidebar collapse preference in localStorage. Mobile drawer state remains transient browser UI state.

The Settings shell mirrors that split: the Profile tab keeps backup, billing and currency, timezone, audit and privacy, and retention flows scoped to the selected profile, while the Global tab owns instance-wide authentication and the shared vendor catalog.

The current split-bundle config workflow also mirrors that ownership split:
- profile export/import uses `bundle_kind = profile_config` and is authoritative only for profile-scoped rows
- `GET /api/config/profile/export` returns the safe redacted bundle, while `POST /api/config/profile/export/with-secrets` returns the dangerous full secret-bearing bundle
- vendor catalog export/import uses `bundle_kind = vendor_catalog` and is authoritative only for shared vendor metadata
- profile bundles never export plaintext endpoint API keys; safe exports null reusable endpoint secret refs and omit `secret_payload.entries[]`
- dangerous profile exports include `secret_payload.entries[]` and reusable endpoint secret refs
- profile import resolves vendors by `vendor_key` when present, keeps vendorless models vendorless when `vendor_key` is null, reuses existing global vendors, and never mutates existing global vendor metadata from profile-bundle hint drift
- profile import replaces profile-scoped rows only, while global vendor rows, other profiles, and request logs remain untouched
- vendor catalog import mutates only the shared vendor catalog and leaves profile-scoped rows untouched
- apply is header-bound with `X-Prism-Preview-Token`, and the raw bundle JSON stays unchanged in transit
- these bundle and backup flows remain PostgreSQL-backed state transport and do not seed or replace the startup bootstrap JSON

### 3.6 Custom Header Injection

When a connection has `custom_headers` configured, they are injected into the upstream request after all other headers:

```
build_upstream_headers():
  1. Start with client headers (minus hop-by-hop, minus client auth headers, minus proxy-controlled auth/version headers)
  2. Apply blocklist sanitization to client-supplied headers
  3. Add api-family auth headers
  4. Add api-family extra headers (e.g., anthropic-version)
  5. Apply connection custom_headers (from `connections.custom_headers` JSON)
     -> Same-name headers from earlier steps are overwritten
  6. Apply final blocklist pass (with api-family auth/version headers protected)
     -> Blocked headers cannot be reintroduced by custom headers
  7. Return final header dict
```

Custom headers are a power-user feature. While they can override most headers, they cannot be used to re-add headers that are blocked by the Header Blocklist. This is enforced by applying the blocklist last in the header construction pipeline.

### 3.7 Realtime Dashboard And Analytics Updates

```
Dashboard overview page -> WebSocket connect /api/realtime/ws
  -> If auth enabled: management auth handlers validate the access-token cookie
  -> Client sends {type: "subscribe", profile_id, channel: "dashboard"}
  -> Realtime manager stores dashboard room membership keyed by profile and channel

Proxy request completes
  -> Stats service persists the `request_logs` row
  -> Dashboard publisher gathers:
     - request_log
     - stats_summary_24h
     - api_family_summary_24h
     - spending_summary_30d
     - throughput_24h
     - routing_route_24h
  -> Broadcast {type: "dashboard.update", ...payload} to dashboard subscribers for that profile
  -> `frontend/src/pages/dashboard/useDashboardRealtime.ts` merges the overview payload into dashboard state
  -> On reconnect or manual refresh, frontend reconciles overview state through REST bootstrap calls

Dashboard analytics tab -> WebSocket connect /api/realtime/ws
  -> Client sends {type: "subscribe", profile_id, channel: "analytics", preset}
  -> Realtime manager stores analytics room membership keyed by profile, channel, and preset scope
  -> Service sends an initial full `analytics.snapshot` for that {profile_id,preset}
  -> Manual refresh sends {type: "refresh", profile_id, channel: "analytics", preset}
  -> Refresh returns a fresh full `analytics.snapshot` on the socket
  -> Analytics snapshots include the usage snapshot plus endpoint model statistics keyed by endpoint ID string
  -> The frontend treats each `analytics.snapshot` as a full replacement for that scoped analytics view
```

The realtime API has two supported channels. `dashboard.update` is the overview dashboard signal and does not carry analytics page replacement data. `analytics.snapshot` is scoped by `{profile_id,preset}` inside the WebSocket message payload and powers the Analytics tab without requiring UI calls to `/api/stats/*`. The REST stats endpoints, including `GET /api/stats/usage-snapshot`, remain supported API and debug surfaces.

## 4. Routing Strategies and Runtime Health Signals

### 4.1 Routing policy contract

- Native models still attach one profile-scoped loadbalance strategy, but strategy rows now use a top-level family discriminator: `strategy_type = legacy | adaptive`.
- `legacy` strategies carry `legacy_strategy_type` (`single`, `fill-first`, or `round-robin`) plus `auto_recovery`. `adaptive` strategies carry `routing_policy` with `routing_objective`, `hedge`, `circuit_breaker`, and `admission` branches.
- The selected profile's loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default legacy routing` and `Default adaptive routing` for that profile.
- The adaptive strategy template's explicit circuit-breaker defaults come from the canonical backend load-balance policy code and the persisted strategy config, not from environment fallback knobs.
- Upstream request timing is controlled by shared backend timeout settings, not by per-strategy timeout documents.

### 4.2 Runtime execution pipeline

1. Request setup resolves the active-profile model, attached strategy, and one immutable effective strategy snapshot for the request.
2. Planner and runtime-state helpers read `routing_connection_runtime_state` to build the current candidate set from circuit state, admission counters, and runtime health signals.
3. Executor claims per-attempt leases, uses the shared upstream timeout behavior from the backend runtime, and may launch hedges only within the configured `max_additional_attempts` budget before any client-visible bytes are committed.
4. Passive request outcomes feed back into runtime state, while durable transition history stays in `loadbalance_events`.

If all eligible candidates are unavailable inside the current policy window, the gateway returns `503` with routing-availability detail.

## 5. Model Proxy Routing

### 5.1 Concept

Proxy models are selector-driven routing records that choose one native target model per request. A proxy request first resolves one same-`api_family` native target from `proxy_targets`, then that chosen target model's attached legacy or adaptive strategy runs unchanged. The runtime never accepts empty target lists, missing target metadata, non-contiguous positions, or cross-family proxy targets.

### 5.2 Rules

- Only same-`api_family` proxying
- Targets must be native models (no chained proxy models)
- Proxy models have no connections of their own
- Proxy models do not attach a loadbalance strategy of their own; the resolved native model's attached strategy applies.
- Proxy models require `proxy_selection_strategy`; native models must leave it null.
- Supported selectors are `ordered_fallback`, `weighted_static`, and `priority_static`.
- Every proxy target carries `target_model_id`, contiguous zero-based `position`, `weight >= 1`, and `target_priority >= 0`.
- Model IDs are unique within a profile (same model ID may exist in other profiles)
- Once one target model is selected for a request, retries stay inside that target model's native connection plan; there is no cross-target retry in the same request.
- The gateway may normalize proxy request payloads before forwarding (for example: requested proxy model ID rewritten to the resolved native target model ID for upstream compatibility).

Model contracts require `api_family`; `vendor_id` is optional metadata. Vendor CRUD remains global, while proxy compatibility is checked against `api_family` only.

### 5.3 Selector Semantics

- `ordered_fallback`: checks routable targets by `position`, then proxy target row id, and uses the first target whose native plan is available.
- `priority_static`: checks routable targets by `target_priority`, then `position`, then proxy target row id.
- `weighted_static`: filters to currently routable same-family native targets, keeps them in `position`, then proxy target row id order, and advances a deterministic cursor over each target's `weight`.

### 5.4 Resolution

```
resolve_model(profile_id, model_id):
  config = lookup(profile_id, model_id)
  if config.model_type == "proxy":
    candidates = proxy_targets(config)
    ordered = apply_selector(config.proxy_selection_strategy, candidates)
    for target in ordered:
      candidate = lookup(profile_id, target.target_model_id)
      if candidate is native and candidate has attempt plan:
        return candidate
    return not_found
  return config
```

## 6. Connection Health Detection

### 6.1 Concept

Manual health checks use one lightweight probe runner so connection verification stays on the same api-family-aware wire contract as the rest of the runtime stack.

### 6.2 Health Probes (API-Family-Specific)

Health checks send api-family-specific lightweight requests using the connection's configured model ID and a simple prompt. This validates full-chain URL routing, authentication, and model availability using the same URL-building logic as the proxy engine.

- **OpenAI**: `POST {base_url}/v1/responses` or `POST {base_url}/v1/chat/completions` based on the connection's persisted `openai_probe_endpoint_variant`; current variants are `responses_minimal` (default), `responses_reasoning_none`, `chat_completions_minimal`, and `chat_completions_reasoning_none`.
- **Anthropic**: `POST {base_url}/v1/messages` with `{"model":"{model_id}","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
- **Gemini**: `POST {base_url}/v1beta/models/{model}:generateContent` with minimal content payload and `maxOutputTokens: 1`.

### 6.3 Status Values

- `unknown` — Never checked (default)
- `healthy` — Last check succeeded (2xx or 429)
- `unhealthy` — Last check failed (401/403, connection error, timeout, other errors)

### 6.4 Connection Success Rate Badge

The primary visual health indicator for connections is the **success rate badge**, computed from `request_logs` data (not from the manual health check status).

- Success rate = `COUNT(2xx) / COUNT(*) * 100` per connection
- Badge colors: ≥98% green, 75-98% yellow, <75% red, N/A gray (no data)
- Displayed in the connection list on the Model Detail page alongside the health tooltip state
- The manual health check still updates `health_status`/`health_detail` in the database and is shown in the tooltip

### 6.5 Model Health Aggregation

Model-level health is computed by aggregating connection success rates:

- Weighted average across all connections: `SUM(success_count) / SUM(total_requests) * 100`
- Displayed on Dashboard and Models pages as a colored badge
- Same color thresholds as connection badges

### 6.4 Error Reporting

When a health check fails, the upstream error message is extracted from the response body and stored in `health_detail`. This provides actionable diagnostics (e.g., "HTTP 503: No available channel for model X" instead of just "HTTP 503"). The detail is shown in the frontend tooltip on hover.

### 6.5 URL Path Failsafe

To prevent the `/v1/v1` double-path bug (where endpoint `base_url` already contains `/v1` and the request path also starts with `/v1`):

1. **Runtime auto-correction**: `build_upstream_url()` detects repeated version segments (e.g., `/v1/v1`, `/v2/v2`) via regex and auto-corrects them, logging a warning.
2. **Input validation**: `validate_base_url()` rejects base URLs that already contain double version segments on endpoint create/update (HTTP 422).
3. **Normalization**: `normalize_base_url()` strips trailing slashes from base URLs on create/update to ensure consistent path joining.

## 7. Request Statistics

### 7.1 Concept

All proxy requests are automatically logged with telemetry data for analytics and debugging.

### 7.2 Logging Flow

```
Client → Proxy Router → LoadBalancer → ProxyService → Upstream (via Connection)
                                                         ↓
                                              Response received
                                                         ↓
                                              Return response to client

                              Background best-effort logging (async):
                                - Log request attempt to request_logs
                                - If audit_enabled: log attempt to audit_logs
```

### 7.3 Data Captured

- Profile ID attribution, model ID, api family, vendor snapshot, and connection used (ID, endpoint base URL, description)
- Prism `ingress_request_id`, per-request `attempt_number`, and best-effort `upstream_correlation_id`
- HTTP status code, response time (ms)
- Token usage (input, output, total) — extracted from upstream response
- Stream flag, request path, error details

Request-log semantics are per-attempt: one incoming runtime request can create multiple request-log rows when failover or retries occur. `ingress_request_id` groups those rows while `request_id` remains the unique identifier for one stored attempt row.

### 7.4 Query Capabilities

- Filter by model, api family, status, time range
- Aggregated statistics with grouping by model/api family/endpoint
- Pagination for request log listing

## 8. Request Audit Logging

### 8.1 Concept

Audit logging records the request-time provenance that was active when the request started. Vendorless models do not synthesize audit defaults from `api_family`; the request keeps the mode it started with, whether audit was disabled, metadata only, or full capture. Sensitive data in headers (API keys, auth tokens) is redacted before storage.
Audit rows are written per upstream attempt, including failover attempts, and metadata-only requests still create audit metadata even when bodies are not stored.

### 8.2 Audit Flow (Non-Streaming)

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Router resolves optional requested-model vendor metadata separately from runtime api_family state
  -> If vendor metadata exists: check vendor.audit_enabled; otherwise skip vendor-scoped audit
  -> ProxyService forwards request to upstream
  -> Upstream responds with JSON
  -> Log to request_logs (including profile_id)
  -> If audit_enabled:
       -> One audit row for this upstream attempt
       -> Redact sensitive headers
       -> Record connection metadata (connection_id, endpoint_base_url, endpoint_description) as snapshot
       -> Link to request_log entry via request_log_id
       -> Store immutable profile_id attribution
       -> If audit_capture_bodies = TRUE: truncate bodies to 64KB
       -> If audit_capture_bodies = FALSE: store request_body/response_body as NULL
       -> INSERT into audit_logs (non-blocking, fire-and-forget)
  -> Return response to client
```

### 8.3 Audit Flow (Streaming)

```
Client -> POST /v1/chat/completions {model: "gpt-4o", stream: true}
  -> Router resolves optional requested-model vendor metadata separately from runtime api_family state
  -> If vendor metadata exists: check vendor.audit_enabled; otherwise skip vendor-scoped audit
  -> ProxyService opens streaming connection
  -> SSE chunks piped to client
  -> On stream complete (finally block):
      -> Log to request_logs (including profile_id)
       -> If audit_enabled:
           -> One audit row for this upstream attempt
           -> Record request headers/body + response headers/status
           -> Record connection metadata (connection_id, endpoint_base_url, endpoint_description)
           -> Link to request_log entry via request_log_id
           -> Store immutable profile_id attribution
           -> Store captured response bytes when body capture is enabled and bytes were captured; is_stream is metadata only
            -> INSERT into audit_logs using a dedicated audit write path
```

### 8.4 Non-Interference Guarantees

- Audit INSERT failures are logged and never propagated to the client
- Streaming audit uses its own write path, separate from the request-scoped runtime state
- No modification to request or response pipeline
- Minimal overhead when `audit_enabled = FALSE` (flag checked once, no payload serialization)

### 8.5 Redaction

Applied at write time before INSERT — sensitive data never reaches the database:

- `authorization` preserves the scheme as `Bearer [REDACTED]`; `x-api-key` and `x-goog-api-key` become `[REDACTED]`
- Any header name containing `key`, `secret`, `token`, `auth` → value redacted
- Body fields are not redacted and may contain sensitive user data; body capture can be disabled per vendor

### 8.6 Vendor Toggle

- `vendors.audit_enabled` (BOOLEAN, default FALSE)
- `vendors.audit_capture_bodies` (BOOLEAN, default TRUE)
- Managed through the shared vendor catalog. Vendor CRUD lives in Settings → Global → Vendor Management, while the profile-scoped audit defaults UI in Settings → Profile → Audit Configuration continues to toggle `audit_enabled` and `audit_capture_bodies` against those shared vendor rows.

### 8.7 Audit Detail Sheet

The audit detail view is a right-side sheet with tabs for:

- Summary strip: model, vendor, api family, connection (ID + description + endpoint base URL), status, duration, timestamp
- Request tab: method, URL, headers (redacted), body (pretty-printed JSON)
- Response tab: status, headers, body (pretty-printed JSON when stored, or a "not recorded" notice when no response body was stored)
- Connection identity fields (`connection_id`, `endpoint_base_url`, `endpoint_description`) are displayed in the summary strip

### 8.8 Conditional Decompression (Performance Optimization)

**Background:** the Go runtime only requests decompressed response bodies when audit capture needs them. When body auditing is disabled, the proxy avoids unnecessary body decoding work.

**Implementation:**

1. **Compression Request Control:**
   - When `audit_enabled=True AND audit_capture_bodies=True`: allow the upstream client to return a body suitable for capture
   - When body auditing is disabled: Send `Accept-Encoding: identity` to request uncompressed responses
   - Decision made via `should_request_compressed_response(audit_enabled, audit_capture_bodies)` helper

2. **Header Filtering:**
   - When compression/body decoding was used: strip `content-encoding` and `content-length` headers as needed
   - When compression was NOT requested and upstream returns identity/no encoding: preserve `content-length`
   - If upstream still responds with compressed encoding, strip stale `content-encoding` and `content-length`
   - Controlled via `filter_response_headers(headers, was_requested_compressed=...)` parameter

3. **Request Flow:**
   ```
   Client -> POST /v1/chat/completions
     -> Router checks audit_enabled and audit_capture_bodies
     -> Compute request_compressed = audit_enabled AND audit_capture_bodies
     -> build_upstream_headers(..., request_compressed=request_compressed)
        -> If request_compressed=False: inject Accept-Encoding: identity
     -> Forward request to upstream
     -> Upstream returns uncompressed response (or compressed if it ignores Accept-Encoding)
     -> filter_response_headers(upstream_headers, was_requested_compressed=request_compressed)
        -> Strip stale compression metadata whenever decoding may have occurred
        -> Preserve content-length on identity/no-encoding path
     -> Return response to client
   ```

**Benefits:**
- Eliminates unnecessary decompression CPU overhead when body auditing is disabled
- Preserves correct header/body alignment in both modes
- No breaking changes to existing behavior when auditing is enabled
- Upstream servers that don't support `Accept-Encoding: identity` will still work (proxy handles both compressed and uncompressed responses)

**Testing:** Covered by DEF-067 regression tests in `tests/smoke_defect_regressions/test_conditional_decompression.py` and `tests/smoke_defect_regressions/test_headers.py`.
## 9. Batch Data Deletion

### 9.1 Concept

Flexible bulk deletion of historical `request_logs`, `audit_logs`, and `loadbalance_events` to manage database growth. The Settings UI offers 7-day, 30-day, 90-day, and delete-all actions per data type, while the API still accepts any integer `older_than_days >= 1`.

### 9.2 Request Log and Loadbalance Deletion Flow

```
User → Settings Page → "Data Management" section
  → Selects data type (Request Logs or Loadbalance Events)
  → Selects action (preset: 7/30/90 days or delete all)
  → Clicks "Delete" button → Confirmation dialog
  -> DELETE /api/stats/requests?older_than_days=7 with explicit `X-Profile-Id`
  → Returns { accepted: true }
  → Background task opens a fresh async DB session
  → Backend computes cutoff = current_utc - 7 days (or deletes all)
  → DELETE FROM request_logs WHERE created_at < cutoff (or no filter)
  → Toast: "Request Logs deletion requested"
```

The UI uses a single action builder pattern: select data type → select action → execute.

Loadbalance event deletion follows the same immediate acceptance pattern through its matching delete endpoint.

### 9.2A Audit Delete Job Flow

Audit log deletion uses durable management jobs instead of deleting rows inside the request. The compatibility query-form endpoint `DELETE /api/audit/logs` creates an audit-delete job for legacy callers, while new clients can create the same job with the JSON endpoint `POST /api/audit/logs/delete-jobs` and an explicit `Idempotency-Key` plus reason.

```
User → Settings Page → "Data Management" section
  → Selects Audit Logs
  → Selects action (preset: 7/30/90 days, before timestamp, or delete all)
  → Clicks "Delete" button → Confirmation dialog
  -> DELETE /api/audit/logs?older_than_days=7 with explicit `X-Profile-Id`
     or POST /api/audit/logs/delete-jobs with JSON scope
  → Returns 202 with { job_id, state, status_url }
  → Sets Location to the same job status URL
  → Background worker deletes matching audit rows in chunks
  → Operator can observe or cancel the job through management job APIs
```

Audit delete job status is visible through `GET /api/management/jobs` and `GET /api/management/jobs/{job_id}`. Operators can request cancellation through `POST /api/management/jobs/{job_id}/cancel`, which returns `202` with the job object when the job is in scope.

### 9.3 Independence

- Deleting `request_logs` does NOT cascade to `audit_logs`
- Deleting `audit_logs` does NOT affect `request_logs`
- On request log deletion, `audit_logs.request_log_id` is set to `NULL` (`ON DELETE SET NULL`), preserving audit rows without dangling FK references
- Optional maintenance: after large deletions, operators may run PostgreSQL `VACUUM (ANALYZE)` as part of DB maintenance

### 9.4 Frontend Placement

Data management controls are on the Settings page (`/settings`) under a "Data Management" section, below the existing "Audit Configuration" and "Configuration Backup" sections.

## 10. Database Design

See [DATA_MODEL.md](./DATA_MODEL.md) for complete schema.

## 11. API Design

See [API_SPEC.md](./API_SPEC.md) for complete API documentation.


## 12. Security Considerations

- **Operator Authentication**: Optional cookie-backed authentication for management APIs (`/api/*`). Supports username/password.
- **Proxy API Keys**: Optional API key enforcement for runtime proxy traffic (`/v1/*`, `/v1beta/*`). Keys are issued and managed through the dashboard.
- **Auth Bifurcation**: Management auth (session cookies) and runtime auth (proxy API keys) are separate enforcement paths.
- **Data at Rest**: API keys and secrets are stored in PostgreSQL. Endpoint secrets are encrypted at rest.
- **CORS**: Local browser traffic stays same-origin through the launcher-local Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL.
- **Network**: No TLS termination; run behind a reverse proxy for HTTPS. Restricted to trusted local/LAN access.

## 13. Supported Runtime API Families

The runtime plane exclusively supports three fixed API families:

- **OpenAI** (`openai`) — GPT-style request and response contracts
- **Anthropic** (`anthropic`) — Claude-style request and response contracts
- **Gemini** (`gemini`) — Gemini-native `/v1beta/models/*` contracts

The vendor catalog is separate and global. Models always carry required `api_family`, while `vendor_id` remains optional metadata, so operators may create additional vendor metadata rows such as `OpenRouter` without changing runtime compatibility. The Global settings tab exposes vendor create/edit/delete flows, and deleting a vendor clears live model vendor metadata instead of blocking the delete.
 clears live model vendor metadata instead of blocking the delete.



 instead of blocking the delete.







ing the delete.
