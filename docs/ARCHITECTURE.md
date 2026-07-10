# Architecture Document: Prism

## 1. System Overview

Client (5173) → Prism (Management APIs + Proxy Engine → PostgreSQL) → Providers (OpenAI / Anthropic / Gemini)

*Local `./start.sh` keeps frontend `5173` and PostgreSQL `15432` fixed, and follows the selected bootstrap file's backend port. Freshly seeded repo-local bootstrap files use backend port `8000`. Standalone frontend containers commonly expose `3000`.*

## 2. Component Architecture

### 2.1 Backend (Go runtime)

```
backend/
├── cmd/prism-backend/          # Go process entrypoint
├── internal/
│   ├── domain/
│   │   ├── audit/              # audit persistence and redaction helpers
│   │   ├── loadbalance/        # Ban Policy and process-local routing state
│   │   └── stats/              # request-log and aggregate query logic
│   ├── gateway/
│   │   ├── core/               # provider-neutral routing, accounting, and envelopes
│   │   ├── provider/           # provider-native adapters and OpenAI translation
│   │   └── routing/            # route and target-selection helpers
│   ├── httpapi/
│   │   ├── management/         # /api/* management handlers and conventions
│   │   └── runtime/            # operation-registered /v1 and /v1beta proxy handlers
│   ├── platform/
│   │   ├── background/         # worker scheduler, budgets, retries, and drain policy
│   │   ├── config/             # strict v1 bootstrap JSON and runtime settings
│   │   ├── db/                 # six isolated PostgreSQL lane pools
│   │   ├── http/               # server assembly, mounts, and admission middleware
│   │   ├── lifecycle/          # production construction and ordered shutdown
│   │   ├── logretention/       # daily partition horizon and retention operations
│   │   ├── managementjobs/     # durable maintenance-job execution
│   │   ├── managementsideeffects/ # durable after-commit management-event dispatcher
│   │   ├── migrate/            # SQL migration runner and schema helpers
│   │   ├── startup/            # migrations and invariant/default seeds
│   │   └── version/            # VERSION loader
│   ├── endpointdomain/         # endpoint and connection helpers
│   ├── profiledomain/          # Default profile helpers and frozen runtime snapshot loading
├── migrations/                 # Fresh-install SQL baseline applied at startup
├── testdata/                   # request and bootstrap fixtures
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
│   ├── App.tsx                 # Query, auth-provider, and TanStack RouterProvider host
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
│   │   └── useTimezone.ts      # Shared timezone formatting helper
│   ├── components/
│   │   ├── layout/page.tsx     # Protected shell wrapper with sidebar provider and route children
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
│       └── */                 # Page clusters reused by feature routes and tests

├── components.json             # shadcn config
├── package.json
├── vite.config.ts
└── tsconfig.json
```

### 2.3 Local Tooling and Build Workflow

- Prism is a monorepo: `backend/` and `frontend/` are root-owned directories that share the root launcher, release helper, and CI wiring.
- Root local orchestration lives in `start.sh`. It first validates `full|headless`, then loads the root `.env` only for variables absent from the invoking shell. Shell-provided values therefore take precedence over `.env`; the script restores its original `PATH` after loading the file.
- The launcher then checks required tools, resolves and exports `PRISM_CONFIG_PATH` (defaulting to repo-local `config.json`) and its local `DATABASE_URL` (`localhost:15432`), builds `backend/prism-backend`, seeds a missing bootstrap file, and validates the selected bootstrap file against the local launcher contract.
- Only after that validation does it bring down the helper compose project, reclaim Prism's backend/frontend ports, verify PostgreSQL port availability, start and wait for the PostgreSQL service from `backend/docker-compose.yml`, start the backend, and finally start Vite for `full` mode. The backend follows `server.port` in the selected bootstrap file; fresh local seeds use `8000`. PostgreSQL and Vite remain fixed at `15432` and `5173`.
- `./start.sh full` launches the frontend on `5173`, unsets `VITE_API_BASE`, and enables a launcher-local Vite proxy via `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at that effective backend port so browser traffic stays same-origin.
- Canonical startup config lives in a plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; backend-native fresh seeds default the database URL to `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, while `start.sh` sets `DATABASE_URL` to the local launcher PostgreSQL DSN on host port `15432` before seeding.
- The root `Dockerfile` plus root `docker-compose.yml` are the default local/self-hosted bundle: Compose builds one Prism app image, runs PostgreSQL as a separate service, and the app image runs the Go backend behind Nginx with optional React assets. `backend/Dockerfile` is the separate backend-only image path used by backend image builds and GHCR workflows. Nginx's `PRISM_BACKEND_UPSTREAM_PORT` is a static environment substitution; it is not derived from the bootstrap file. Any container deployment that changes bootstrap `server.port` must set `PRISM_BACKEND_UPSTREAM_PORT` to the same port.
- The root app image and backend-only image run as `prism:prism`, UID/GID `1000:1000`. Container deployments that bind mount `/app/config` or any other `PRISM_CONFIG_PATH` parent must make that host directory writable by UID/GID `1000:1000`; new and existing root-owned mounts should be prepared once with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.
- Bootstrap JSON is strict v1: `meta.schemaVersion` must be `1`, unknown fields are rejected, and `runtime.transport.requestTimeout` plus `runtime.sideEffects.attemptTimeout` are required positive durations. Legacy encrypted bootstrap fields (`secretPayload`, `database.urlSecretRef`, and `auth.jwtSigningKeySecretRef`) are rejected rather than migrated at boot. Missing files are seeded once, and the entrypoint has a narrow repair path for stale files rejected only because they contain retired `docsEnabled`; other invalid legacy shapes fail validation.
- Safe bootstrap snapshots expose non-secret values and secret metadata only. They never return secret material; `runtime.secretEncryptionKey` is preserve-only in v1 and its metadata is not editable through the bootstrap settings surface. File-backed startup edits require a process restart because Prism does not watch external config changes. Existing valid files are preserved until an operator stops Prism, removes or relocates the file, and restarts.
- `database.pools.realtime`, `auth.resetCodeTtlSeconds`, `runtime.routing.openaiTerminalTranslationMode`, `stateTransfer.bundleEncryptionKey`, `mail`, and `telemetry` remain parse-and-validate compatibility fields for live `config.json` files. They do not restore the removed realtime pool, reset-code TTL override, terminal-translation mode, state transfer, mail delivery, or telemetry exporter processes.
- The backend does not mount a local `/metrics` scrape endpoint.
- Request-history APIs and settings-page state flows remain PostgreSQL-backed product state instead of bootstrap telemetry ownership.
- Disaster recovery is handled outside the dashboard with `pg_dump` plus a copy of the plaintext startup config.
- `.github/workflows/docker-images.yml` builds the separate backend and frontend GHCR images for `linux/arm64` on `v*` tags and `workflow_dispatch`; tag pushes require a green CI conclusion on the tagged commit, while manual dispatch can build one service or both directly. `.github/workflows/cleanup.yml` only prunes untagged backend/frontend GHCR package versions.

### 2.4 Process Lifecycle

The backend process loads the strict bootstrap file, then runs migrations and startup seeds under a 30-second startup timeout. The seed sequence establishes profile invariants, user settings, user-agent client rules, app-auth settings, endpoint-secret normalization, and header-blocklist rules before production services are built.

Production construction creates the hot bootstrap runtime, opens six isolated PostgreSQL pools, creates the scheduler and durable background services, ensures the log partition horizon before serving traffic, creates the shared planning cache and a fresh process-local runtime-state store, builds management and runtime services, registers workers, assembles the HTTP server, and starts the scheduler before `App.Run` begins serving.

On shutdown Prism runs these phases in order: HTTP server shutdown, side-effect drains, scheduler stop, registered service closes in reverse registration order, and database-pool close. This order stops ingress first while allowing already accepted side effects a bounded drain window before worker and database resources disappear.

### 2.5 Priority Enforcement And Operator-Facing Failure Modes

Prism assigns trusted backend priority metadata before work touches shared resources. Runtime proxy traffic is `proxy`, management routes are `management` with an explicit `M1`, `M2`, or `M3` tier, and scheduler-owned workers are `background` with a declared subclass, budget, coalescing policy, retry policy, and drain policy. Priority-sensitive backend changes should stay covered by the standard priority regression tests, including `go test ./tests/priority/...`.

PostgreSQL capacity is split into finite named lanes: `runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `cache_refresh`, and `background_jobs`. The defaults derive `unit = clamp(GOMAXPROCS(0), 8, 16)`: management is `unit + 1`, runtime execution is `unit`, telemetry is `unit / 2`, and feedback, cache refresh, and background jobs are each `unit / 4`. The resulting six-lane maximum is 27 through 53 connections. Operators should treat lane saturation by owner: proxy execution pressure is separate from management UI pressure, telemetry drain pressure, lossy feedback drain pressure, cache refresh, and generic background jobs. Background or management saturation must not consume protected proxy capacity.

Management overload is reported as typed admission failure with retry metadata. Default M2 concurrency is `unit` and is clamped to `management.maxConns - 1`, reserving at least one M1 database slot. Default M3 concurrency is `unit / 2`; an M3 request must acquire both its M3 slot and a shared M2 slot, while the controller reserves one M2 slot from M3. Lower-priority M3 reporting and maintenance routes therefore shed before M2 and M1 management work. HTTP-level proxy admission has no global proxy counter; terminal-target admission remains part of the local runtime state. When overload appears, retry after the advertised delay rather than increasing client concurrency.

Scheduler lag means background workers are queued, coalesced, delayed, retried, or dropped according to their worker policy. Lag can delay dashboard materialization, telemetry materialization, management side-effect dispatch, cache warming, and proxy-key usage flushing, but it must not make request-path handlers borrow direct goroutines, direct DB handles, or unmanaged timers.

Durable outboxes expose failure as queued, retry, sent/succeeded, dead-letter, or permanent-failure state depending on the store. Management mutations place follow-up events in `management_outbox` in the primary transaction and wake the `management_side_effect_outbox` dispatcher after commit; handler failures retry or become visibly permanent failures without rolling back the committed management mutation. Dashboard snapshot invalidation is one such after-commit side effect. Failover incident webhook alerts use `alert_webhook_outbox` and the `alert_webhook_worker`; runtime feedback writes enqueue alert payloads in the same transaction as the loadbalance event, and webhook HTTP POSTs run only in background work.

Runtime telemetry has durable success handoffs, scheduled activity handoffs, and background materialization. Every provider-forwarded successful `2xx` response requires a durable `runtime_telemetry_outbox` row: buffered or translated responses commit a completed envelope before the response is committed, while passthrough SSE and non-SSE responses commit an accepted row before the first flush and finalize that row after response capture completes. Captured non-`2xx` activity, telemetry-eligible target-resolution/translation planning failures carrying `PlanningFailure`, and `admission_exhausted` execution failures first use a bounded in-memory scheduler side-effect queue, which later attempts durable outbox insertion and can be lost if rejected, terminally failed, or abandoned during shutdown. A worker materializes accepted outbox rows into `request_logs`, `audit_logs`, `usage_request_events`, and proxy-key usage in one transaction before deleting the outbox row. Runtime feedback is separately and intentionally lossy under pressure; queue-full, invalid, closed, or store-failure cases drop feedback with accounting and never block proxy responses.

Audit and statistics reads are bounded. Raw audit lists require backend-enforced time windows and keyset cursors. `GET /api/stats/dashboard` still returns backend-computed `routing_health_map`, but the current dashboard adapter does not render it; the production Models table presents retained success rate, P95 latency, and 24-hour request count as text rather than health badges. The connection-success-rate API also exists without a current production UI consumer. Broad deletes run as durable management jobs.

Runtime cache correctness is generation-based. Management mutations advance durable runtime-cache generations in the same transaction as the primary state change, runtime reads validate generation vectors and refresh or fail closed when stale, and post-response cache warming is non-authoritative. Cache generation lag may delay warm snapshots, but auth-sensitive runtime reads reject stale or unverifiable snapshots instead of accepting old state.

## 3. Request Flow

Prism is proxy-first. It forwards only the provider-native operations registered in the runtime operation catalog, and it is not a full OpenAI, Anthropic, or Gemini API clone.

Global CORS handling runs before the runtime branch. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the operation registry in that order. The operation registry is the ingress contract inside the authenticated runtime handler. Each supported operation declares an exact HTTP method, path template, API family, model-binding source, streaming classification, canonical operation name, and `HookCollectionID`; it does not declare a provider adapter. The current canonical operation names are `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `anthropic.messages`, `anthropic.count_tokens`, `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`. Requests that do not match that registry are rejected before body reads, planning, provider transport, telemetry, audit, feedback, or durable side effects.

`GET /v1/models` is the exception: `openai.models` branches to the local models-list handler before provider request-body handling, planning, or provider execution core. Every other registered proxy operation enters the shared runtime and gateway path: it resolves against frozen Default profile id `1`, resolves ordered access targets, applies the attached Ban Policy strategy, claims local attempt state, builds an upstream request, and hands activity to telemetry seams. The provider adapter is selected during planned-upstream request construction, not registry resolution. Request, non-stream response, and stream hooks are looked up by `HookCollectionID`, allowing related operations such as token counting or compact Responses to use hook collections different from their canonical operation names. Those hooks own generation extraction and stream intent, non-stream parsing and token usage, and stream terminal classification and usage merge respectively.

OpenAI Chat Completions and Responses can translate only across explicit sibling-operation terminal targets. Planning remains ingress-led: estimation, generation-parameter extraction, and persisted `operation_name` come from the client-visible operation. Translation requires three gates: the model's `openai_accepted_format`, the selected connection's `openai_text_capability` (`responses_only`, `chat_completions_only`, or `dual_native`), and the OpenAI adapter's conversion-capability check for the actual request, response, and stream shapes. If all gates allow it, the attempt is native with `operation_translation_mode = "none"` or translated with `openai_responses_to_chat_completions` or `openai_chat_completions_to_responses`. Attempt ordering still follows authored access-target and terminal-target order; Prism does not globally reorder native-compatible attempts ahead of translated ones. Responses adjunct operations require responses-capable targets and never sibling-translate. Translation rewrites supported request shapes after target selection, rewrites non-stream or stream responses back to the ingress shape for the client, preserves canonical usage from upstream payloads or stream terminal events, and drops unsafe entity headers from translated responses.

Runtime observability stores canonical disjoint token components. Base input, cache-read input, cache-creation input, base output, and reasoning output are separate dimensions, while provider totals remain authoritative when supplied. Pricing uses five concrete pricing strings from the attached template snapshot, and explicit `"0"` component prices mean configured free pricing instead of a missing-price condition.

Terminal Target `openai_text_capability` remains connection-owned metadata used by supported OpenAI operation-translation checks. Model-owned capability authoring, context-window preflight filtering, and overflow-promotion routing have been removed; ordinary strategy selection now uses explicit Ban Policy routing families.

### 3.1 Runtime Request With Private Connection Target

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Operation registry resolves `openai.chat_completions` and its HookCollectionID
  -> Shared core resolves against frozen Default profile id `1` at request start
  -> Gateway assigns one Prism `ingress_request_id` for the incoming runtime request
  -> Request setup resolves the requested model and its ordered access targets in frozen Default profile id `1` scope
  -> Planner reaches the model's Terminal Target, applies the attached explicit Ban Policy strategy, and checks admission counters plus retry-window state
  -> Planned request construction selects the provider adapter and builds the selected upstream request
  -> Executor claims the primary attempt lease and forwards that request to the selected endpoint
  -> Upstream responds with JSON
  -> Gateway returns JSON to client, releases any non-stream lease, commits durable telemetry on the applicable success path, and feeds the outcome back into local runtime routing state
```

### 3.2 Runtime Request Through Model Access Targets

```
Client -> POST /v1/messages {model: "claude-sonnet-4-5"}
  -> Operation registry resolves `anthropic.messages` and its HookCollectionID
  -> Shared core resolves against frozen Default profile id `1`
  -> Resolver evaluates ordered same-profile, same-api-family model targets and aggregates eligible candidates
  -> Direct Terminal Targets are considered only when no model-target candidate is eligible
  -> Executor plans attempts against Terminal Targets
  -> Upstream responds; eventual request history keeps model_id as the requested model and resolved_target_model_id as the final target model for each materialized attempt
  -> Gateway returns response to client
```

### 3.3 Runtime Request (Streaming)

```
Client -> POST /v1/chat/completions {model: "gpt-4o", stream: true}
  -> Operation registry resolves `openai.chat_completions`; its request hook determines stream intent
  -> Shared core resolves against frozen Default profile id `1`
  -> Gateway assigns one Prism `ingress_request_id`
  -> Access-target resolution, route planning, adapter request build, and admission finish before downstream commit
  -> Executor claims a streaming lease before opening the upstream stream
  -> ProxyService opens streaming connection to the selected upstream endpoint
  -> SSE chunks stream back to the client after provider-adapter stream classification allows the operation
  -> Internal buffering is automatic for rewrite or hook-safety cases before downstream commit
  -> First downstream byte/event commits the stream boundary
  -> After commit: no retry, redirect, or hedge replay can start
  -> On stream finalization or cancellation: release the stream lease, finalize the accepted telemetry outbox payload when possible, and record runtime feedback
```

### 3.4 API Family Routing

| API family            | Canonical operation names                       | Supported Prism operation paths                    | Upstream path                                      | Auth header                                          |
| --------------------- | ----------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                | `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact` | `GET /v1/models`, `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/responses/input_tokens`, `POST /v1/responses/compact` | Local OpenAI list or `client_version`-selected Codex catalog for `GET /v1/models`; otherwise same path under `{base_url}` | `Authorization: Bearer {key}`                        |
| Anthropic             | `anthropic.messages`, `anthropic.count_tokens`  | `POST /v1/messages`, `POST /v1/messages/count_tokens` | Same path under `{base_url}` | `x-api-key` set to `{key}` plus `anthropic-version` set to `2023-06-01` |
| Gemini                | `gemini.generate_content`, `gemini.stream_generate_content`, `gemini.count_tokens` | `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent`, `POST /v1beta/models/{model}:countTokens` | Same path under `{base_url}` | `Authorization: Bearer {key}`                        |

OpenAI runtime support is limited to the registered local models list plus the chat, Responses generation, Responses input-token, and Responses compact operations listed above. Stored Responses object lifecycle APIs, including retrieve, list, delete, and cancel routes, are outside Prism's supported contract.

The local OpenAI models operation branches only on presence of the `client_version` query parameter. Ordinary callers retain the OpenAI `object`/`data` response, while Codex clients receive the embedded model-catalog metadata with a stable weak ETag and exact-match `304` support. Both branches read the same frozen Default-profile runtime snapshot and never contact an upstream provider.

Note: Gemini requests use `/v1beta/models/{model}:...` paths only. When access-target resolution reaches a different final Gemini model ID, Prism rewrites the model ID segment in the URL path before forwarding upstream.
For Gemini, `gemini.stream_generate_content` and the `:streamGenerateContent` path are authoritative for stream classification even when the request body omits `stream: true`; `gemini.generate_content` remains non-stream generate content, and `gemini.count_tokens` remains the token-count operation.

Runtime upstream requests capture an immutable bootstrap runtime snapshot at request start. The snapshot includes an HTTP client built from startup bootstrap transport settings. Fresh seeds use transport `100/16/16/300s/90s/0s/10s/1s` and side-effect attempt timeout `10s`. Runtime buffering is automatic and not user-configurable. `runtime.transport.requestTimeout` is applied as `http.Client.Timeout`, making it the whole-request timeout for outbound provider calls; `runtime.sideEffects.attemptTimeout` is the per-attempt budget for scheduler-owned runtime activity handoff work. Strict v1 validation requires both durations and rejects zero or negative values.

The hot bootstrap projection owns the in-process snapshot used by CORS origin checks, auth TTL and cookie metadata, runtime transport, and M2/M3 management admission limits. No file watcher republishes external edits, so changing any effective field still requires a process restart.

Live startup resources include listener host and port, PostgreSQL URL and six-lane pool budgets, runtime transport, runtime side-effect attempt timeout, runtime secret encryption key, auth JWT signing key, CORS, auth TTL and cookie metadata, and management admission. Compatibility-only mail, telemetry, realtime-pool, reset-code, terminal-translation, and state-transfer fields do not create live services.

Runtime compatibility and redirect checks use each model's required `api_family`. Model rows do not depend on catalog metadata for routing, validation, or display. The Models page renders each row's `api_family` metadata directly.

### 3.5 Management API Profile Scoping
- Prism keeps one route-class matrix:
  - Global management routes omit `X-Profile-Id`.
  - Profile-scoped management routes accept `X-Profile-Id`, but the backend ignores its value and resolves against Default profile id `1`.
  - Supported runtime operations under `/v1` and `/v1beta` ignore management overrides and always resolve against frozen Default profile id `1`.
- Global management routes include `/api/auth/*`, auth and proxy-key settings under `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Multi-profile management is frozen. Profile-scoped management reads and writes are pinned to Default id `1`; runtime routing still loads the published Default-profile runtime snapshot.
- Scope-control errors return stable `code` values plus human-readable `detail` text.
- Supported runtime operations always resolve against frozen Default profile id `1` and ignore override headers.

The protected frontend shell derives sidebar destinations and breadcrumbs from the route metadata in `frontend/src/components/layout/app-layout/useShellNavigation.ts`, and persists only the desktop sidebar collapse preference in localStorage. Mobile drawer state remains transient browser UI state.

The Settings shell visibly uses `全局` and `实例` tabs while preserving internal query values `profile` and `global`. The visible `全局` tab keeps billing and currency, timezone, audit, privacy, and config-rule flows scoped to Default id `1`; the visible `实例` tab owns authentication and global log retention. Normal log retention applies across all profiles; list and detail APIs are pinned to Default id `1`.

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

### 3.7 Dashboard And Analytics REST Polling

```
Dashboard overview page
  -> Initial bootstrap reads the stats-only snapshot from GET /api/stats/dashboard
  -> Initial bootstrap reads recent activity from GET /api/stats/dashboard/recent-activity
  -> Page hook polls both REST endpoints every 30 seconds and on manual refresh
  -> Overview state reconciles against snapshot_revision
  -> Activity rows reconcile by request_log_id for feed dedupe and request-log drilldown

Proxy request completes
  -> Runtime telemetry hands activity to the durable outbox or scheduler-owned failure path
  -> Background materialization writes retained request history and usage-event data
  -> Dashboard and analytics REST reads observe retained history after that materialization

Dashboard analytics tab
  -> Initial load reads GET /api/stats/usage-snapshot?preset={preset}
  -> Page hook polls the same REST snapshot every 30 seconds and on manual refresh
  -> Endpoint drilldown rows load through GET /api/stats/endpoints/{endpoint_id}/models
  -> The frontend treats each accepted snapshot as a full replacement for that scoped analytics view
```

Dashboard and analytics updates use REST polling rather than a persistent browser transport. Snapshot ordering uses lexicographic `snapshot_revision`; `source_watermark` is diagnostic. Activity uses `activity_watermark` and `request_log_id` for feed reconciliation and request-log drilldown only. The REST stats endpoints, including `GET /api/stats/dashboard`, `GET /api/stats/dashboard/recent-activity`, request-history detail/list routes, spending, throughput, model metrics, and `GET /api/stats/usage-snapshot`, remain product-facing retained-history APIs.

## 4. Routing Strategies and Runtime Health Signals

### 4.1 Routing policy contract

- Models attach one Default-profile explicit loadbalance strategy.
- Strategies carry the routing family field `legacy_strategy_type` (`single`, `fill-first`, or `round-robin`).
- Strategies also carry explicit Ban Policy fields for failure status codes, retry delay, backoff, jitter, retry-window limits, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, ban mode, and ban duration.
- Retry-cycle exhaustion is inclusive at `cycle_retry_attempts >= cycle_retry_attempt_limit`.
- Ban creation is inclusive at `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; the runtime never derives this threshold from the cycle limit.
- `ban_mode` accepts `off`, `temporary`, and `until_reset`. The `until_reset` mode keeps a terminal target's current connection row banned until the current-state reset endpoint clears it.
- The loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default single routing`, `Default fill-first routing`, and `Default round-robin routing` for Default profile id `1`.
- Upstream request timing is controlled by shared backend timeout settings, not by per-strategy timeout documents.

### 4.2 Runtime execution pipeline

1. The operation registry resolves the exact runtime operation and hook collection before the request body is consumed.
2. Request setup resolves the frozen Default profile id `1` model by exact `planningSnapshot.ModelsByID` lookup, ordered access targets, attached strategy, and one immutable effective strategy snapshot for the request.
3. Planner and runtime-state helpers use the production `LocalRuntimeStateStore` to build the current candidate set from admission counters, leases, round-robin cursors, and Ban Policy retry-window state.
4. The shared execution core claims per-attempt local leases and uses shared upstream timeout behavior from the backend runtime before any client-visible bytes are committed.
5. Operation request, response, and stream hooks interpret provider-native payload details by `HookCollectionID`, not necessarily operation name. Token-count and compact operations use their dedicated collections; passive outcomes feed back into process-local connection state while durable `loadbalance_events` retain transition history and model-policy snapshots, including `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` when Ban Policy evaluation produced the event.

Runtime request bodies are capped at `20 MiB`; oversized supported-operation requests return JSON `413` with `error: "request_body_too_large"` and `limit_bytes` before planning. If all eligible candidates are unavailable inside the current retry window, the gateway returns `503` with routing-availability detail. If all otherwise available candidates are blocked by admission limits, runtime returns `503` with `error: "admission_exhausted"` plus route-reason metadata before upstream transport. Exact facade routing, context-window preflight filtering, regex matching, capability-metadata expansion, hidden weight or tier semantics, and response-body model rewriting are retired. Request logs and usage events keep the existing requested-model and resolved-target fields.

### 4.3 Runtime State Residency

Production creates a fresh `LocalRuntimeStateStore` for every process. It owns connection-level Ban Policy state, admission counters, in-flight lease counts, connection round-robin cursors, and model-target round-robin cursors. A normal restart resets this hot state safely.

The SQL tables `routing_connection_runtime_state`, `routing_connection_runtime_leases`, and `loadbalance_round_robin_state` remain compatibility schema, not the runtime hot path. Prism does not rely on startup reconciliation or persistent SQL leases for active routing. Durable `loadbalance_events` remain the historical record of routing transitions and incidents.

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
  model_candidates = []
  for target in ordered_model_targets(config):
    candidate = resolve_access(profile_id, target.model_id)
    if candidate is eligible:
      append_all(model_candidates, candidate.terminal_attempts)

  if model_candidates is not empty:
    return aggregate(model_candidates)

  direct_terminal_candidates = evaluate_all(ordered_terminal_targets(config))
  return aggregate(direct_terminal_candidates) or no_eligible_target
```

Every ordered model target is evaluated and eligible candidates are aggregated into one plan. Direct Terminal Targets are a fallback only when no eligible model-target candidate plan exists; routing does not return the first access-target match. The executor consumes the resulting ordered terminal attempts sequentially or with the strategy's configured hedge behavior.

### 5.4 Default profile and active runtime separation

Profile-scoped management APIs are frozen to Default id `1`. They accept `X-Profile-Id` for frontend compatibility, but the backend ignores the header value. Runtime proxy traffic ignores that management header and resolves through the frozen Default profile id `1` runtime snapshot.

## 6. Request-Derived Metrics

Prism has no manual Terminal Target probe routes or probe-backed health fields. Retained request history supports success-rate, latency, request-count, spending, and endpoint aggregates. The backend dashboard response includes `routing_health_map`, but the current dashboard adapter does not render that field. The production Models table shows success rate, P95 latency, and 24-hour request count as plain values; it does not assign health badges or color thresholds. `GET /api/stats/connection-success-rates` is available for consumers but is not currently used by the production UI.

### 6.1 URL Path Joining

Endpoint `base_url` values may include an upstream path prefix such as `/v1` or `/v1beta`. On create and update, Prism strips trailing slashes, requires a scheme and host, and rejects query strings or fragments. Runtime joins the normalized endpoint path with the allowlisted operation path for the selected request.

Prism does not document or apply version-segment de-duplication. Operators should configure endpoint paths to match the operation path shape expected by that upstream.

## 7. Request Statistics

### 7.1 Concept

Materialized runtime activity creates retained, product-facing request history for analytics, debugging, spending, and dashboard views. This is not a guarantee that every ingress request or every upstream transport failure will have history.

### 7.2 Logging Flow

```
Client -> Operation registry -> Router / Planner -> Terminal target -> Endpoint -> Upstream
                                                         ↓
                                              Applicable telemetry handoff
                                                         ↓
  Buffered or translated 2xx: completed durable outbox row before response commit
  Passthrough SSE or non-SSE 2xx: durable accepted row before first flush, then final payload update
  Captured non-2xx: bounded in-memory side-effect queue, then outbox attempt
  Eligible PlanningFailure or admission_exhausted: bounded in-memory side-effect queue, then outbox attempt
                                                         ↓
                              Background materializer transaction:
                                - Write request_logs rows
                                - Write audit_logs rows when audit was enabled
                                - Write usage_request_events and proxy-key usage
                                - Delete the durable outbox row
```

Unsupported routes and wrong methods are rejected before telemetry. Early request/planning errors such as malformed bodies, unknown models, and API-family mismatches do not carry `PlanningFailure` and therefore do not create synthetic history. Eligible target-resolution/translation planning failures, captured non-`2xx` activity, and admission activity can be lost before they reach the outbox, and a final all-transport-failures `502` is not currently covered by execution-failure telemetry. Operators must therefore not interpret retained history as a complete ledger of every transport failure.

### 7.3 Data Captured

- Profile ID attribution, requested model ID, final target model ID, api family, terminal-target compatibility ID, endpoint base URL, and endpoint description
- Prism `ingress_request_id`, per-request `attempt_number`, persisted ingress `operation_name`, additive `upstream_operation_name`, `operation_translation_mode`, `upstream_request_path`, and best-effort `provider_correlation_id`
- HTTP status code, response time (ms)
- Token usage (input, output, total), extracted by upstream operation response or stream hooks before any client-facing response translation
- Flat final-target attribution, including requested model, resolved target model, selected terminal target, endpoint, operation, upstream operation, translation mode, and sanitized upstream request path.
- Stream flag, ingress request path, sanitized upstream request path, error details

Request-log semantics are per-materialized attempt: one incoming runtime request can create multiple request-log rows when failover or retries occur. `ingress_request_id` groups those rows while `request_id` remains the unique identifier for one stored attempt row. Final usage ownership stays with the final returned response.

### 7.4 Query Capabilities

- Filter by model, final target model, caller client rule, endpoint, api family, status, and time range
- Aggregated statistics with grouping by model/api family/endpoint using stored endpoint label snapshots
- Pagination for request log listing

These query APIs intentionally remain product-facing retained-history surfaces for the UI and operators. Prism no longer exposes local metrics or tracing exporters.

## 8. Request Audit Logging

### 8.1 Concept

Audit logging records request-time provenance without changing routing choices or client-facing response translation. Before persistence, it redacts only the three configured upstream request authentication header names. Runtime snapshots load audit policy from `profile_api_family_audit_settings` by profile and model API family, then retain request-time booleans in the telemetry envelope. Materialization creates one audit row for each audited upstream attempt, including failover attempts, and metadata-only requests still create audit metadata when audit is enabled. Body capture is allowed only when audit is enabled for that family. The audited request body can be stored for each attempted upstream request; the response body is stored only for the final attempt. Translated OpenAI audit bodies remain upstream-native, never substituted with the rewritten client-facing request or response body.

### 8.2 Audit Flow

```
Client -> supported runtime operation
  -> Operation registry and planner resolve the attempt
  -> ProxyService forwards request to upstream
  -> Runtime captures audit metadata in the telemetry envelope
  -> Durable telemetry materialization, when reached:
       -> One audit row for each audited upstream attempt
       -> Redact the three configured request authentication headers and record connection metadata snapshots
       -> Link weakly to materialized request-log metadata when available
       -> Store immutable profile_id attribution
       -> Store request bodies when body capture is enabled
       -> Store the response body only for the final attempt
```

### 8.3 Non-Interference Guarantees

- Audit policy and body capture never change routing, target selection, or client-facing response adaptation.
- Audit materialization occurs with durable runtime telemetry rather than inline `INSERT`s in the proxy handler.
- Telemetry handoff has its own durability rules: buffered or translated successful responses require a completed outbox handoff before response commit; passthrough SSE and non-SSE success require an accepted outbox row before first flush and a later final update. Asynchronous captured non-`2xx` and eligible synthetic failure activity can be rejected or abandoned before materialization.

### 8.4 Redaction

Applied at write time before INSERT:

- Only upstream request-header names `authorization`, `x-api-key`, and `x-goog-api-key` are replaced with `[REDACTED]`, case-insensitively.
- Other upstream request headers and all upstream response headers are serialized as captured; Prism does not apply a pattern-based header scrubber.
- Captured body fields are not redacted and can contain sensitive user data, so body capture remains an explicit request-time setting.

### 8.5 Dedicated Audit Detail Page

The audit detail view is reached from request investigation at `/observe/requests/:requestId/audit`. It shows summary metadata, stored request headers/body, response status/headers/body, and connection identity fields. The three redacted request auth-header values remain redacted, but other stored header and body content can contain sensitive data. The request-log detail sheet remains overview-only and links to this page instead of loading audit payloads inline.

### 8.6 Upstream Response Encoding

Prism's upstream transport sets `DisableCompression: true` and removes the client `Accept-Encoding` header. Prism neither asks Go to automatically decompress upstream responses nor conditionally decompresses them for audit capture. Audit captures the response bytes actually read from the upstream response; a translated OpenAI response may then be adapted for the client separately.

## 9. Global Log Retention

### 9.1 Concept

Historical `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events` are partitioned by UTC day and managed by global log-retention jobs. Normal retention is instance-wide across all profiles. Default-profile filtering still applies to profile-owned list and detail APIs, but `X-Profile-Id` does not scope retention settings or retention jobs.

The visible Settings `实例` tab owns `/api/settings/log-retention` and can store a day count per retained dataset. Operators can also create an immediate retention job through `POST /api/maintenance/log-retention/jobs` with a table name plus either an explicit cutoff or `delete_all=true`.

Before the HTTP server starts, production ensures a 15-day horizon for every managed table: the current UTC day plus the next 14 days. The low-priority `log_partition_maintenance` worker refreshes that horizon hourly. Independently, the low-priority management-jobs worker runs every 5 seconds: it reads configured global retention-day policies, creates one idempotent daily job per configured table, and processes a due job.

### 9.2 Retention Job Flow

```
Operator -> Settings `实例` tab -> Log Retention
  -> Saves global day policies with PUT /api/settings/log-retention
  -> Starts POST /api/maintenance/log-retention/jobs
  -> Returns 202 with { job_id, state, status_url, scope }
  -> Sets Location to /api/management/jobs/{job_id}
  -> Background worker runs a durable log_retention job with profile_id = 0
  -> API callers can read or cancel the job through management job endpoints
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

Audit rows keep weak request metadata rather than a hard dependency on live request-log rows. Request-log retention does not clear `request_log_id`, `request_log_created_at`, or `ingress_request_id`; audit APIs report `request_log_missing=true` only when both request-log link fields are non-null and the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves. Request detail links can therefore be unavailable after request-log retention expires before audit-log retention.

Deleting or expiring request logs does not delete audit rows. Deleting or expiring audit rows does not affect request logs. Operators should treat request-to-audit linking as best-effort historical context, not as guaranteed referential availability.

### 9.5 Current Retention Boundaries

Partitioned retention manages the current log-table set only. Prism does not rewrite historical log storage shapes into the current partitions.

### 9.6 Frontend Placement

Log retention controls live on the visible Settings `实例` tab. The visible `全局` tab still manages costing, timezone, audit capture preferences, config rules, and other Default-profile state.

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
- **Network**: Prism does not terminate TLS and does not enforce a LAN-only boundary. Deployment exposure is the operator's responsibility: use firewall rules, reverse-proxy access controls, container or host network policy, and TLS termination appropriate to the environment.

## 13. Supported Runtime API Families

The runtime plane supports three fixed API families through the operation registry:

- **OpenAI** (`openai`): `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, and `openai.responses.compact`
- **Anthropic** (`anthropic`): `anthropic.messages` and `anthropic.count_tokens`
- **Gemini** (`gemini`): `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`

Models always carry required `api_family`; runtime compatibility does not depend on catalog metadata.
