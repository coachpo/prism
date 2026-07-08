# Architecture Document: Prism

## 1. System Overview

Client (5173) → Prism (Management APIs + Proxy Engine → PostgreSQL) → Providers (OpenAI / Anthropic / Gemini)

*Local `./start.sh` keeps frontend `5173` and PostgreSQL `15432` fixed, and follows the selected bootstrap file's backend port. The checked-in `config.json` and freshly seeded bootstrap files use backend port `8000`. Standalone frontend containers commonly expose `3000`.

## 2. Component Architecture

### 2.1 Backend (Go runtime)

```
backend/
├── cmd/prism-backend/          # Go process entrypoint
├── internal/
│   ├── httpapi/
│   │   ├── management/         # /api/* management handlers
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
│   ├── profiledomain/          # Default profile helpers and runtime active-profile loading
├── migrations/                 # Fresh-install SQL baseline applied at startup
├── testdata/                   # request, bootstrap, and realtime fixtures
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
│   ├── App.tsx                 # Query, BrowserRouter, auth-provider, and TanStack RouterProvider host
│   ├── app/router/             # Canonical route tree, protected shell gates, and search schemas
│   ├── context/
│   │   ├── AuthContext.tsx     # Operator auth bootstrap, refresh, and session state
│   │   └── ReportingCurrencyContext.tsx # Default-profile currency state
│   ├── lib/
│   │   ├── api.ts              # Typed API client + /api scoped X-Profile-Id injection
│   │   ├── types.ts            # TypeScript contracts aligned with backend schemas
│   │   ├── costing.ts          # Micros and currency formatting helpers
│   │   ├── reportingCurrency.ts # Shared reporting-currency cache and normalization
│   │   ├── timezone.ts         # Shared timezone formatting helpers
│   ├── hooks/
│   │   ├── useRealtimeData.ts  # WebSocket-backed live refresh helper
│   │   └── useTimezone.ts      # Shared timezone formatting helper
│   ├── components/
│   │   ├── layout/page.tsx     # Protected shell wrapper with sidebar provider and Outlet
│   │   ├── layout/app-layout/  # Sidebar, header, nav metadata, and version label
│   │   ├── loadbalance/        # Shared loadbalance renderers
│   │   ├── statistics/         # Shared statistics renderers
│   │   └── ui/                 # shadcn/ui components
│   ├── features/
│   │   ├── observe/           # `/observe` dashboard adapter
│   │   ├── models/            # `/models` list and `/models/$modelId` detail adapter
│   │   ├── endpoints/         # `/route/endpoints`
│   │   ├── loadbalance/       # `/route/ban-policies`
│   │   ├── pricing/           # `/route/pricing`
│   │   ├── proxy-keys/        # `/control/proxy-keys`
│   │   ├── request-logs/      # `/observe/requests` and dedicated audit adapter
│   │   └── settings/          # `/system/settings` profile and global settings
│   └── pages/
│       ├── LoginPage.tsx
│       ├── DashboardPage.tsx, RequestLogsPage.tsx, SettingsPage.tsx
│       └── */                 # Legacy/oracle page clusters reused by feature routes and tests

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
- The root `Dockerfile` plus root `docker-compose.yml` are the default local/self-hosted bundle: Compose builds one Prism app image, runs PostgreSQL as a separate service, and the app image runs the Go backend behind Nginx with optional React assets. `backend/Dockerfile` is the separate backend-only image path used by backend image builds and GHCR workflows.
- The backend-only image runs as `prism:prism`, UID/GID `1000:1000`. Container deployments that bind mount `/app/config` or any other `PRISM_CONFIG_PATH` parent must make that host directory writable by UID/GID `1000:1000`; new and existing root-owned mounts should be prepared once with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.
- File-backed startup edits are made directly in `config.json` and require a Prism restart after R2. External edits are not watched automatically.
- Operational telemetry is startup-JSON-owned: the top-level `telemetry` section configures OTLP endpoint, protocol, compression, timeout, auth, TLS, metrics, and traces. Prism does not use long-lived `OTEL_*` environment variables as the steady-state config source.
- The primary ops path is OTLP to an OpenTelemetry Collector or Grafana Alloy, with Prometheus/Grafana/Tempo or another backend attached from that collector layer. The backend does not mount a local `/metrics` scrape endpoint.
- Request-history APIs and settings-page state flows remain PostgreSQL-backed product state instead of bootstrap or OTLP ownership.
- Disaster recovery is handled outside the dashboard with `pg_dump` plus a copy of the plaintext startup config.
- `.github/workflows/docker-images.yml` builds the separate backend and frontend GHCR images only (no backend pytest or frontend lint/typecheck jobs) and currently targets `linux/arm64`.

### 2.4 Priority Enforcement And Operator-Facing Failure Modes

Prism assigns trusted backend priority metadata before work touches shared resources. Runtime proxy traffic is `proxy`, management routes are `management` with an explicit `M1`, `M2`, or `M3` tier, and scheduler-owned workers are `background` with a declared subclass, budget, coalescing policy, retry policy, and drain policy. Priority-sensitive backend changes should stay covered by the standard priority regression tests, including `go test ./tests/priority/...`.

PostgreSQL capacity is split into finite named lanes: `runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `realtime`, `cache_refresh`, and `background_jobs`. Operators should treat lane saturation by owner: proxy execution pressure is separate from management UI pressure, telemetry drain pressure, lossy feedback drain pressure, realtime fanout, cache refresh, and generic background jobs. Background or management saturation must not consume protected proxy capacity. Lane metrics are emitted through OTLP DB-pool instruments, not through a backend-local Prometheus text endpoint.

Management overload is reported as typed admission failure with retry metadata. Lower-priority M3 reporting and maintenance routes shed before M2 and M1 management work, and proxy traffic remains isolated from management/background saturation. When overload appears, retry after the advertised delay rather than increasing client concurrency.

Scheduler lag means background workers are queued, coalesced, delayed, retried, or dropped according to their worker policy. Lag can delay dashboard fanout, telemetry materialization, management side-effect dispatch, cache warming, and proxy-key usage flushing, but it must not make request-path handlers borrow direct goroutines, direct DB handles, or unmanaged timers.

Durable outboxes expose failure as queued, retry, sent/succeeded, dead-letter, or permanent-failure state depending on the store. Management side-effect dispatch failures retry or become visibly permanent failures without rolling back the already committed primary management mutation.

Runtime telemetry and runtime feedback have different loss semantics. Accepted runtime activity intents are required-durable background work until the telemetry outbox transaction commits, terminal validation fails, or shutdown prevents completion. Runtime feedback is intentionally lossy under pressure; queue-full, invalid, closed, or store-failure cases drop feedback with accounting and never block proxy responses.

Audit and statistics reads are bounded. Raw audit lists require backend-enforced time windows and keyset cursors. The overview dashboard reads one canonical `/api/stats/dashboard` aggregate snapshot with backend-computed Routing Health Map data, while the internal rollup helper remains scoped to background rollup tests. Broad deletes run as durable management jobs.

Runtime cache correctness is generation-based. Management mutations advance durable runtime-cache generations in the same transaction as the primary state change, runtime reads validate generation vectors and refresh or fail closed when stale, and post-response cache warming is non-authoritative. Cache generation lag may delay warm snapshots, but auth-sensitive runtime reads reject stale or unverifiable snapshots instead of accepting old state.

## 3. Request Flow

Prism is proxy-first. It forwards only the provider-native operations registered in the runtime operation catalog, and it is not a full OpenAI, Anthropic, or Gemini API clone.

The operation registry is the ingress contract for the runtime plane. Each supported operation declares an exact HTTP method, path template, API family, model-binding source, streaming classification, canonical operation name, and provider adapter when upstream transport is required. The current canonical operation names are `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, `openai.images.edits`, `anthropic.messages`, `anthropic.count_tokens`, `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`. Requests that do not match that registry are rejected before body reads, planning, provider transport, telemetry, audit, feedback, or durable side effects.

After registry resolution, every runtime operation enters the same execution core. The shared runtime and gateway layers resolve against frozen Default profile id `1`, resolve ordered access targets to a final model-private connection or final model target, apply the attached explicit Ban Policy strategy, claim leases, record retained request history through durable seams, and emit bounded OTLP metrics/traces through startup-owned providers when enabled. Provider adapters own provider-specific parsing, upstream request building, response adaptation, streaming terminal classification, usage extraction, token counting or estimation, media handling, overflow classification, and pure OpenAI Chat/Responses request, response, and stream conversion. The shared runtime/gateway owns the operation registry, routing, admission, SSE lifecycle, accounting, telemetry, audit persistence and raw capture, pricing, feedback, request-log metadata, and side-effect handoff.

OpenAI Chat Completions and Responses can translate only across explicit sibling-operation terminal targets. Planning remains ingress-led: estimation, generation-parameter extraction, and `operation_name` come from the client-visible operation. A selected OpenAI connection's `openai_text_capability` decides whether the attempt is native with `operation_translation_mode = "none"` or translated with `openai_responses_to_chat_completions` or `openai_chat_completions_to_responses`. The capability values are `responses_only`, `chat_completions_only`, and `dual_native`. Native-compatible terminal attempts remain preferred before translated attempts, and sibling translation is always on only for adapter-approved text-only shapes. Unsupported shapes are not universally routable, so tools, multimodal or file inputs, stateful Responses features, structured-output mismatches, streaming event-shape mismatches, and other adapter-rejected conversions stay blocked by adapter capability checks. Responses adjunct operations require responses-capable targets and never translate to Chat Completions. Translation rewrites supported request shapes after target selection, rewrites non-stream or stream responses back to ingress shape for the client, preserves canonical usage from upstream payloads or stream terminal events, and drops unsafe entity headers from translated responses.

Runtime observability stores canonical disjoint token components. Base input, cache-read input, cache-creation input, base output, and reasoning output are separate dimensions, while provider totals remain authoritative when supplied. Pricing uses five concrete pricing strings from the attached template snapshot, and explicit `"0"` component prices mean configured free pricing instead of a missing-price condition.

Terminal Target `openai_text_capability` remains connection-owned metadata used by supported OpenAI operation-translation checks. Model-owned capability authoring, context-window preflight filtering, and overflow-promotion routing have been removed; ordinary strategy selection now uses explicit Ban Policy routing families.

### 3.1 Runtime Request With Private Connection Target

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Operation registry resolves `openai.chat_completions` and its OpenAI provider adapter
  -> Shared core resolves against frozen Default profile id `1` at request start
  -> Gateway assigns one Prism `ingress_request_id` for the incoming runtime request
  -> Request setup resolves the requested model and its ordered access targets in frozen Default profile id `1` scope
  -> Planner reaches the model's Terminal Target, applies the attached explicit Ban Policy strategy, and checks admission counters plus retry-window state
  -> Executor claims the primary attempt lease and forwards the request to the selected endpoint
  -> Upstream responds with JSON
  -> Gateway returns JSON to client, releases any non-stream lease, persists one `request_logs` row for the attempt, and feeds the outcome back into runtime routing state
```

### 3.2 Runtime Request Through Model Access Targets

```
Client -> POST /v1/messages {model: "claude-sonnet-4-5"}
  -> Operation registry resolves `anthropic.messages` and its Anthropic provider adapter
  -> Shared core resolves against frozen Default profile id `1`
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
  -> Shared core resolves against frozen Default profile id `1`
  -> Gateway assigns one Prism `ingress_request_id`
  -> Access-target resolution, route planning, adapter request build, and admission finish before downstream commit
  -> Executor claims a streaming lease before opening the upstream stream
  -> ProxyService opens streaming connection to the selected upstream endpoint
  -> SSE chunks stream back to the client after provider-adapter stream classification allows the operation
  -> Internal buffering is automatic for rewrite, replay, or hook-safety cases before downstream commit
  -> First downstream byte/event commits the stream boundary
  -> After commit: no retry, redirect, or hedge replay can start
  -> On stream finalization or cancellation: release the stream lease, persist the per-attempt request log, and record runtime feedback
```

### 3.4 API Family Routing

| API family            | Canonical operation names                       | Supported Prism operation paths                    | Upstream path                                      | Auth header                                          |
| --------------------- | ----------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                | `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, `openai.images.edits` | `GET /v1/models`, `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/responses/input_tokens`, `POST /v1/responses/compact`, `POST /v1/images/generations`, `POST /v1/images/edits` | Local response for `GET /v1/models`; otherwise same path under `{base_url}` | `Authorization: Bearer {key}`                        |
| Anthropic             | `anthropic.messages`, `anthropic.count_tokens`  | `POST /v1/messages`, `POST /v1/messages/count_tokens` | Same path under `{base_url}` | `x-api-key` set to `{key}` plus `anthropic-version` set to `2023-06-01` |
| Gemini                | `gemini.generate_content`, `gemini.stream_generate_content`, `gemini.count_tokens` | `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent`, `POST /v1beta/models/{model}:countTokens` | Same path under `{base_url}` | `Authorization: Bearer {key}`                        |

OpenAI runtime support is limited to the registered local models list plus the chat, Responses generation, Responses input-token, Responses compact, and image operations listed above. Stored Responses object lifecycle APIs, including retrieve, list, delete, and cancel routes, are outside Prism's supported contract. `openai.images.generations` and `openai.images.edits` are media operations with copy-only token usage semantics, not generic OpenAI passthrough routes.

Note: Gemini requests use `/v1beta/models/{model}:...` paths only. When access-target resolution reaches a different final Gemini model ID, Prism rewrites the model ID segment in the URL path before forwarding upstream.
For Gemini, `gemini.stream_generate_content` and the `:streamGenerateContent` path are authoritative for stream classification even when the request body omits `stream: true`; `gemini.generate_content` remains non-stream generate content, and `gemini.count_tokens` remains the token-count operation.

Runtime upstream requests capture an immutable bootstrap runtime snapshot at request start. The snapshot includes an HTTP client built from startup bootstrap transport settings. Fresh seeds use transport `100/16/16/300s/90s/0s/10s/1s` and side-effect attempt timeout `10s`. Runtime buffering is automatic and not user-configurable. The raw `runtime.transport.requestTimeout` Go duration is applied as `http.Client.Timeout`, which makes it the whole-request timeout for outbound provider calls. A missing request timeout fails startup validation by design. Raw `runtime.sideEffects.attemptTimeout` is a separate per-attempt background side-effect enqueue budget. Both values change only after editing `config.json` and restarting Prism.

Hot bootstrap projection still owns the runtime snapshot used by CORS origin checks, auth TTL and cookie metadata, runtime transport, and M2/M3 management admission limits, but R2 removed the management API caller that published new snapshots from file edits.

Startup boundaries are process resources: listener host and port, PostgreSQL URL and pool budgets, runtime transport, runtime side-effects attempt timeout, runtime secret encryption key, auth JWT signing key, CORS, auth TTL and cookie metadata, management admission, and telemetry. Mail fields remain parse-only for old `config.json` files. Edit `config.json` and restart Prism to apply changes.

Runtime compatibility and redirect checks use each model's required `api_family`. Model rows do not depend on catalog metadata for routing, validation, or display. The Models page renders each row's `api_family` metadata directly.

### 3.5 Management API Profile Scoping
- Prism keeps one route-class matrix:
  - Global management routes omit `X-Profile-Id`.
  - Profile-scoped management routes accept `X-Profile-Id`, but the backend ignores its value and resolves against Default profile id `1`.
  - Supported runtime operations under `/v1` and `/v1beta` ignore management overrides and always resolve against frozen Default profile id `1`.
- Global management routes include `/api/auth/*`, `/api/realtime/*`, auth and proxy-key settings under `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Multi-profile management is frozen. Profile-scoped management reads and writes are pinned to Default id `1`; runtime routing still loads the published Default-profile runtime snapshot.
- Scope-control errors return stable `code` values plus human-readable `detail` text.
- Supported runtime operations always resolve against frozen Default profile id `1` and ignore override headers.

The protected frontend shell derives sidebar destinations and breadcrumbs from the route metadata in `frontend/src/components/layout/app-layout/useShellNavigation.ts`, and persists only the desktop sidebar collapse preference in localStorage. Mobile drawer state remains transient browser UI state.

The Settings shell mirrors that split: the Profile tab keeps billing and currency, timezone, audit and privacy flows scoped to Default id `1`, while the Global tab owns instance-wide authentication and global log retention. Normal log retention applies across all profiles; list and detail APIs are pinned to Default id `1`.

### 3.6 Custom Header Injection

When a connection has `custom_headers` configured, they are injected into the upstream request after all other headers:

```
build_upstream_headers():
  1. Start with client headers (minus hop-by-hop, minus client auth headers, minus proxy-controlled auth/version headers)
  2. Apply blocklist sanitization to client-supplied headers
  3. Add api-family auth headers
  4. Add api-family extra headers (e.g., anthropic-version)
  5. Apply connection custom_headers (from `connections.custom_headers` JSON), skipping proxy-controlled auth/version header names
     -> Same-name ordinary headers from earlier steps are overwritten
  6. Apply final blocklist pass (with api-family auth/version headers protected)
     -> Blocked headers cannot be reintroduced by custom headers
  7. Return final header dict
```

Custom headers are a power-user feature. They can override ordinary forwarded headers, but they cannot override Prism-controlled authentication or provider-version headers and cannot re-add headers blocked by the Header Blocklist. This is enforced by skipping proxy-controlled custom header names and applying the blocklist last in the header construction pipeline.

### 3.7 Realtime Dashboard And Analytics Updates

```
Dashboard overview page -> WebSocket connect /api/realtime/ws
  -> If auth enabled: management auth handlers validate the access-token cookie
  -> Client sends {type: "subscribe", profile_id, channel: "dashboard"}
  -> Realtime manager stores dashboard room membership keyed by profile and channel

Proxy request completes
  -> Runtime telemetry persists request history and usage-event data
  -> Dashboard activity publisher sends {type: "dashboard.activity", profile_id, activity_watermark, activity} with one request-history item
  -> Dashboard snapshot publisher sends {type: "dashboard.snapshot", profile_id, snapshot} only after aggregate snapshot rebuilds
  -> REST bootstrap reads the stats-only snapshot from GET /api/stats/dashboard
  -> REST bootstrap reads recent activity from GET /api/stats/dashboard/recent-activity
  -> Overview and backend-owned topology graph state reconcile against the snapshot revision, while activity rows reconcile separately

Dashboard analytics tab -> WebSocket connect /api/realtime/ws
  -> Client sends {type: "subscribe", profile_id, channel: "analytics", preset}
  -> Realtime manager stores analytics room membership keyed by profile, channel, and preset scope
  -> Service sends an initial full `analytics.snapshot` for that {profile_id,preset}
  -> Manual refresh sends {type: "refresh", profile_id, channel: "analytics", preset}
  -> Refresh returns a fresh full `analytics.snapshot` on the socket
  -> Analytics snapshots include the usage snapshot plus endpoint model statistics keyed by endpoint ID string
  -> The frontend treats each `analytics.snapshot` as a full replacement for that scoped analytics view
```

The realtime API has two supported channels. The dashboard channel emits `dashboard.snapshot` for stats-only aggregate snapshots and `dashboard.activity` for single request-history feed entries. Snapshot ordering uses lexicographic `snapshot_revision`; `source_watermark` is diagnostic. Activity uses `activity_watermark` and `request_log_id` for feed reconciliation and request-log drilldown only. `analytics.snapshot` is scoped by `{profile_id,preset}` inside the WebSocket message payload and is the Analytics tab's preferred data path; the current UI falls back to `GET /api/stats/usage-snapshot` when no realtime snapshot arrives, uses the same REST route for manual-refresh fallback, and uses endpoint model statistics REST calls for drilldown. The REST stats endpoints, including `GET /api/stats/dashboard`, `GET /api/stats/dashboard/recent-activity`, request-history detail/list routes, spending, throughput, model metrics, and `GET /api/stats/usage-snapshot`, remain product-facing retained-history APIs; OTLP/Prometheus operations telemetry does not replace them.

## 4. Routing Strategies and Runtime Health Signals

### 4.1 Routing policy contract

- Models attach one profile-scoped explicit loadbalance strategy.
- Strategies carry the routing family field `legacy_strategy_type` (`single`, `fill-first`, or `round-robin`).
- Strategies also carry explicit Ban Policy fields for failure status codes, retry delay, backoff, jitter, retry-window limits, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, ban mode, and ban duration.
- Retry-cycle exhaustion is inclusive at `cycle_retry_attempts >= cycle_retry_attempt_limit`.
- Ban creation is inclusive at `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; the runtime never derives this threshold from the cycle limit.
- `ban_mode` accepts `off`, `temporary`, and `until_reset`. The `until_reset` mode keeps a terminal target's current connection row banned until the current-state reset endpoint clears it.
- The loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default single routing`, `Default fill-first routing`, and `Default round-robin routing` for Default profile id `1`.
- Upstream request timing is controlled by shared backend timeout settings, not by per-strategy timeout documents.

### 4.2 Runtime execution pipeline

1. The operation registry resolves the exact runtime operation and hook collection before the request body is consumed.
2. Request setup resolves the active-profile model by exact `planningSnapshot.ModelsByID` lookup, ordered access targets, attached strategy, and one immutable effective strategy snapshot for the request.
3. Planner and runtime-state helpers read `routing_connection_runtime_state` to build the current candidate set from admission counters and Ban Policy retry-window state.
4. The shared execution core claims per-attempt leases and uses shared upstream timeout behavior from the backend runtime before any client-visible bytes are committed.
5. Operation request, response, stream, and media hooks interpret provider-native payload details by canonical operation name. Token-count hooks are attached to `anthropic.count_tokens` and `gemini.count_tokens`, media hooks are attached to `openai.images.generations` and `openai.images.edits`, and the Gemini SSE hook is attached to `gemini.stream_generate_content`; passive outcomes feed back into connection-global runtime state while durable transition history persists model-policy snapshots and exposes them on event APIs as `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` when Ban Policy evaluation produced the event.

If all eligible candidates are unavailable inside the current retry window, the gateway returns `503` with routing-availability detail. Exact facade routing, context-window preflight filtering, regex matching, capability-metadata expansion, hidden weight or tier semantics, and response-body model rewriting are retired. Request logs and usage events keep the existing requested-model and resolved-target fields.

## 5. Unified Model Access

### 5.1 Concept

Models resolve through ordered access targets. Public target authoring points only to other same-profile, same-`api_family` models. Terminal Targets are the product-facing model-private endpoint bindings. They remain in `model_access_targets` as internal `connection` ownership and terminal routing edges backed by `connections` rows. Model targets can chain until a Terminal Target is reached, and the runtime records requested model, final target model, selected terminal target, and endpoint for observability. The requested model enters planning by exact `model_id`, not by regex, capability metadata, facade policy, or context-window preflight matching.

### 5.2 Rules

- Access targets must stay in the same profile and same `api_family`.
- Compatibility connection targets are terminal and are presented as Terminal Targets in product-facing routing surfaces.
- Model targets can chain, but cycles and self-targets are rejected.
- Endpoints are reusable. Terminal Targets are created and managed from model detail through model-scoped connection routes while retaining `connections` and `connection_id` compatibility names.
- Every access target carries explicit ordering metadata.
- Access-target `position` orders flat peers and is not priority, tier, or weight.
- Model IDs are unique within a profile.
- The gateway may normalize provider request payloads before forwarding, for example rewriting the requested model ID to the final target model ID for upstream compatibility. Prism does not rewrite response-body model identity on the client-facing way back out.

Model contracts require `api_family`; runtime compatibility is checked against `api_family` only.

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

### 5.4 Default profile and active runtime separation

Profile-scoped management APIs are frozen to Default id `1`. They accept `X-Profile-Id` for frontend compatibility, but the backend ignores the header value. Runtime proxy traffic ignores that management header and resolves through the frozen Default profile id `1` runtime snapshot.

### 5.5 Dashboard topology graph

`GET /api/stats/dashboard` and the snapshot inside realtime `dashboard.snapshot` include a backend-owned `topology_graph` alongside the legacy `routing_health_map`. The graph is built from Default-profile configuration and final-attributed telemetry in the backend, not reconstructed by the browser from management reads. Disabled models remain present as muted model nodes, inactive terminal targets remain present as muted target nodes, and endpoint nodes stay visible when referenced by configured terminal targets. During the additive compatibility wave, the backend keeps compatibility kinds (`connection`, `model_to_connection`, and `connection_to_endpoint`) and exposes product-facing terminal-target meaning through `product_kind`, with `connection_id` retained as the persisted compatibility identifier.

## 6. Terminal Target Health Detection

### 6.1 Concept

Manual health checks use one lightweight probe runner so Terminal Target verification stays on the same api-family-aware wire contract as the rest of the runtime stack.

### 6.2 Health Probes (API-Family-Specific)

Health checks send api-family-specific lightweight requests using the Terminal Target's configured model ID and a simple prompt. This validates full-chain URL routing, authentication, and model availability using the same URL-building logic as the proxy engine.

- **OpenAI**: endpoint base URL joined with `/v1/responses` or `/v1/chat/completions` based on the Terminal Target's persisted `openai_probe_endpoint_variant`; current variants are `responses_minimal` (default), `responses_reasoning_none`, `chat_completions_minimal`, and `chat_completions_reasoning_none`. This is health-probe behavior only. Runtime OpenAI text capability comes from `openai_text_capability`, not probe results or probe variant.
- **Anthropic**: endpoint base URL joined with `/v1/messages` and `{"model":"{model_id}","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
- **Gemini**: endpoint base URL joined with `/v1beta/models/{model}:generateContent` with minimal content payload and `maxOutputTokens: 1`.

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

Model-level health is computed from retained request-log rows grouped by the requested `request_logs.model_id` in the effective profile:

- Success rate = `COUNT(2xx for model_id) / COUNT(* for model_id) * 100`
- Request count = total retained request-log rows for that requested model ID
- Displayed on Dashboard and Models pages as a colored badge
- Same color thresholds as Terminal Target badges

### 6.6 Error Reporting

When a health check fails, the upstream error message is extracted from the response body and stored in `health_detail`. This provides actionable diagnostics (e.g., "HTTP 503: No available channel for model X" instead of just "HTTP 503"). The detail is shown in the frontend tooltip on hover.

### 6.7 URL Path Joining

Endpoint `base_url` values may include an upstream path prefix such as `/v1` or `/v1beta`. On create and update, Prism strips trailing slashes, requires a scheme and host, and rejects query strings or fragments. Runtime joins the normalized endpoint path with the allowlisted operation path for the selected request.

Prism does not document or apply version-segment de-duplication. Operators should configure endpoint paths to match the operation path shape expected by that upstream.

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

                              Durable runtime telemetry handoff:
                                - Persist request attempt to request_logs
                                - If audit capture is enabled: persist attempt to audit_logs
```

### 7.3 Data Captured

- Profile ID attribution, requested model ID, final target model ID, api family, terminal-target compatibility ID, endpoint base URL, and endpoint description
- Prism `ingress_request_id`, per-request `attempt_number`, persisted ingress `operation_name`, additive `upstream_operation_name`, `operation_translation_mode`, `upstream_request_path`, and best-effort `provider_correlation_id`
- HTTP status code, response time (ms)
- Token usage (input, output, total), extracted by upstream operation response or stream hooks before any client-facing response translation
- Flat final-target attribution, including requested model, resolved target model, selected terminal target, endpoint, operation, upstream operation, translation mode, and sanitized upstream request path.
- Stream flag, ingress request path, sanitized upstream request path, error details

Request-log semantics are per-attempt: one incoming runtime request can create multiple request-log rows when failover or retries occur. `ingress_request_id` groups those rows while `request_id` remains the unique identifier for one stored attempt row. Final usage ownership stays with the final returned response.

### 7.4 Query Capabilities

- Filter by model, final target model, caller client rule, endpoint, api family, status, and time range
- Aggregated statistics with grouping by model/api family/endpoint using stored endpoint label snapshots
- Pagination for request log listing

These query APIs intentionally remain product-facing retained-history surfaces for the UI and operators. Prometheus/Grafana should consume Prism operations data from the configured OTLP Collector or Alloy path instead of scraping Prism for local metrics.

For operation attribution, runtime traces keep `prism.operation_name` as ingress-led. Additive upstream-attribution attributes include sanitized `prism.upstream_operation_name`, `prism.operation_translation_mode`, and `prism.upstream_request_path`; the upstream path value is normalized through the operation registry so path-bound model IDs do not leak into spans. Other runtime span attributes include `prism.api_family`, `prism.streaming`, `prism.status_class`, `prism.stream_outcome`, `prism.route_reason`, `prism.usage_source`, `prism.pricing_config_version_used`, `prism.body_mode`, `prism.attempt_result`, `prism.attempt_count`, `prism.feedback_kind`, `prism.enqueue_status`, OpenAI translation-loss diagnostics under `prism.runtime.translation_*`, `http.request.method`, and `http.response.status_code`.

## 8. Request Audit Logging

### 8.1 Concept

Audit logging records request-time provenance without changing runtime proxy behavior. Sensitive headers are redacted before storage. Runtime snapshots load audit policy from `profile_api_family_audit_settings` by profile and model API family, then persist request-time booleans to existing audit and request-log fields. Audit rows are written per upstream attempt, including failover attempts, and metadata-only requests still create audit metadata when audit is enabled. Body capture is allowed only when audit is enabled for that family. Translated OpenAI attempts keep audit request and response bodies upstream-native; the rewritten client-facing request or response body is never substituted into audit storage.

### 8.2 Audit Flow

```
Client -> supported runtime operation
  -> Operation registry and planner resolve the attempt
  -> ProxyService forwards request to upstream
  -> Runtime persists request_logs with immutable profile attribution
  -> If audit capture is enabled for the request:
       -> One audit row for this upstream attempt
       -> Redact sensitive headers
       -> Record connection metadata as a snapshot
       -> Link to request_log metadata when available
       -> Store immutable profile_id attribution
       -> Store bodies only when body capture is enabled
  -> Return response to client
```

### 8.3 Non-Interference Guarantees

- Audit INSERT failures are logged and never propagated to the client
- Streaming audit uses its own write path, separate from request-scoped runtime state
- Audit never changes request routing or response handling

### 8.4 Redaction

Applied at write time before INSERT:

- `authorization` preserves the scheme as `Bearer [REDACTED]`; `x-api-key` and `x-goog-api-key` become `[REDACTED]`
- Any header name containing `key`, `secret`, `token`, or `auth` has its value redacted
- Body fields are not redacted and may contain sensitive user data, so body capture remains an explicit request-time setting

### 8.5 Dedicated Audit Detail Page

The audit detail view is reached from request investigation at `/observe/requests/:requestId/audit`. It shows summary metadata, redacted request headers/body when stored, response status/headers/body when stored, and connection identity fields. The request-log side drawer remains overview-only and links to this page instead of loading audit payloads inline.

### 8.6 Conditional Decompression

The Go runtime only requests decompressed response bodies when audit body capture needs them. When body auditing is disabled, the proxy avoids unnecessary body decoding work and preserves the response path without adding request latency for unused payload capture.

## 9. Global Log Retention

### 9.1 Concept

Historical `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events` are partitioned by UTC day and managed by global log-retention jobs. Normal retention is instance-wide across all profiles. Default-profile filtering still applies to profile-owned list and detail APIs, but `X-Profile-Id` does not scope retention settings or retention jobs.

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

Log retention controls live on the Settings Global tab. Profile-scoped Settings sections still manage costing, timezone, audit capture preferences, and other Default-profile state.

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

- **OpenAI** (`openai`): `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, and `openai.images.edits`
- **Anthropic** (`anthropic`): `anthropic.messages` and `anthropic.count_tokens`
- **Gemini** (`gemini`): `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`

Models always carry required `api_family`; runtime compatibility does not depend on catalog metadata.
