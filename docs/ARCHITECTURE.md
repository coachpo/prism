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

*Local `./start.sh` defaults. The backend service itself still defaults to `PORT=8000`, the frontend container exposes `3000`, and `backend/docker-compose.yml` binds PostgreSQL on `15432`.

## 2. Component Architecture

### 2.1 Backend (FastAPI)

```
backend/
├── app/
│   ├── alembic/                # Packaged Alembic env + revisions used at runtime
│   ├── main.py                 # CLI entrypoint, app factory, lifespan, CORS, router mounting
│   ├── dependencies.py         # Shared FastAPI dependencies (active/effective profile scope)
│   ├── core/
│   │   ├── config.py           # App settings (pydantic-settings)
│   │   ├── database.py         # Async engine, session factory, Base
│   │   └── migrations.py       # Programmatic Alembic runner
│   ├── models/
│   │   ├── models.py           # ORM re-export boundary
│   │   └── domains/            # Identity, routing, and observability tables
│   ├── schemas/
│   │   ├── schemas.py          # Pydantic re-export boundary
│   │   └── domains/            # Auth, routing, stats, pricing, and profile contracts
│   ├── routers/                # API route handlers
│   │   ├── profiles.py         # /api/profiles CRUD + CAS activation
│   │   ├── vendors.py          # /api/vendors global catalog CRUD + usage lookup + delete safety
│   │   ├── models.py           # /api/models CRUD
│   │   ├── endpoints.py        # /api/endpoints CRUD + duplication + reordering
│   │   ├── connections.py      # /api/models/{id}/connections CRUD + health-check + owner
│   │   ├── pricing_templates.py # /api/pricing-templates CRUD + usage
│   │   ├── stats.py            # /api/stats requests/summary/success-rates/spending + batch metrics + batch delete
│   │   ├── monitoring.py       # /api/monitoring overview, vendor, model, and manual-probe routes
│   │   ├── audit.py            # /api/audit/logs list/detail/batch delete
│   │   ├── loadbalance.py      # /api/loadbalance/current-state reset/list + events list/detail/batch delete
│   │   ├── settings.py         # /api/settings/costing, /api/settings/timezone, /api/settings/monitoring, /api/settings/auth, /api/settings/auth/proxy-keys
│   │   ├── auth.py             # /api/auth login/logout/refresh/session/password-reset/webauthn
│   │   ├── config.py           # /api/config/profile/* + /api/config/vendors/* + header blocklist CRUD
│   │   └── proxy.py            # /v1/* and /v1beta/* catch-all proxy handlers
│   └── services/               # Business logic + shared app infrastructure
│       ├── auth/               # Session, password reset, proxy-key internals
│       ├── auth_service.py     # Auth public boundary
│       ├── background_tasks.py # Lifespan-managed async worker queue
│       ├── monitoring/         # Probe runner, scheduler, monitoring queries, routing feedback
│       ├── monitoring_service.py # Monitoring public boundary
│       ├── proxy_service.py    # Request forwarding, streaming, header sanitization
│       ├── loadbalancer/       # Planner, persistent state, recovery, events, and admin facade
│       ├── stats_service.py    # Request logging, aggregation queries, metrics batching
│       ├── audit_service.py    # Audit recording, redaction
│       ├── costing_service.py  # Token costing, FX conversion, pricing snapshots
│       ├── realtime/           # WebSocket room state and broadcasts
│       ├── webauthn/           # Passkey registration/authentication internals
│       └── webauthn_service.py # Passkey public boundary
├── Dockerfile                  # Runtime image; copies uv and installs from `uv.lock`
├── docker-compose.yml          # Local PostgreSQL helper on host port 15432
├── pyproject.toml              # Runtime deps, dev dependency group, package data, and console script
├── uv.lock                     # Locked dependency graph consumed by `uv sync --locked`
└── alembic.ini                 # Root Alembic CLI config pointing at `app/alembic`
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
│       ├── StatisticsPage.tsx
│       ├── MonitoringPage.tsx      # Monitoring overview grouped by vendor
│       ├── MonitoringVendorPage.tsx # Monitoring vendor drill-down grouped by model
│       ├── MonitoringModelPage.tsx # Monitoring model detail with connection history and manual probe
│       ├── RequestLogsPage.tsx     # Request-log investigation with lazy audit lookup
│       ├── ProxyApiKeysPage.tsx
│       ├── SettingsPage.tsx        # Profile-scoped settings shell + backend-owned monitoring cadence + global auth/vendor management
│       ├── PricingTemplatesPage.tsx
│       ├── LoadbalanceStrategiesPage.tsx
│       └── monitoring/             # Monitoring polling hooks and route-local presentation helpers

├── components.json             # shadcn config
├── package.json
├── vite.config.ts
└── tsconfig.json
```

### 2.3 Local Tooling and Build Workflow

- Prism is a monorepo: `backend/` and `frontend/` are root-owned directories that share the root launcher, release helper, and CI wiring.
- Root local orchestration lives in `start.sh`: it loads `.env`, starts PostgreSQL from `backend/docker-compose.yml`, syncs the backend with `uv sync --locked --python "$BACKEND_PYTHON_BIN"`, and runs `prism-backend` via `uv run --no-sync --python "$BACKEND_PYTHON_BIN" ...` on port `18000`.
- `./start.sh full` also launches the frontend on `15173` with `VITE_API_BASE=http://localhost:18000`; local dev does not rely on a Vite proxy.
- `backend/pyproject.toml` is the backend dependency declaration source of truth, while `backend/uv.lock` pins the resolved environment used by local sync, tests, and Docker builds.
- `backend/Dockerfile` copies `uv` from `ghcr.io/astral-sh/uv:0.9.8`, performs two locked runtime-only sync passes into `/app/.venv`, and runs `prism-backend` from that environment.
- `.github/workflows/docker-images.yml` builds Docker images only (no backend pytest or frontend lint/typecheck jobs) and currently targets `linux/arm64`.

## 3. Request Flow

### 3.1 Proxy Request (Non-Streaming, Native Model)

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Router captures active profile snapshot at request start
  -> Gateway assigns one Prism `ingress_request_id` for the incoming runtime request
  -> Request setup resolves the native model, attached adaptive routing policy, and current candidate set in active profile scope
  -> Planner ranks candidates from live runtime state, admission counters, and fresh monitoring signals
  -> Executor claims the primary attempt lease and forwards the request to the selected endpoint
  -> Upstream responds with JSON
  -> Gateway returns JSON to client, releases any non-stream lease, persists one `request_logs` row for the attempt, and feeds the outcome back into runtime routing state
```

### 3.2 Proxy Request (Proxy Model With Ordered Targets)

```
Client -> POST /v1/messages {model: "claude-sonnet-4-5"}
  -> Router captures active profile snapshot
  -> LoadBalancer looks up model config in active profile scope
  -> Model is proxy -> evaluate ordered proxy_targets within same profile
  -> Select the first enabled native target whose own native attempt plan is non-empty
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
  -> Planner resolves the live candidate set and executor claims a streaming lease within the request deadline
  -> ProxyService opens streaming connection to the selected upstream endpoint
  -> SSE chunks piped directly to client via StreamingResponse
  -> Streaming heartbeats keep the lease fresh while the stream is open
  -> On upstream error: release the stream lease, classify the failure, and continue only if policy deadline and hedge rules still allow another attempt
  -> On stream finalization or cancellation: release the stream lease, persist the per-attempt request log, and record monitoring-aware runtime feedback
```

### 3.4 Vendor and api_family Routing

| Vendor                | Proxy Path                                    | Upstream Path                                      | Auth Header                                          |
| --------------------- | --------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                | `POST /v1/chat/completions`                   | `{base_url}/v1/chat/completions`                   | `Authorization: Bearer {key}`                        |
| Anthropic             | `POST /v1/messages`                           | `{base_url}/v1/messages`                           | `x-api-key: {key}` + `anthropic-version: 2023-06-01` |
| Gemini                | `POST /v1beta/models/{model}:generateContent` / `POST /v1beta/models/{model}:streamGenerateContent` | `{base_url}/v1beta/models/{model}:generateContent` / `{base_url}/v1beta/models/{model}:streamGenerateContent` | `Authorization: Bearer {key}`                        |

Note: Gemini requests use native `/v1beta/models/{model}:...` paths only. When a Gemini proxy model resolves to a different native model ID, the proxy rewrites the model ID segment in the URL path to the resolved native target model ID before forwarding upstream.
For Gemini, the `:streamGenerateContent` path is authoritative for stream classification even when the request body omits `stream: true`.

Vendor rows are global publisher metadata. Models may keep `vendor_id = null` and `vendor = null`, while runtime compatibility and redirect checks still use the model's required `api_family`, not the vendor row. The frontend owns vendor icon rendering through a locally vendored registry sourced from pinned `cc-switch` presets, and it falls back to a monogram or placeholder only at render time when icon data or vendor metadata is missing or unknown. The Models page still renders each row's `api_family` metadata even when vendor identity is absent.

### 3.5 Management API Profile Scoping
- Profile-scoped management routes use explicit `X-Profile-Id` and effective-profile resolution.
- Profile-scoped config bundle routes now live under `/api/config/profile/*`.
- Global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/config/vendors/*`, `/api/auth/*`, `/api/realtime/*`, and the auth/email/proxy-key settings routes under `/api/settings/auth*`.
- Runtime proxy routes (`/v1/*`, `/v1beta/*`) always use active profile and ignore override headers.
- Selected profile (UI management context) and active profile (runtime routing context) are intentionally distinct states.

The protected frontend shell now boots profile state from `GET /api/profiles/bootstrap`, derives sidebar destinations and breadcrumbs from the route metadata registry in `frontend/src/components/layout/app-layout/navigationProfileConfig.ts`, and persists only the desktop sidebar collapse preference in localStorage. Mobile drawer state remains transient browser UI state.

The Settings shell mirrors that split: the Profile tab keeps backup, billing and currency, timezone, audit and privacy, and retention flows scoped to the selected profile, while the Global tab owns instance-wide authentication and the shared vendor catalog.

The v2 config workflow also mirrors that ownership split:
- profile export/import uses `bundle_kind = profile_config` and is authoritative only for profile-scoped rows
- vendor catalog export/import uses `bundle_kind = vendor_catalog` and is authoritative only for shared vendor metadata
- profile bundles never export plaintext endpoint API keys; endpoint secrets move through an encrypted `secret_payload`
- endpoints without upstream credentials export `api_key_secret_ref = null` and do not create a bundle secret entry
- profile import resolves vendors by `vendor_key` when present, keeps vendorless models vendorless when `vendor_key` is null, reuses existing global vendors, and never mutates existing global vendor metadata from profile-bundle hint drift

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

### 3.7 Realtime Dashboard Updates

```
Dashboard page -> WebSocket connect /api/realtime/ws
  -> If auth enabled: access-token cookie is validated in `routers/realtime.py`
  -> Client sends {type: "subscribe", profile_id, channel: "dashboard"}
  -> Connection manager stores room membership keyed by (profile_id, channel)

Proxy request completes
  -> `services/stats/logging.py` commits `request_logs` row
  -> build_dashboard_update_message() gathers:
     - request_log
     - stats_summary_24h
     - api_family_summary_24h
     - spending_summary_30d
     - throughput_24h
     - routing_route_24h
  -> Broadcast {type: "dashboard.update", ...payload} to dashboard subscribers for that profile
  -> `frontend/src/pages/dashboard/useDashboardRealtime.ts` merges the payload into dashboard state
  -> On reconnect or manual refresh, frontend reconciles via REST bootstrap calls
```

## 4. Routing Strategies and Monitoring

### 4.1 Routing policy contract

- Native models still attach one profile-scoped loadbalance strategy, but strategy rows now use a top-level family discriminator: `strategy_type = legacy | adaptive`.
- All strategies now carry a shared top-level `timeout_policy` with `attempt_open_timeout_ms`, `buffered_total_timeout_ms`, `stream_precommit_timeout_ms`, and optional `stream_hard_cap_timeout_ms`.
- `legacy` strategies carry `legacy_strategy_type` (`single`, `fill-first`, or `round-robin`) plus `auto_recovery`. `adaptive` strategies carry `routing_policy` with `routing_objective`, `hedge`, `circuit_breaker`, and `admission` branches.
- Startup seeds the default profile with two editable preset strategies: `Default legacy routing` and `Default adaptive routing`.
- Backend fallback settings such as `FAILOVER_COOLDOWN_SECONDS`, `FAILOVER_FAILURE_THRESHOLD`, `FAILOVER_BACKOFF_MULTIPLIER`, `FAILOVER_MAX_COOLDOWN_SECONDS`, and `FAILOVER_JITTER_RATIO` still shape the seeded circuit-breaker defaults.

### 4.2 Runtime execution pipeline

1. Request setup resolves the active-profile model, attached strategy, and one immutable effective strategy snapshot for the request.
2. Planner and runtime-state helpers read `routing_connection_runtime_state` to build the current candidate set from circuit state, admission counters, live latency, and fresh monitoring signals.
3. Executor claims per-attempt leases, applies `buffered_total_timeout_ms` for buffered requests or `stream_precommit_timeout_ms` until the first streaming chunk, and may launch one hedge only before any client-visible bytes are committed.
4. Passive request outcomes and synthetic probe outcomes both feed back into the same runtime state, while durable transition history stays in `loadbalance_events`.

If all eligible candidates are unavailable inside the current policy window, the gateway returns `503` with routing-availability detail.

### 4.3 Monitoring producers and scheduler ownership

- Manual connection checks and `/api/monitoring/connections/{connection_id}/probe` share the same api-family-aware probe builders and runner.
- FastAPI lifespan starts `MonitoringScheduler` only after bootstrap, shared HTTP client creation, and background-task startup, then stops it during shutdown before closing the client.
- Probe cadence lives in profile-scoped `user_settings.monitoring_probe_interval_seconds`, is clamped on the backend (`30..3600`, default `300`), and is never owned by frontend polling logic.
- Durable probe history lands in `monitoring_connection_probe_results`; fresh fused routing signals and leases live in UNLOGGED `routing_connection_runtime_state` and `routing_connection_runtime_leases`.

## 5. Model Proxy Routing

### 5.1 Concept

Proxy models are ordered routing records that choose one native target model per request. A proxy request first resolves one same-`api_family` native target from `proxy_targets`, then that chosen target model's attached legacy or adaptive strategy runs unchanged.

### 5.2 Rules

- Only same-`api_family` proxying
- Targets must be native models (no chained proxy models)
- Proxy models have no connections of their own
- Proxy models do not attach a strategy of their own; the resolved native model's attached strategy applies.
- Model IDs are unique within a profile (same model ID may exist in other profiles)
- Proxy targets are ordered contiguously from `0..n-1`; v1 routing is first-available target selection only.
- Once one target model is selected for a request, retries stay inside that target model's native connection plan; there is no cross-target retry in the same request.
- The gateway may normalize proxy request payloads before forwarding (for example: requested proxy model ID rewritten to the resolved native target model ID for upstream compatibility).

Model contracts require `api_family`; `vendor_id` is optional metadata. Vendor CRUD remains global, while proxy compatibility is checked against `api_family` only.

### 5.3 Resolution

```
resolve_model(profile_id, model_id):
  config = lookup(profile_id, model_id)
  if config.model_type == "proxy":
    for target in ordered_proxy_targets(config):
      candidate = lookup(profile_id, target.target_model_id)
      if candidate is native and candidate has attempt plan:
        return candidate
    return not_found
  return config
```

## 6. Connection Health Detection

### 6.1 Concept

Manual health checks and backend-owned monitoring probes share one lightweight probe runner so connection verification, scheduled probes, and manual re-probes stay on the same wire contract.

### 6.2 Health Probes (API-Family-Specific)

Health checks and monitoring probes send api-family-specific lightweight requests using the connection's configured model ID and a simple prompt. This validates full-chain URL routing, authentication, and model availability using the same URL-building logic as the proxy engine.

- **OpenAI**: `POST {base_url}/v1/responses` or `POST {base_url}/v1/chat/completions` based on the connection's persisted `openai_probe_endpoint_variant` (`responses` default).
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

Full HTTP request/response recording for proxied requests, toggled per vendor when vendor metadata exists. Vendorless models do not synthesize audit defaults from `api_family`. Sensitive data in headers (API keys, auth tokens) is redacted before storage.
Audit rows are written per upstream attempt, including failover attempts.

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
           -> response_body = NULL (streaming bodies are never stored)
           -> INSERT into audit_logs (separate AsyncSessionLocal)
```

### 8.4 Non-Interference Guarantees

- Audit INSERT runs in try/except — failures logged to console, never propagated
- Streaming audit uses its own DB session (request-scoped session is closed)
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
- Response tab: status, headers, body (pretty-printed JSON, or "not recorded" notice for streaming)
- Connection identity fields (`connection_id`, `endpoint_base_url`, `endpoint_description`) are displayed in the summary strip

### 8.8 Conditional Decompression (Performance Optimization)

**Background:** httpx automatically decompresses gzip/deflate/brotli responses by default. When body auditing is disabled, the proxy doesn't need the decompressed body content, so requesting compressed responses wastes CPU cycles.

**Implementation:**

1. **Compression Request Control:**
   - When `audit_enabled=True AND audit_capture_bodies=True`: Allow httpx to request compressed responses (default behavior)
   - When body auditing is disabled: Send `Accept-Encoding: identity` to request uncompressed responses
   - Decision made via `should_request_compressed_response(audit_enabled, audit_capture_bodies)` helper

2. **Header Filtering:**
   - When compression was requested: Strip `content-encoding` and `content-length` headers (stale after httpx decompression)
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

### 9.2 Deletion Flow

```
User → Settings Page → "Data Management" section
  → Selects data type (Request Logs, Audit Logs, or Loadbalance Events)
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

Same flow applies to audit logs and loadbalance events via their matching delete endpoints.

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

- **Operator Authentication**: Optional cookie-backed authentication for management APIs (`/api/*`). Supports username/password and WebAuthn (passkeys).
- **Proxy API Keys**: Optional API key enforcement for runtime proxy traffic (`/v1/*`, `/v1beta/*`). Keys are issued and managed through the dashboard.
- **Auth Bifurcation**: Management auth (session cookies) and runtime auth (proxy API keys) are separate enforcement paths.
- **Data at Rest**: API keys and secrets are stored in PostgreSQL. Endpoint secrets are encrypted at rest.
- **CORS**: Allowed origins come from `CORS_ALLOWED_ORIGINS`; local defaults target the Vite dev server.
- **Network**: No TLS termination; run behind a reverse proxy for HTTPS. Restricted to trusted local/LAN access.

## 13. Supported Runtime API Families

The runtime plane exclusively supports three fixed API families:

- **OpenAI** (`openai`) — GPT-style request and response contracts
- **Anthropic** (`anthropic`) — Claude-style request and response contracts
- **Gemini** (`gemini`) — Gemini-native `/v1beta/models/*` contracts

The vendor catalog is separate and global. Models always carry required `api_family`, while `vendor_id` remains optional metadata, so operators may create additional vendor metadata rows such as `OpenRouter` without changing runtime compatibility. The Global settings tab exposes vendor create/edit/delete flows, and deleting a vendor clears live model vendor metadata instead of blocking the delete.
 clears live model vendor metadata instead of blocking the delete.
