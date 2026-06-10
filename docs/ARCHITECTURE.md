# Architecture Document: Prism

## 1. System Overview

```
┌─────────────┐     ┌──────────────────────────────────────────────┐     ┌──────────────┐
│   Client    │     │                    Prism                     │     │   Providers  │
│             │     │  ┌────────────┐  ┌──────────┐               │     │              │
│ Port 5173*  │◀────│  │ Management │  │  Proxy   │          │◀────│  OpenAI API  │
│             │     │  │   APIs     │  │  Engine  │          │     │  Anthropic   │
│             │     │  └─────┬──────┘  └────┬─────┘          │     │  Gemini API  │
└─────────────┘     │        │              │                │     └──────────────┘
                    │  ┌─────▼──────────────▼─────┐          │
                    │  │    PostgreSQL Database    │          │
                    │  │ (profiles, models,        │          │
                    │  │  endpoints, connections,  │          │
                    │  │  settings, request_logs,  │          │
                    │  │  audit_logs, sidecars)    │          │
                    │  └───────────────────────────┘          │
                     │           Configured Port*              │
                    └──────────────────────────────────────────┘
```

*Local `./start.sh` keeps frontend `5173` and PostgreSQL `15432` fixed, and follows the selected bootstrap file's backend port. Freshly seeded bootstrap files default that backend port to `8000`. Standalone frontend containers commonly expose `3000`.

## 2. Component Architecture

### 2.1 Backend (Go runtime)

```
backend/
├── cmd/prism-backend/          # Go process entrypoint
├── internal/
│   ├── httpapi/
│   │   ├── management/         # /api/* management handlers, including sidecars
│   │   ├── runtime/            # operation-registered /v1 and /v1beta proxy handlers
│   │   └── realtime/           # WebSocket room management and publishing
│   ├── platform/
│   │   ├── config/             # startup bootstrap JSON and runtime settings
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
├── migrations/                 # Fresh-install SQL baseline applied at startup
├── testdata/                   # bundle, request, bootstrap, and realtime fixtures
├── tests/                      # Go contract, integration, and runtime regressions
├── Dockerfile                  # live Go backend image build
├── docker-compose.yml          # local PostgreSQL helper on host port 15432
└── VERSION                     # backend version surface
```

### 2.2 Frontend (React + Vite)

```
frontend/
├── src/
│   ├── main.tsx                # Entry point
│   ├── App.tsx                 # BrowserRouter + public auth routes + protected Page shell routes
│   ├── context/
│   │   ├── ProfileContext.tsx  # Selected profile vs active profile state bootstrapped from /api/profiles/bootstrap
│   │   └── AuthContext.tsx     # Operator auth bootstrap, refresh, and session state
│   ├── lib/
│   │   ├── api.ts              # Typed API client + /api scoped X-Profile-Id injection
│   │   ├── types.ts            # TypeScript contracts aligned with backend schemas
│   │   ├── costing.ts          # Micros and currency formatting helpers
│   │   ├── reportingCurrency.ts # Shared reporting-currency cache and normalization
│   │   ├── timezone.ts         # Shared timezone formatting helpers
│   │   └── configImportValidation.ts # Config import validation for the current configuration format
│   ├── hooks/
│   │   ├── usePolling.ts       # Shared polling helper
│   │   ├── useRealtimeData.ts  # WebSocket-backed live refresh helper
│   │   └── useTimezone.ts      # Shared timezone formatting helper
│   ├── components/
│   │   ├── layout/page.tsx     # Protected shell wrapper with sidebar provider and Outlet
│   │   ├── layout/app-layout/  # Sidebar, header, profile switcher, nav metadata, and version label
│   │   ├── loadbalance/        # Shared loadbalance renderers
│   │   ├── statistics/         # Shared statistics renderers
│   │   └── ui/                 # shadcn/ui components
│   └── pages/
│       ├── DashboardPage.tsx
│       ├── ModelsPage.tsx
│       ├── ModelDetailPage.tsx     # Model detail shell + loadbalance events tab
│       ├── EndpointsPage.tsx
│       ├── dashboard/DashboardPage.tsx # Dashboard shell with analytics tab and shared statistics content
│       ├── RequestLogsPage.tsx     # Request-log investigation with lazy audit lookup
│       ├── ProxyApiKeysPage.tsx
│       ├── SidecarsPage.tsx        # Global CLIProxyAPI sidecar control plane
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
- Root local orchestration lives in `start.sh`: it loads the root `.env`, starts PostgreSQL from `backend/docker-compose.yml`, validates that the selected bootstrap config keeps the launcher's local host and database contract, and launches the Go backend service on the bootstrap file's configured port.
- `./start.sh full` launches the frontend on `5173`, unsets `VITE_API_BASE`, and enables a launcher-local Vite proxy via `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at that effective backend port so browser traffic stays same-origin.
- Canonical startup config lives in a plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; backend-native fresh seeds default the database URL to `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, while `start.sh` sets `DATABASE_URL` to the local launcher PostgreSQL DSN on host port `15432` before seeding.
- Plaintext bootstrap startup reads that bootstrap file directly through `PRISM_CONFIG_PATH`; encrypted bootstrap files must be replaced before boot. Missing files are seeded once, and the entrypoint has a narrow repair path for stale files rejected only because they still contain retired `docsEnabled`. Other invalid legacy shapes fail validation. Existing valid files are preserved until an operator resets manually by stopping Prism, removing or relocating the file, and restarting.
- The backend image runs as `prism:prism`, UID/GID `1000:1000`. Container deployments that bind mount `/app/config` or any other `PRISM_CONFIG_PATH` parent must make that host directory writable by UID/GID `1000:1000`; new and existing root-owned mounts should be prepared once with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.
- The Startup tab and `PUT /api/config/bootstrap` are the only supported hot publication paths for file-backed startup edits. External edits to `config.json` are not watched automatically.
- Operational telemetry is startup-JSON-owned: the top-level `telemetry` section configures OTLP endpoint, protocol, compression, timeout, auth, TLS, metrics, and traces. Prism does not use long-lived `OTEL_*` environment variables as the steady-state config source.
- The primary ops path is OTLP to an OpenTelemetry Collector or Grafana Alloy, with Prometheus/Grafana/Tempo or another backend attached from that collector layer. The backend does not mount a local `/metrics` scrape endpoint.
- Profile backup/restore, vendor catalog export/import, request-history APIs, and other settings-page state flows remain PostgreSQL-backed product state instead of bootstrap or OTLP ownership.
- The current implementation keeps the split-bundle contract canonical: `profile_config` bundles use `version: 3`, `vendor_catalog` bundles use `version: 1`, and no older profile-bundle narrative survives.
- `backend/Dockerfile` is the live Go backend image build path and copies the backend binary, version surface, and `migrations/` into the image.
- `.github/workflows/docker-images.yml` builds Docker images only (no backend pytest or frontend lint/typecheck jobs) and currently targets `linux/arm64`.

### 2.4 Priority Enforcement And Operator-Facing Failure Modes

Prism assigns trusted backend priority metadata before work touches shared resources. Runtime proxy traffic is `proxy`, management routes are `management` with an explicit `M1`, `M2`, or `M3` tier, and scheduler-owned workers are `background` with a declared subclass, budget, coalescing policy, retry policy, and drain policy. Priority-sensitive backend changes should stay covered by the standard priority regression tests, including `go test ./tests/priority/...`.

PostgreSQL capacity is split into finite named lanes: `runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `realtime`, `cache_refresh`, and `background_jobs`. Operators should treat lane saturation by owner: proxy execution pressure is separate from management UI pressure, telemetry drain pressure, lossy feedback drain pressure, realtime fanout, cache refresh, and generic background jobs. Background or management saturation must not consume protected proxy capacity. Lane metrics are emitted through OTLP DB-pool instruments, not through a backend-local Prometheus text endpoint.

Management overload is reported as typed admission failure with retry metadata. Lower-priority M3 reporting and maintenance routes shed before M2 and M1 management work, and proxy traffic remains isolated from management/background saturation. When overload appears, retry after the advertised delay rather than increasing client concurrency.

Scheduler lag means background workers are queued, coalesced, delayed, retried, or dropped according to their worker policy. Lag can delay dashboard fanout, telemetry materialization, email delivery, management side-effect dispatch, cache warming, and proxy-key usage flushing, but it must not make request-path handlers borrow direct goroutines, direct DB handles, or unmanaged timers.

Durable outboxes expose failure as queued, retry, sent/succeeded, dead-letter, or permanent-failure state depending on the store. Email provider failures retry and eventually dead-letter without exposing OTPs or SMTP credentials. Management side-effect dispatch failures retry or become visibly permanent failures without rolling back the already committed primary management mutation.

Runtime telemetry and runtime feedback have different loss semantics. Accepted runtime activity intents are required-durable background work until the telemetry outbox transaction commits, terminal validation fails, or shutdown prevents completion. Runtime feedback is intentionally lossy under pressure; queue-full, invalid, closed, or store-failure cases drop feedback with accounting and never block proxy responses.

Audit and statistics reads are bounded. Raw audit lists require backend-enforced time windows and keyset cursors. The overview dashboard reads one canonical `/api/stats/dashboard` aggregate snapshot with backend-computed Routing Health Map data, while the internal rollup helper remains scoped to background rollup tests. Broad deletes run as durable management jobs.

Runtime cache correctness is generation-based. Management mutations advance durable runtime-cache generations in the same transaction as the primary state change, runtime reads validate generation vectors and refresh or fail closed when stale, and post-response cache warming is non-authoritative. Cache generation lag may delay warm snapshots, but auth-sensitive runtime reads reject stale or unverifiable snapshots instead of accepting old state.

## 3. Request Flow

Prism is proxy-first. It forwards only the provider-native operations registered in the runtime operation catalog, and it is not a full OpenAI, Anthropic, or Gemini API clone.

The operation registry is the ingress contract for the runtime plane. Each supported operation declares an exact HTTP method, path template, API family, model-binding source, streaming classification, canonical operation name, and provider adapter. The current canonical operation names are `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, `openai.images.edits`, `anthropic.messages`, `anthropic.count_tokens`, `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`. Requests that do not match that registry are rejected before body reads, planning, provider transport, telemetry, audit, feedback, or durable side effects.

After registry resolution, every runtime operation enters the same execution core. The shared runtime and gateway layers capture the active profile snapshot, resolve ordered access targets to a final model-private connection or final model target, apply the attached explicit Ban Policy strategy, claim leases, record retained request history through durable seams, and emit bounded OTLP metrics/traces through startup-owned providers when enabled. Provider adapters own provider-specific parsing, upstream request building, response adaptation, streaming terminal classification, usage extraction, token counting or estimation, media handling, overflow classification, and OpenAI Chat/Responses conversion. The shared runtime/gateway owns operation routing, admission, accounting, telemetry, audit persistence, pricing, feedback, and side-effect handoff.

OpenAI Chat Completions and Responses can translate only across explicit sibling-operation terminal targets. Planning remains ingress-led: estimation, generation-parameter extraction, and `operation_name` come from the client-visible operation. A selected OpenAI connection's derived `openai_upstream_operation` decides whether the attempt is native with `operation_translation_mode = "none"` or translated with `openai_responses_to_chat_completions` or `openai_chat_completions_to_responses`. Native-compatible terminal attempts remain preferred before translated attempts. `runtime.routing.openaiTerminalTranslationMode = "safe_only"` admits only adapter-approved safe OpenAI text sibling translations between Chat Completions and Responses, while `off` leaves translated-only terminal sets unavailable through normal no-eligible-target planning. Unsupported shapes are not universally routable, so tools, multimodal or file inputs, stateful Responses features, structured-output mismatches, streaming event-shape mismatches, and other adapter-rejected conversions stay blocked by adapter capability checks. Translation rewrites supported request shapes after target selection, rewrites non-stream or stream responses back to ingress shape for the client, preserves canonical usage from upstream payloads or stream terminal events, and drops unsafe entity headers from translated responses.

Runtime observability stores canonical disjoint token components. Base input, cache-read input, cache-creation input, base output, and reasoning output are separate dimensions, while provider totals remain authoritative when supplied. Pricing uses five concrete pricing strings from the attached template snapshot, and explicit `"0"` component prices mean configured free pricing instead of a missing-price condition.

`cheapest_eligible_context` uses hard-fit legality and optional preferred bands. `max_context_utilization` caps whether a terminal target can legally receive the request. `preferred_context_utilization_threshold` is persisted on model defaults and owner-scoped terminal-target overrides; `null` means no preferred band, and a supplied value must be less than or equal to `max_context_utilization`. Preferred candidates sort before discretionary candidates, while ineligible candidates are skipped. Within a band, ranking stays priced first, then estimated blended request cost, access-target position, terminal target ID, and target ID.

### 3.1 Runtime Request With Private Connection Target

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Operation registry resolves `openai.chat_completions` and its OpenAI provider adapter
  -> Shared core captures active profile snapshot at request start
  -> Gateway assigns one Prism `ingress_request_id` for the incoming runtime request
  -> Request setup resolves the requested model and its ordered access targets in active profile scope
  -> Planner reaches the model's Terminal Target, applies the attached explicit Ban Policy strategy, and checks admission counters plus retry-window state
  -> Executor claims the primary attempt lease and forwards the request to the selected endpoint
  -> Upstream responds with JSON
  -> Gateway returns JSON to client, releases any non-stream lease, persists one `request_logs` row for the attempt, and feeds the outcome back into runtime routing state
```

### 3.2 Runtime Request Through Model Access Targets

```
Client -> POST /v1/messages {model: "claude-sonnet-4-5"}
  -> Operation registry resolves `anthropic.messages` and its Anthropic provider adapter
  -> Shared core captures active profile snapshot
  -> Resolver loads ordered same-profile, same-api-family access targets
  -> Model targets can chain to another model; compatibility connection targets are terminal
  -> Executor plans attempts against Terminal Targets
  -> Upstream responds; request log keeps model_id as the requested model and resolved_target_model_id as the final target model for the attempt
  -> Gateway returns response to client
```

### 3.3 Runtime Request (Streaming)

```
Client -> POST /v1/chat/completions {model: "gpt-4o", stream: true}
  -> Operation registry resolves `openai.chat_completions`; the OpenAI adapter marks stream intent
  -> Shared core captures active profile snapshot
  -> Gateway assigns one Prism `ingress_request_id`
  -> Access-target resolution, route planning, adapter request build, and admission finish before downstream commit
  -> Executor claims a streaming lease before opening the upstream stream
  -> ProxyService opens streaming connection to the selected upstream endpoint
  -> SSE chunks stream back to the client after provider-adapter stream classification allows the operation
  -> Internal buffering is automatic for rewrite, replay, or hook-safety cases before downstream commit
  -> First downstream byte/event commits the stream boundary
  -> After commit: no retry, redirect, context-overflow fallback, or hedge replay can start
  -> On stream finalization or cancellation: release the stream lease, persist the per-attempt request log, and record runtime feedback
```

### 3.4 Vendor and api_family Routing

| API family            | Canonical operation names                       | Supported Prism operation paths                    | Upstream path                                      | Auth header                                          |
| --------------------- | ----------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                | `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, `openai.images.edits` | `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/responses/input_tokens`, `POST /v1/responses/compact`, `POST /v1/images/generations`, `POST /v1/images/edits` | Same path under `{base_url}` | `Authorization: Bearer {key}`                        |
| Anthropic             | `anthropic.messages`, `anthropic.count_tokens`  | `POST /v1/messages`, `POST /v1/messages/count_tokens` | Same path under `{base_url}` | `x-api-key` set to `{key}` plus `anthropic-version` set to `2023-06-01` |
| Gemini                | `gemini.generate_content`, `gemini.stream_generate_content`, `gemini.count_tokens` | `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent`, `POST /v1beta/models/{model}:countTokens` | Same path under `{base_url}` | `Authorization: Bearer {key}`                        |

OpenAI runtime support is limited to the registered chat, Responses generation, Responses input-token, Responses compact, and image operations listed above. Stored Responses object lifecycle APIs, including retrieve, list, delete, and cancel routes, are outside Prism's supported contract. `openai.images.generations` and `openai.images.edits` are media operations with copy-only token usage semantics, not generic OpenAI passthrough routes.

Note: Gemini requests use `/v1beta/models/{model}:...` paths only. When access-target resolution reaches a different final Gemini model ID, Prism rewrites the model ID segment in the URL path before forwarding upstream.
For Gemini, `gemini.stream_generate_content` and the `:streamGenerateContent` path are authoritative for stream classification even when the request body omits `stream: true`; `gemini.generate_content` remains non-stream generate content, and `gemini.count_tokens` remains the token-count operation.

Runtime upstream requests capture an immutable bootstrap runtime snapshot at request start. The snapshot includes an HTTP client built from startup bootstrap transport settings. Fresh seeds use transport `100/16/16/300s/90s/0s/10s/1s` and side-effect attempt timeout `10s`. Runtime buffering is automatic and not user-configurable. The raw `runtime.transport.requestTimeout` Go duration is applied as `http.Client.Timeout`, which makes it the whole-request timeout for outbound provider calls, and it is hot-applicable for new requests. A missing request timeout fails startup validation by design. Raw `runtime.sideEffects.attemptTimeout` is a separate per-attempt background side-effect enqueue budget and is restart-required rather than hot-applied.

Hot bootstrap projection builds a new aggregate snapshot, validates it, then atomically publishes it for future work. CORS origin checks, auth TTL and cookie metadata, mail delivery settings, runtime transport, and M2/M3 management admission limits are hot-apply boundaries. New requests and new email sends read the current snapshot; in-flight proxy requests keep the HTTP client they captured, and a retired runtime transport only has idle connections closed.

Restart-required boundaries are structural process resources: listener host and port, PostgreSQL URL and pool budgets, runtime side-effects attempt timeout, runtime secret encryption key, auth JWT signing key, and the state-transfer bundle key. Those values can be written through the bootstrap API, but they do not change the running process until Prism restarts.

Vendor rows are global publisher metadata. Models may keep `vendor_id = null` and `vendor = null`, while runtime compatibility and redirect checks still use the model's required `api_family`, not the vendor row. The frontend owns vendor icon rendering through a locally vendored registry sourced from pinned `cc-switch` presets, and it falls back to a monogram or placeholder only at render time when icon data or vendor metadata is missing or unknown. The Models page still renders each row's `api_family` metadata even when vendor identity is absent.

### 3.5 Management API Profile Scoping
- Prism keeps one route-class matrix:
  - Global management routes omit `X-Profile-Id`.
  - Profile-scoped management routes require `X-Profile-Id` and resolve against the selected profile.
  - Supported runtime operations under `/v1` and `/v1beta` ignore management overrides and always use the active profile.
- Profile-scoped config bundle routes live under `/api/config/profile/*`, and `POST /api/config/profile/import/preview` is also profile-scoped and requires `X-Profile-Id`.
- Global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/config/vendors/*`, `/api/auth/*`, `/api/realtime/*`, and the auth/email/proxy-key settings routes under `/api/settings/auth*`.
- Selected profile (UI management context) and active profile (runtime routing context) are intentionally distinct states.
- Scope-control errors return stable `code` values plus human-readable `detail` text.
- Supported runtime operations always use active profile and ignore override headers.

The protected frontend shell now boots profile state from `GET /api/profiles/bootstrap`, derives sidebar destinations and breadcrumbs from the route metadata registry in `frontend/src/components/layout/app-layout/navigationProfileConfig.ts`, and persists only the desktop sidebar collapse preference in localStorage. Mobile drawer state remains transient browser UI state.

The Settings shell mirrors that split: the Profile tab keeps backup, billing and currency, timezone, audit and privacy flows scoped to the selected profile, while the Global tab owns instance-wide authentication, the shared vendor catalog, and global log retention. Normal log retention applies across all profiles; list and detail APIs still filter by the selected profile.

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
  -> Runtime telemetry persists the `request_logs` row
  -> Realtime refreshes or reads the shared dashboard aggregate snapshot for the profile
  -> Dashboard publisher gathers request_log plus the canonical DashboardSnapshot
  -> Broadcast {type: "dashboard.update", request_log, snapshot} to dashboard subscribers for that profile
  -> REST bootstrap reads the same shape from GET /api/stats/dashboard
  -> Overview and backend-owned topology graph state reconcile against that shared snapshot shape

Dashboard analytics tab -> WebSocket connect /api/realtime/ws
  -> Client sends {type: "subscribe", profile_id, channel: "analytics", preset}
  -> Realtime manager stores analytics room membership keyed by profile, channel, and preset scope
  -> Service sends an initial full `analytics.snapshot` for that {profile_id,preset}
  -> Manual refresh sends {type: "refresh", profile_id, channel: "analytics", preset}
  -> Refresh returns a fresh full `analytics.snapshot` on the socket
  -> Analytics snapshots include the usage snapshot plus endpoint model statistics keyed by endpoint ID string
  -> The frontend treats each `analytics.snapshot` as a full replacement for that scoped analytics view
```

The realtime API has two supported channels. `dashboard.update` is the overview dashboard signal and carries `request_log` plus the same `DashboardSnapshot` returned by `GET /api/stats/dashboard`; it does not carry analytics page replacement data. `analytics.snapshot` is scoped by `{profile_id,preset}` inside the WebSocket message payload and powers the Analytics tab without requiring UI calls to `/api/stats/*`. The REST stats endpoints, including `GET /api/stats/dashboard`, request-history detail/list routes, spending, throughput, model metrics, and `GET /api/stats/usage-snapshot`, remain product-facing retained-history APIs; OTLP/Prometheus operations telemetry does not replace them.

## 4. Routing Strategies and Runtime Health Signals

### 4.1 Routing policy contract

- Models attach one profile-scoped explicit loadbalance strategy.
- Strategies carry the routing family field `legacy_strategy_type` (`single`, `fill-first`, `round-robin`, or `cheapest_eligible_context`).
- `cheapest_eligible_context` is labelled in the UI as `Cheapest target that fits context` and keeps the existing strategy shape plus the new policy value.
- Strategies also carry explicit Ban Policy fields for failure status codes, retry delay, backoff, jitter, retry-window limits, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, ban mode, and ban duration.
- Retry-cycle exhaustion is inclusive at `cycle_retry_attempts >= cycle_retry_attempt_limit`.
- Ban creation is inclusive at `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; the runtime never derives this threshold from the cycle limit.
- `ban_mode` accepts `off`, `temporary`, and `until_reset`. The `until_reset` mode keeps a terminal target's current connection row banned until the current-state reset endpoint clears it.
- The selected profile's loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default single routing`, `Default fill-first routing`, and `Default round-robin routing` for that profile.
- Upstream request timing is controlled by shared backend timeout settings, not by per-strategy timeout documents.

### 4.2 Runtime execution pipeline

1. The operation registry resolves the exact runtime operation and hook collection before the request body is consumed.
2. Request setup resolves the active-profile model by exact `planningSnapshot.ModelsByID` lookup, ordered access targets, attached strategy, and one immutable effective strategy snapshot for the request.
3. OpenAI Chat Completions and Responses requests that use `cheapest_eligible_context` run local preflight context estimation before terminal-target choice. The deterministic methods are `openai_chat_heuristic_v1` and `openai_responses_heuristic_v1`. When estimation is unavailable for these two operations, Prism now passes the request through the normal resolved target path instead of returning local `400 context_estimation_unavailable`; hard no-fit `413 context_window_exceeded` still requires a successful estimate.
4. Planner and runtime-state helpers read `routing_connection_runtime_state` to build the current candidate set from admission counters and Ban Policy retry-window state.
5. For `cheapest_eligible_context`, the planner filters terminal targets whose `estimated_total_context_tokens` exceed the target's usable context window, then ranks fitting candidates by estimated blended request cost, access-target position, then terminal target ID.
6. Release 1 exact OpenAI facade routing is a backend-first planner specialization on that same exact requested-model lookup. It activates only when the requested OpenAI model is `facade_enabled = true`, `facade_selection_policy = "weighted_eligible_context"`, and `facade_fallback_policy = "redistribute_ineligible_weight"`, evaluates same-family model targets only, redistributes weights across the eligible subset only, and returns one selected child plan.
7. The shared execution core claims per-attempt leases and uses shared upstream timeout behavior from the backend runtime before any client-visible bytes are committed.
8. Operation request, response, stream, and media hooks interpret provider-native payload details by canonical operation name. Token-count hooks are attached to `anthropic.count_tokens` and `gemini.count_tokens`, media hooks are attached to `openai.images.generations` and `openai.images.edits`, and the Gemini SSE hook is attached to `gemini.stream_generate_content`; passive outcomes feed back into connection-global runtime state while durable transition history persists model-policy snapshots and exposes them on event APIs as `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` when Ban Policy evaluation produced the event.

If all eligible candidates are unavailable inside the current retry window, the gateway returns `503` with routing-availability detail. If context fit is evaluated and no terminal target fits, the gateway returns HTTP `413` before provider transport with `error="context_window_exceeded"` and context-routing detail for skipped terminal targets. Exact facade routing adds no regex matching, capability-metadata expansion, frontend facade authoring, sibling-target provider-failure failover after child selection, or response-body model rewriting. Request logs and usage events keep the existing requested-model and resolved-target fields, while `context_routing.facade_selection` and matching `prism.runtime.facade_*` trace attributes carry the additive facade metadata.

Context overflow promotion is a CLIProxyAPI-specific replay path, not official OpenAI behavior and not a generic failover rule. The known upstream for this path is CLIProxyAPI, which can return different native and translated OpenAI-compatible error envelopes. Prism promotes only non-stream `openai.chat_completions` and `openai.responses` attempts, only before downstream commit, only once, and only to the source model's explicit `context_overflow_promotion_target_id`. The promoted model inherits the same planner behavior as ordinary terminal resolution: native-compatible attempts remain preferred, `safe_only` can admit only adapter-approved safe OpenAI text sibling translations, and `off` keeps translated-only promoted terminal sets unavailable. Promotion remains a one-shot explicit replay path, not a promotion-only fallback exception. The selected-child exact-facade restriction is preserved: after a facade selects one child, promotion can start only from that selected child and cannot reopen sibling facade targets.

Promotion classification is body-aware. Plain `429` never promotes; only body-confirmed overflow `429` can promote. Native non-stream paths may accept an OpenAI-style top-level `error` object or unambiguous flat CLIProxyAPI gateway JSON, while translated non-stream paths accept only top-level `error` objects and reject translated flat-gateway JSON for promotion in v1. Streaming promotion and strict-mode promotion are not implemented in v1. Final status, usage, pricing, and finalized usage-event ownership come from the final response returned to the client. Failed source attempts remain visible as attempt-level request-log and optional audit rows.

## 5. Unified Model Access

### 5.1 Concept

Models resolve through ordered access targets. Public target authoring points only to other same-profile, same-`api_family` models. Terminal Targets are the product-facing model-private endpoint bindings. They remain in `model_access_targets` as internal `connection` ownership and terminal routing edges backed by `connections` rows. Model targets can chain until a Terminal Target is reached, and the runtime records requested model, final target model, selected terminal target, endpoint, and context-routing metadata for observability. Release 1 facade routing remains exact-ID-only and backend-authored: the requested model still enters planning by exact `model_id`, not by regex or capability matching, and the facade layer selects one child model target before ordinary terminal-target execution continues.

### 5.2 Rules

- Access targets must stay in the same profile and same `api_family`.
- Compatibility connection targets are terminal and are presented as Terminal Targets in product-facing routing surfaces.
- Model targets can chain, but cycles and self-targets are rejected.
- Endpoints are reusable. Terminal Targets are created and managed from model detail through model-scoped connection routes while retaining `connections` and `connection_id` compatibility names.
- Every access target carries explicit ordering metadata.
- Model IDs are unique within a profile.
- The gateway may normalize provider request payloads before forwarding, for example rewriting the requested model ID to the final target model ID for upstream compatibility. Release 1 exact facades do not add response-body model rewriting on the client-facing way back out.

Model contracts require `api_family`; `vendor_id` is optional metadata. Vendor CRUD remains global, while runtime compatibility is checked against `api_family` only.

### 5.3 Resolution

```
resolve_access(profile_id, model_id):
  config = lookup(profile_id, model_id)
  for target in ordered_access_targets(config):
    if target is connection:
      return terminal_connection(target)
    if target is model:
      return resolve_access(profile_id, target.model_id)
  return no_eligible_target
```

### 5.4 Selected profile and active runtime separation

Selected-profile management APIs use `X-Profile-Id` to read and edit profile-scoped configuration. Runtime proxy traffic ignores that management header and always resolves through the active runtime profile snapshot. Changing the selected profile in the frontend changes management scope only; activating a profile is the separate operation that changes runtime routing.

### 5.5 Dashboard topology graph

`GET /api/stats/dashboard` and realtime `dashboard.update.snapshot` include a backend-owned `topology_graph` alongside the legacy `routing_health_map`. The graph is built from selected-profile configuration and recent telemetry in the backend, not reconstructed by the browser from management reads. Disabled models remain present as muted model nodes, inactive terminal targets remain present as muted target nodes, and endpoint nodes stay visible when referenced by configured terminal targets. During the additive compatibility wave, the backend keeps compatibility kinds (`connection`, `model_to_connection`, and `connection_to_endpoint`) and exposes product-facing terminal-target meaning through `product_kind`, with `connection_id` retained as the persisted compatibility identifier.

## 6. Terminal Target Health Detection

### 6.1 Concept

Manual health checks use one lightweight probe runner so Terminal Target verification stays on the same api-family-aware wire contract as the rest of the runtime stack.

### 6.2 Health Probes (API-Family-Specific)

Health checks send api-family-specific lightweight requests using the Terminal Target's configured model ID and a simple prompt. This validates full-chain URL routing, authentication, and model availability using the same URL-building logic as the proxy engine.

- **OpenAI**: `POST {base_url}/v1/responses` or `POST {base_url}/v1/chat/completions` based on the Terminal Target's persisted `openai_probe_endpoint_variant`; current variants are `responses_minimal` (default), `responses_reasoning_none`, `chat_completions_minimal`, and `chat_completions_reasoning_none`.
- **Anthropic**: `POST {base_url}/v1/messages` with `{"model":"{model_id}","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
- **Gemini**: `POST {base_url}/v1beta/models/{model}:generateContent` with minimal content payload and `maxOutputTokens: 1`.

### 6.3 Status Values

- `unknown` — Never checked (default)
- `healthy` — Last check succeeded (2xx or 429)
- `unhealthy` — Last check failed (401/403, connection error, timeout, other errors)

### 6.4 Terminal Target Success Rate Badge

The primary visual health indicator for Terminal Targets is the **success rate badge**, computed from `request_logs` data and stored with compatibility `connection_id` attribution.

- Success rate = `COUNT(2xx) / COUNT(*) * 100` per Terminal Target
- Badge colors: ≥98% green, 75-98% yellow, <75% red, N/A gray (no data)
- Displayed in the Terminal Targets list on the Model Detail page alongside the health tooltip state
- The manual health check still updates `health_status`/`health_detail` in the database and is shown in the tooltip

### 6.5 Model Health Aggregation

Model-level health is computed by aggregating Terminal Target success rates:

- Weighted average across all Terminal Targets: `SUM(success_count) / SUM(total_requests) * 100`
- Displayed on Dashboard and Models pages as a colored badge
- Same color thresholds as Terminal Target badges

### 6.4 Error Reporting

When a health check fails, the upstream error message is extracted from the response body and stored in `health_detail`. This provides actionable diagnostics (e.g., "HTTP 503: No available channel for model X" instead of just "HTTP 503"). The detail is shown in the frontend tooltip on hover.

### 6.5 URL Path Failsafe

To prevent the `/v1/v1` double-path bug (where endpoint `base_url` already contains `/v1` and the request path also starts with `/v1`):

1. **Runtime auto-correction**: `build_upstream_url()` detects repeated version segments (e.g., `/v1/v1`, `/v2/v2`) via regex and auto-corrects them, logging a warning.
2. **Input validation**: `validate_base_url()` rejects base URLs that already contain double version segments on endpoint create/update (HTTP 422).
3. **Normalization**: `normalize_base_url()` strips trailing slashes from base URLs on create/update to ensure consistent path joining.

## 7. Request Statistics

### 7.1 Concept

All proxy attempts are automatically logged as retained, product-facing request history for analytics, debugging, spending, and dashboard views. OTLP metrics and traces are the primary operations path, but they do not replace the PostgreSQL-backed request-history APIs.

### 7.2 Logging Flow

```
Client -> Operation registry -> Router / Planner -> Terminal target -> Endpoint -> Upstream
                                                         ↓
                                              Response received
                                                         ↓
                                              Return response to client

                              Background best-effort logging (async):
                                - Log request attempt to request_logs
                                - If audit_enabled: log attempt to audit_logs
```

### 7.3 Data Captured

- Profile ID attribution, requested model ID, final target model ID, api family, vendor snapshot, terminal-target compatibility ID, endpoint base URL, and endpoint description
- Prism `ingress_request_id`, per-request `attempt_number`, persisted ingress `operation_name`, additive `upstream_operation_name`, `operation_translation_mode`, `upstream_request_path`, and best-effort `upstream_correlation_id`
- HTTP status code, response time (ms)
- Token usage (input, output, total), extracted by upstream operation response or stream hooks before any client-facing response translation
- Context-routing metadata when preflight routing ran, including policy, selected terminal target, selected endpoint, selected preferred-context band, estimator method, estimated token totals, ranking method, skipped terminal-target reasons, and additive `context_overflow_promotion` detail when one-shot CLIProxyAPI overflow replay ran
- Stream flag, ingress request path, sanitized upstream request path, error details

Request-log semantics are per-attempt: one incoming runtime request can create multiple request-log rows when failover, retries, or one-shot overflow promotion occur. `ingress_request_id` groups those rows while `request_id` remains the unique identifier for one stored attempt row. Final usage ownership stays with the final returned response; failed source overflow attempts remain attempt-level observability and may have null usage.

### 7.4 Query Capabilities

- Filter by model, api family, status, time range
- Aggregated statistics with grouping by model/api family/endpoint
- Pagination for request log listing

These query APIs intentionally remain product-facing retained-history surfaces for the UI and operators. Prometheus/Grafana should consume Prism operations data from the configured OTLP Collector or Alloy path instead of scraping Prism for local metrics.

Runtime traces keep `prism.runtime.operation_name` as ingress-led. Additive trace attributes include sanitized `prism.runtime.upstream_operation_name`, `prism.runtime.operation_translation_mode`, `prism.runtime.upstream_request_path`, `prism.runtime.preferred_context_band`, and `prism.runtime.selected_terminal_target_id`. The upstream path attribute is normalized through the operation registry so path-bound model IDs do not leak into spans.

## 8. Request Audit Logging

### 8.1 Concept

Audit logging records the request-time provenance that was active when the request started. Vendorless models do not synthesize audit defaults from `api_family`; the request keeps the mode it started with, whether audit was disabled, metadata only, or full capture. Sensitive data in headers (API keys, auth tokens) is redacted before storage.
Audit rows are written per upstream attempt, including failover attempts, and metadata-only requests still create audit metadata even when bodies are not stored. Translated OpenAI attempts keep audit request and response bodies upstream-native; the rewritten client-facing request or response body is never substituted into audit storage.

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

**Testing:** Keep this behavior covered by current Go runtime/header regression tests under `backend/internal/httpapi/runtime/` or `backend/tests/runtime/`; the old Python smoke-defect regression tree is no longer part of this monorepo.

## 8A. CLIProxyAPI Sidecars

Sidecars are global management resources for coordinating CLIProxyAPI instances. Prism stores registration metadata plus optional normalized provider inventory for operator display. CLIProxyAPI remains the live authority for auth files and provider inventories.

```text
Frontend /sidecars
  -> api.sidecars.* typed client
  -> Backend /api/sidecars/* global management routes
  -> Sidecar service validates network policy and management auth
  -> CLIProxyAPI /v0/management/{auth-files,provider endpoints}
  -> Prism persists sidecar registrations and optional provider observations in sidecar_* tables
  -> Low-priority scheduler runs periodic provider sync
```

Sidecar control-plane routes omit `X-Profile-Id`. The browser never calls CLIProxyAPI directly; all management-password use, network policy enforcement, live auth-file reads, direct auth-file mutations, and provider observation redaction happen inside `backend/internal/httpapi/management/sidecars/`.

The retained sidecar surface covers instance CRUD, connection testing, manual provider sync, sync-status reads, live auth-files, optional provider inventory snapshots, read-only auth-file model discovery, and direct auth-file status or field mutations. Provider inventory stays a read-only supplement and never substitutes for live auth-file state.

The scheduler registers the bounded low-priority `sidecar_snapshot_sync` worker with queue limit 1, single concurrency, best-effort drain, and drop-new coalescing so sidecar background work cannot borrow protected proxy capacity.

## 9. Global Log Retention

### 9.1 Concept

Historical `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events` are partitioned by UTC day and managed by global log-retention jobs. Normal retention is instance-wide across all profiles. Selected-profile filtering still applies to profile-owned list and detail APIs, but `X-Profile-Id` does not scope retention settings or retention jobs.

The Settings Global tab owns `/api/settings/log-retention` and can store a day count per retained dataset. Operators can also create an immediate retention job through `POST /api/maintenance/log-retention/jobs` with a table name plus either an explicit cutoff or `delete_all=true`.

### 9.2 Retention Job Flow

```
Operator -> Settings Global tab -> Log Retention
  -> Saves global day policies with PUT /api/settings/log-retention
  -> Starts POST /api/maintenance/log-retention/jobs
  -> Returns 202 with { job_id, state, status_url, scope }
  -> Sets Location to /api/management/jobs/{job_id}
  -> Background worker runs a durable log_retention job with profile_id = 0
  -> Operator can observe or cancel the job through management job APIs
```

If a job omits `cutoff` and `delete_all`, the backend computes the cutoff from the stored global policy for the requested table. If no policy exists, the job is rejected and the operator must provide a cutoff, request delete-all, or configure `/api/settings/log-retention` first.

### 9.3 Partition Drop and Boundary Cleanup

Retention removes old partitions before it falls back to bounded row cleanup. Daily partitions use half-open UTC ranges, with `FROM` inclusive and `TO` exclusive. A whole child partition is expired when its upper bound is `<= cutoff`; those children are dropped as tables rather than cleaned with row deletes.

Only the single child partition that overlaps the cutoff receives a bounded delete for rows older than the cutoff. After that boundary delete, Prism runs `VACUUM (ANALYZE, PROCESS_TOAST TRUE)` on the boundary child so planner statistics and TOAST cleanup catch up without rewriting the whole table.

Operators can inspect managed root, child, and TOAST relation sizes with a read-only catalog query. Partitioned roots normally have `reltoastrelid = 0`; physical child partitions can own TOAST relations through `pg_class.reltoastrelid`.

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

If an operator performs manual bounded deletes against the cutoff-overlapping boundary child, the standard follow-up is the same child-only vacuum Prism uses automatically. Replace the child name with the actual boundary partition and do not run this against the partitioned root or already expired children.

```sql
VACUUM (ANALYZE, PROCESS_TOAST TRUE) public.request_logs_pYYYYMMDD;
```

Routine retention does not run parent-root bulk deletes. Operator-only shrink tools such as `VACUUM FULL`, `CLUSTER`, and `pg_repack` are manual or emergency database actions. They are not automatic retention steps, and `pg_repack` is not installed in the default local `postgres:16-alpine` image.

### 9.4 Audit and Request Linkage

Audit rows keep weak request metadata rather than a hard dependency on live request-log rows. They retain `request_log_id`, `request_log_created_at`, and `ingress_request_id` when known, but request detail links can be missing after request-log retention expires before audit-log retention.

Deleting or expiring request logs does not delete audit rows. Deleting or expiring audit rows does not affect request logs. Operators should treat request-to-audit linking as best-effort historical context, not as guaranteed referential availability.

### 9.5 Current Retention Boundaries

Partitioned retention manages the current log-table set only. Prism does not rewrite historical log storage shapes into the current partitions.

### 9.6 Frontend Placement

Log retention controls live on the Settings Global tab. Profile-scoped Settings sections still manage profile backup, costing, timezone, audit capture preferences, and other selected-profile state.

## 10. Database Design

See [DATA_MODEL.md](./DATA_MODEL.md) for complete schema.

## 11. API Design

See [API_SPEC.md](./API_SPEC.md) for complete API documentation.


## 12. Security Considerations

- **Operator Authentication**: Optional cookie-backed authentication for management APIs (`/api/*`). Supports username/password.
- **Proxy API Keys**: Optional API key enforcement for supported runtime operations mounted under `/v1` and `/v1beta`. Keys are issued and managed through the dashboard.
- **Auth Bifurcation**: Management auth (session cookies) and runtime auth (proxy API keys) are separate enforcement paths.
- **Data at Rest**: API keys and secrets are stored in PostgreSQL. Endpoint secrets are encrypted at rest.
- **CORS**: Local browser traffic stays same-origin through the launcher-local Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL.
- **Network**: No TLS termination; run behind a reverse proxy for HTTPS. Restricted to trusted local/LAN access.

## 13. Supported Runtime API Families

The runtime plane supports three fixed API families through the operation registry:

- **OpenAI** (`openai`): `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, and `openai.images.edits`
- **Anthropic** (`anthropic`): `anthropic.messages` and `anthropic.count_tokens`
- **Gemini** (`gemini`): `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`

The vendor catalog is separate and global. Models always carry required `api_family`, while `vendor_id` remains optional metadata, so operators may create additional vendor metadata rows such as `OpenRouter` without changing runtime compatibility. The Global settings tab exposes vendor create, edit, and delete flows, and deleting a vendor clears live model vendor metadata instead of blocking the delete.
