# Architecture Document: Prism

## 1. System Overview

Client (5173) → Prism (Management APIs + Proxy Engine → PostgreSQL) → Providers (OpenAI / Anthropic / Gemini)

*Local `./start.sh` keeps frontend `5173` and PostgreSQL `15432` fixed, and follows the selected bootstrap file's backend port. Freshly seeded repo-local bootstrap files use backend port `8000`. Production uses the root single-image bundle.*

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
│   │   ├── provider/           # provider-native adapters and usage parsing
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
- Only after that validation does it bring down the root Compose PostgreSQL service for the launcher project, reclaim Prism's backend/frontend development ports, verify PostgreSQL port availability, start and wait for the root `docker-compose.yml` `postgres` service, start the backend, and finally start Vite for `full` mode. The backend follows `server.port` in the selected bootstrap file; fresh local seeds use `8000`. PostgreSQL and Vite remain fixed at `15432` and `5173`.
- `./start.sh full` launches the frontend on `5173`, unsets `VITE_API_BASE`, and enables a launcher-local Vite proxy via `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at that effective backend port so browser traffic stays same-origin.
- Canonical startup config lives in a plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; backend-native fresh seeds default the database URL to `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, while `start.sh` sets `DATABASE_URL` to the local launcher PostgreSQL DSN on host port `15432` before seeding.
- The root `Dockerfile` plus root `docker-compose.yml` are the only local/self-hosted bundle: Compose builds one Prism app image, runs PostgreSQL as a separate service, and the app image runs the Go backend behind Nginx with the React dashboard. Nginx's `PRISM_BACKEND_UPSTREAM_PORT` is a static environment substitution; it is not derived from the bootstrap file. Any container deployment that changes bootstrap `server.port` must set `PRISM_BACKEND_UPSTREAM_PORT` to the same port.
- The single app image runs as `prism:prism`, UID/GID `1000:1000`. Container deployments that bind mount `/app/config` or any other `PRISM_CONFIG_PATH` parent must make that host directory writable by UID/GID `1000:1000`; new and existing root-owned mounts should be prepared once with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.
- Bootstrap JSON is strict v1: `meta.schemaVersion` must be `1`, unknown fields are rejected, and `runtime.transport.requestTimeout` plus `runtime.sideEffects.attemptTimeout` are required positive durations. Legacy encrypted bootstrap fields (`secretPayload`, `database.urlSecretRef`, and `auth.jwtSigningKeySecretRef`) are rejected rather than migrated at boot. Missing files are seeded once, and the entrypoint has a narrow repair path for stale files rejected only because they contain retired `docsEnabled`; other invalid legacy shapes fail validation.
- Safe bootstrap snapshots expose non-secret values and secret metadata only. They never return secret material; `runtime.secretEncryptionKey` is preserve-only in v1 and its metadata is not editable through the bootstrap settings surface. File-backed startup edits require a process restart because Prism does not watch external config changes. Existing valid files are preserved until an operator stops Prism, removes or relocates the file, and restarts.
- `database.pools.realtime`, `auth.resetCodeTtlSeconds`, `runtime.routing.openaiTerminalTranslationMode`, `stateTransfer.bundleEncryptionKey`, `mail`, and `telemetry` remain parse-and-validate compatibility fields for live `config.json` files. They do not restore the removed realtime pool, reset-code TTL override, terminal-translation mode, state transfer, mail delivery, or telemetry exporter processes.
- The backend does not mount a local `/metrics` scrape endpoint.
- Request-history APIs and settings-page state flows remain PostgreSQL-backed product state instead of bootstrap telemetry ownership.
- Disaster recovery is handled outside the dashboard with `pg_dump` plus a copy of the plaintext startup config.
- `.github/workflows/docker-images.yml` builds only `ghcr.io/coachpo/prism` for `linux/arm64` on `v*` tags and `workflow_dispatch`; tag pushes require a green CI conclusion on the tagged commit. `.github/workflows/cleanup.yml` prunes only untagged single-image GHCR package versions.

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

Runtime telemetry has durable success handoffs, scheduled activity handoffs, and background materialization. Every provider-forwarded successful `2xx` response requires a durable `runtime_telemetry_outbox` row: buffered responses commit a completed envelope before the response is committed, while passthrough SSE and non-SSE responses commit an accepted row before the first flush and finalize that row after response capture completes. Captured non-`2xx` activity, telemetry-eligible target-resolution or native-compatibility planning failures carrying `PlanningFailure`, and `admission_exhausted` execution failures first use a bounded in-memory scheduler side-effect queue, which later attempts durable outbox insertion and can be lost if rejected, terminally failed, or abandoned during shutdown. A worker materializes accepted outbox rows into `request_logs`, `audit_logs`, `usage_request_events`, and proxy-key usage in one transaction before deleting the outbox row. Runtime feedback is separately and intentionally lossy under pressure; queue-full, invalid, closed, or store-failure cases drop feedback with accounting and never block proxy responses.

Audit and statistics reads are bounded. Raw audit lists require backend-enforced time windows and keyset cursors. `GET /api/stats/dashboard` still returns backend-computed `routing_health_map`, but the current dashboard adapter does not render it; the production Models table presents retained success rate, P95 latency, and 24-hour request count as text rather than health badges. The connection-success-rate API also exists without a current production UI consumer. Broad deletes run as durable management jobs.

Runtime cache correctness is generation-based. Management mutations advance durable runtime-cache generations in the same transaction as the primary state change, runtime reads validate generation vectors and refresh or fail closed when stale, and post-response cache warming is non-authoritative. Cache generation lag may delay warm snapshots, but auth-sensitive runtime reads reject stale or unverifiable snapshots instead of accepting old state.

## 3. Request Flow

Prism is proxy-first. It forwards only the provider-native operations registered in the runtime operation catalog, and it is not a full OpenAI, Anthropic, or Gemini API clone.

Global CORS handling runs before the runtime branch. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the operation registry in that order. The operation registry is the ingress contract inside the authenticated runtime handler. Each supported operation declares an exact HTTP method, path template, API family, model-binding source, streaming classification, canonical operation name, and `HookCollectionID`; it does not declare a provider adapter. The current canonical operation names are `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `anthropic.messages`, `anthropic.count_tokens`, `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`. Requests that do not match that registry are rejected before body reads, planning, provider transport, telemetry, audit, feedback, or durable side effects.

`GET /v1/models` is the exception: `openai.models` branches to the local models-list handler before provider request-body handling, planning, or provider execution core. Every other registered proxy operation enters the shared runtime and gateway path: it resolves against frozen Default profile id `1`, resolves ordered access targets, applies the attached Ban Policy strategy, claims local attempt state, builds an upstream request, and hands activity to telemetry seams. The provider adapter is selected during planned-upstream request construction, not registry resolution. Request, non-stream response, and stream hooks are looked up by `HookCollectionID`, allowing related operations such as token counting or compact Responses to use hook collections different from their canonical operation names. Those hooks own generation extraction and stream intent, non-stream parsing and token usage, and stream terminal classification and usage merge respectively.

OpenAI Chat Completions and Responses are operation-native. Planning requires the model's `openai_accepted_format` and the selected connection's `openai_text_capability` (`responses_only`, `chat_completions_only`, or `dual_native`) to support the ingress operation. Incompatible terminal attempts are skipped in authored order so the next target can be tried; planning rejection uses three stable, mutually distinguishable codes before provider transport: `400 openai_operation_not_supported` (root model does not accept the operation), `503 openai_no_compatible_terminal_target` (no capability-compatible Terminal leaf in the authored graph), and `503 openai_no_eligible_terminal_target` (compatible leaves exist but none is statically eligible). A statically eligible route that is dynamically unavailable keeps the ordinary `503` family. Current native attempts record `operation_translation_mode = "none"`; the columns and stats reads remain for historical rows. Responses adjunct operations require responses-capable targets.

Runtime observability stores canonical disjoint token components. Base input, cache-read input, cache-creation input, base output, and reasoning output are separate dimensions, while provider totals remain authoritative when supplied. Pricing uses five concrete pricing strings from the attached template snapshot, and explicit `"0"` component prices mean configured free pricing instead of a missing-price condition.

Terminal Target `openai_text_capability` remains connection-owned metadata used for native OpenAI operation-set coverage. Model-owned capability authoring, context-window preflight filtering, overflow-promotion routing, and sibling-operation translation have been removed; ordinary strategy selection now uses explicit Ban Policy routing families.

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
  -> Resolver orders the model's enabled Model Target stage and Terminal Target stage by (position, id)
  -> The attached strategy shapes the Model Target stage first: single keeps only its first row,
     fill-first walks that stage, and round-robin rotates its direct peers
  -> Each Model Target row recursively resolves through the child model's own strategy and
     contributes one contiguous block; capability or leaf misses leave the stage without a candidate
  -> Only when the Model Target stage has no eligible candidate does the resolver enter the
     Terminal Target stage and apply the same strategy independently to its direct rows
  -> Cycle/depth and missing-strategy errors fail closed
  -> Executor plans attempts against Terminal Targets
  -> Upstream responds; eventual request history keeps model_id as the requested model and resolved_target_model_id as the final target model for each materialized attempt
  -> Gateway returns response to client
```

Model Target rows and Terminal Target rows are stored in one access-target relation but are not runtime peers. The Model Target stage is evaluated first; the Terminal Target stage is entered only when the first stage has no eligible candidate for the operation. Diagnostics and UI copy expose this stage boundary, and `single` truncation is reported separately for each stage.

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

The local OpenAI models operation branches only on presence of the `client_version` query parameter. Ordinary callers retain the OpenAI `object`/`data` response. Codex clients receive the current model-catalog metadata with a stable weak ETag and exact-match `304` support, excluding `chat_completions_only` models. The store starts from the embedded catalog and refreshes asynchronously from the OpenAI Codex repository at startup and every 24 hours; failures retain the last good catalog. Both request branches read the frozen Default-profile runtime snapshot and never contact a configured model provider.

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

### 3.7 Custom Request Parameter Overlay

Terminal Targets can carry an optional static top-level JSON object (`connections.custom_request_parameters`). When any planned candidate for a request has a non-empty configuration, Prism must buffer the ingress body, verify it is a JSON object, and materialize a per-attempt upstream body before provider transport:

```
build_planned_terminal_attempts():
  1. For each candidate attempt, build the provider-native upstream request (adapter model/path rewrite)
  2. If the Connection has no custom_request_parameters: use the existing body unchanged
  3. Otherwise overlay O onto the native body B:
       R = deep_copy(B)
       for k in sorted(top_level_keys(O)): R[k] = deep_copy(O[k])
       re-encode R and enforce the 20 MiB effective-body limit
  4. Re-extract the attempt's generation-parameter snapshot from its own final body
  5. Mark the plan as requiring a replayable body (streaming request-body fast path disabled)
```

Overlay rules: non-conflicting client top-level fields are preserved verbatim; matching top-level keys are replaced wholesale (nested objects are never recursively merged); configured `null` is sent as literal JSON null; there is no delete-member syntax. `model`, `models`, `stream`, `messages`, `input`, `contents`, `instructions`, `system`, and `systemInstruction` are protected and can never appear in the configuration.

Body-dependent headers (`Content-Encoding`, `Content-MD5`, `Digest`, `Content-Digest`) are stripped from client headers, provider auth extras, and Connection `custom_headers` whenever an overlay re-encodes the body; `Content-Length` is recomputed from the merged body. Non-identity `Content-Encoding` cannot be re-encoded: Gemini path-bound operations with a configured candidate reject it with `415`, while body-bound OpenAI/Anthropic operations keep the existing `400` malformed-body path.

Buffering decisions:
- If any candidate has non-empty parameters, `requiresReplayableRequestBody` becomes true and `canStreamIncomingRequestBody` returns false; Gemini `streamGenerateContent` then uses the two-phase planning boundary (probe with `rawBody == nil` only decides the replayable requirement, never overlays or 400s; the second full plan reads the body and materializes the merged body).
- With no configured parameters anywhere, the existing request-body streaming fast path is preserved unchanged.
- Each attempt owns an immutable merged body; failover, retry, or hedge candidates never share mutable maps, slices, or buffers.

Fail-closed boundaries: non-object ingress fails with `400` before admission/transport; planning-snapshot compilation fails on invalid persisted data (cold start fails, hot refresh keeps the last-good snapshot); validation errors and logs never echo configuration values. Audit body capture stores each attempt's final merged body; request-log generation parameters are extracted per attempt from its final body.

### 3.8 Dashboard And Analytics REST Polling

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
- Access-target `position` remains the shared storage order for all rows and is not priority, tier, or weight; runtime filters that order into a Model Target stage and a Terminal Target stage.
- The Model Target stage is evaluated first. The Terminal Target stage is a fallback entered only when the model stage has no eligible candidate for the operation. `single`, `fill-first`, and `round-robin` apply independently within the entered stage; a Model Target row is an atomic parent peer whose child attempts stay one contiguous block and whose child keeps its own strategy and round-robin cursor.
- Model IDs are unique within a profile.
- The gateway may normalize provider request payloads before forwarding, for example rewriting the requested model ID to the final target model ID for upstream compatibility. Prism does not rewrite response-body model identity on the client-facing way back out.

Model contracts require `api_family`; runtime compatibility is checked against `api_family` only.

### 5.3 Resolution

```
resolve_access(profile_id, model_id):
  config = lookup(profile_id, model_id)
  model_targets = order_for_strategy(strategy, enabled_model_targets(config))
  model_result = resolve_model_stage(model_targets)  // Model row -> recursive child resolution
  if model_result has eligible terminal attempts:
    return model_result

  terminal_targets = order_for_strategy(strategy, enabled_terminal_targets(config))
  terminal_result = resolve_terminal_stage(terminal_targets)
  if terminal_result has eligible terminal attempts:
    return terminal_result
  if terminal_result has a compatibility error:
    return terminal_result.compatibility_error
  if model_result has a compatibility error:
    return model_result.compatibility_error
  return no_eligible_target
```

The three strategies act independently per entered stage: Model Targets are evaluated first, and Terminal Targets are evaluated only when the Model Target stage has no eligible candidate. Within a stage, `single` keeps only the first enabled row, `fill-first` walks that stage's authored order, and `round-robin` rotates direct peers once per request (each row occupies one cursor slot; a child model's expanded attempts do not enlarge the parent modulus, and the child claims its own cursor keyed by its own source model, strategy, and target-set hash). An enabled but currently unavailable or incompatible row still consumes its round-robin slot; eligibility is judged after rotation. Reordering, adding, removing, or toggling rows changes the target-set hash, and a new hash starts a fresh cursor while a reappearing identical hash may continue its existing cursor. Cursors are process-local.

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
  Buffered 2xx: completed durable outbox row before response commit
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

Unsupported routes and wrong methods are rejected before telemetry. Early request/planning errors such as malformed bodies, unknown models, and API-family mismatches do not carry `PlanningFailure` and therefore do not create synthetic history. Eligible target-resolution or native-compatibility planning failures, captured non-`2xx` activity, and admission activity can be lost before they reach the outbox, and a final all-transport-failures `502` is not currently covered by execution-failure telemetry. Operators must therefore not interpret retained history as a complete ledger of every transport failure.

### 7.3 Data Captured

- Profile ID attribution, requested model ID, final target model ID, api family, terminal-target compatibility ID, endpoint base URL, and endpoint description
- Prism `ingress_request_id`, per-request `attempt_number`, persisted ingress `operation_name`, additive `upstream_operation_name`, `operation_translation_mode`, `upstream_request_path`, and best-effort `provider_correlation_id`
- HTTP status code, response time (ms)
- Token usage (input, output, total), extracted by native upstream operation response or stream hooks
- Flat final-target attribution, including requested model, resolved target model, selected terminal target, endpoint, operation, upstream operation, current-or-historical translation mode, and sanitized upstream request path.
- Stream flag, ingress request path, sanitized upstream request path, error details

Request-log semantics are per-materialized attempt: one incoming runtime request can create multiple request-log rows when failover or retries occur. `ingress_request_id` groups those rows while `request_id` remains the unique identifier for one stored attempt row. Final usage ownership stays with the final returned response.

### 7.4 Query Capabilities

- Filter by model, final target model, caller client rule, endpoint, api family, status, and time range
- Aggregated statistics with grouping by model/api family/endpoint using stored endpoint label snapshots
- Pagination for request log listing

These query APIs intentionally remain product-facing retained-history surfaces for the UI and operators. Prism no longer exposes local metrics or tracing exporters.

## 8. Request Audit Logging

### 8.1 Concept

Audit logging records request-time provenance without changing routing choices or client-facing response handling. Before persistence, it redacts only the three configured upstream request authentication header names. Runtime snapshots load audit policy from `profile_api_family_audit_settings` by profile and model API family, then retain request-time booleans in the telemetry envelope. Materialization creates one audit row for each audited upstream attempt, including failover attempts, and metadata-only requests still create audit metadata when audit is enabled. Body capture is allowed only when audit is enabled for that family. The audited request body can be stored for each attempted upstream request; the response body is stored only for the final attempt.

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
- Telemetry handoff has its own durability rules: buffered successful responses require a completed outbox handoff before response commit; passthrough SSE and non-SSE success require an accepted outbox row before first flush and a later final update. Asynchronous captured non-`2xx` and eligible synthetic failure activity can be rejected or abandoned before materialization.

### 8.4 Redaction

Applied at write time before INSERT:

- Only upstream request-header names `authorization`, `x-api-key`, and `x-goog-api-key` are replaced with `[REDACTED]`, case-insensitively.
- Other upstream request headers and all upstream response headers are serialized as captured; Prism does not apply a pattern-based header scrubber.
- Captured body fields are not redacted and can contain sensitive user data, so body capture remains an explicit request-time setting.

### 8.5 Dedicated Audit Detail Page

The audit detail view is reached from request investigation at `/observe/requests/:requestId/audit`. It shows summary metadata, stored request headers/body, response status/headers/body, and connection identity fields. The three redacted request auth-header values remain redacted, but other stored header and body content can contain sensitive data. The request-log detail sheet remains overview-only and links to this page instead of loading audit payloads inline.

### 8.6 Upstream Response Encoding

Prism's upstream transport sets `DisableCompression: true` and removes the client `Accept-Encoding` header. Prism neither asks Go to automatically decompress upstream responses nor conditionally decompresses them for audit capture. Audit captures the response bytes actually read from the native upstream response.

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

See section 15 (Data Model Reference) for the complete schema.

## 11. API Design

See section 14 (API Reference) for the complete API documentation.


## 12. Security Considerations

- **Operator Authentication**: Optional cookie-backed authentication for management APIs (`/api/*`). Supports username/password.
- **Proxy API Keys**: Optional API key enforcement for supported runtime operations mounted under `/v1` and `/v1beta`. Keys are issued and managed through the dashboard.
- **Auth Bifurcation**: Management auth (session cookies) and runtime auth (proxy API keys) are separate enforcement paths.
- **Data at Rest**: API keys and secrets are stored in PostgreSQL. Endpoint secrets are encrypted at rest.
- **CORS**: Local browser traffic stays same-origin through the launcher-local Vite proxy in `full` mode; production single-image traffic is served from the same origin.
- **Network**: Prism does not terminate TLS and does not enforce a LAN-only boundary. Deployment exposure is the operator's responsibility: use firewall rules, reverse-proxy access controls, container or host network policy, and TLS termination appropriate to the environment.

## 13. Supported Runtime API Families

The runtime plane supports three fixed API families through the operation registry:

- **OpenAI** (`openai`): `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, and `openai.responses.compact`
- **Anthropic** (`anthropic`): `anthropic.messages` and `anthropic.count_tokens`
- **Gemini** (`gemini`): `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`

Models always carry required `api_family`; runtime compatibility does not depend on catalog metadata.

## 14. API Reference


Local `./start.sh` backend base URL follows the selected bootstrap file's `server.port`; fresh repo-local bootstrap seeds use `http://localhost:8000`.

Container and custom deployments use the listener configured in the plaintext bootstrap file. The root single-image Docker bundle publishes Nginx on `http://localhost:8080` by default and proxies to the private backend upstream on port `8000`.

Prism does not expose a backend-local `/metrics` operations endpoint or start telemetry exporters. The startup `telemetry` block is parsed for existing `config.json` compatibility only. The retained `/api/stats/*` routes remain product-facing request-history and aggregate APIs.

### 0. Profile Context Semantics
- Prism has three route classes:
  - Global management routes, which omit `X-Profile-Id`.
  - Profile-scoped management routes, which accept `X-Profile-Id` but ignore its value and resolve against Default profile id `1`.
  - Runtime proxy routes, which resolve against frozen Default profile id `1` and ignore management scope overrides.
- Proxy endpoints (`/v1/*`, `/v1beta/*`) always resolve against frozen Default profile id `1` and ignore management scope overrides.
- Global management routes include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Management job routes `/api/management/jobs*` are low-priority management routes: list resolves Default-profile jobs, while read and cancel resolve Default-profile jobs and can fall back to global log-retention jobs by ID. The frontend does not treat them as `X-Profile-Id`-scoped routes.
- Profile-scoped management routes include `/api/config/header-blocklist-rules*`, `/api/config/user-agent-client-rules*`, `/api/settings/costing`, `/api/settings/timezone`, `/api/settings/audit`, `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, `/api/models/*`, `/api/endpoints/*`, `/api/pricing-templates*`, and `/api/connections/*`.
- Detail endpoints return `404` when a resource exists outside Default profile id `1`.
- Scope-control failures return structured JSON with `code` and `detail`, where `code` is stable for machine handling and `detail` is safe to show to operators.


### 1. Management API (`/api/*`)

#### 1.0 Request Boundary

Global CORS handling runs before both management and runtime branches and answers qualifying preflight requests with `204` before route middleware runs.

For management routes, the mounted request path applies runtime-cache invalidation handling, management admission, request-body limits, management-session authentication, and then the mounted management router. `POST`, `PUT`, and `PATCH` requests under `/api/auth/*` are limited to `64 KiB`; other mutating management requests are limited to `1 MiB`. The limit wrapper observes reads from the request body. If a downstream handler reads past the limit, Prism replaces its response with `413`:
```json
{
  "error": "request_body_too_large",
  "message": "Request body exceeds the maximum allowed size.",
  "limit_bytes": 65536
}
```

The wrapper is installed before management authentication, but authentication or admission can reject a request before any body read. For example, an oversized unauthenticated request to a protected management route can return its normal `401` authentication response rather than `413`.

When operator auth is enabled, the exact public management paths are `GET /api/auth/status`, `GET /api/auth/public-bootstrap`, `POST /api/auth/login`, `POST /api/auth/logout`, and `POST /api/auth/refresh`. Other `/api/*` paths, including `GET /api/auth/session`, require a valid access-token cookie. Auth is bypassed for all management routes while operator auth is disabled.

#### 1.0A Startup Config File

Prism loads steady-state startup settings from the plaintext `config.json` selected by `PRISM_CONFIG_PATH`. The live v1 file requires `meta`, `server`, `database.url`, `database.pools`, `database.managementAdmission`, `runtime.secretEncryptionKey`, `runtime.transport`, `runtime.sideEffects`, `http.corsAllowedOrigins`, and `auth`. Optional top-level sections are `alerting`, `mail`, `telemetry`, and `stateTransfer`; `mail`, `telemetry`, and `stateTransfer` are parsed for compatibility only and do not re-enable retired behavior. R2 removed the management API for editing that file; external edits require a Prism restart before they affect the running process.

#### 1.1 Profiles

The profile management API is frozen. Prism preserves the `profiles` table and all `profile_id` storage columns, but no longer exposes `/api/profiles*` management routes. Profile-scoped management APIs are pinned to Default profile id `1`.

---

#### 1.2 Catalog Management

Prism no longer exposes a catalog management product surface. Model compatibility is carried by each model's required `api_family`; catalog metadata does not participate in runtime routing.

---

#### 1.3 Model Configurations

##### List Models
```
GET /api/models
```
Response `200`: Array of model objects.

##### Get Model
```
GET /api/models/{id}
```
Response `200`: Full model object with required `api_family`, optional `loadbalance_strategy_id`, ordered `access_targets` (the storage rows retain a shared zero-based `position`, while runtime resolves Model Target and Terminal Target stages separately), and attached Terminal Target summaries in the effective profile scope. Model create/update does not author access targets; use `/api/models/{id}/targets` for Model Target authoring and model-owned Terminal Target ownership edges.

##### Get Models by Endpoints (Batch)
```
POST /api/models/by-endpoints
```
Request:
```json
{
  "endpoint_ids": [1, 2, 3]
}
```
Response `200`: `items[]`, where each item contains an `endpoint_id` and the models that can reach that endpoint through Terminal Targets. Endpoints are reusable and may be referenced by Terminal Targets owned by different models.

##### Get Models by Endpoint
```
GET /api/models/by-endpoint/{endpoint_id}
```
Response `200`: Array of models that can reach the endpoint through Terminal Targets within the effective profile scope.

##### Create Model
```
POST /api/models
```
Request:
```json
{
  "api_family": "openai",
  "model_id": "gpt-4o-public",
  "display_name": "GPT-4o Public",
  "loadbalance_strategy_id": 7,
  "openai_accepted_format": "dual_native",
  "is_enabled": false,
  "initial_terminal_target": {
    "endpoint_id": 1,
    "name": "Primary",
    "is_active": true,
    "openai_text_capability": "dual_native",
    "pricing_template_id": 2,
    "qps_limit": 10,
    "custom_headers": {},
    "custom_request_parameters": {}
  }
}
```
Response `201`:
```json
{
  "model": { "...": "created model object" },
  "configuration_warnings": []
}
```
Create supports an optional `initial_terminal_target` (composite create): `endpoint_id` XOR inline `endpoint_create` (`name`, `base_url`, `api_key`), plus name, active, auth type, capability, pricing, limits, custom headers and validated `custom_request_parameters`. The nested target never receives `api_family`, owner/profile, priority/position or IDs. Composite create runs in one transaction (model + optional inline endpoint with encrypted key + private Connection + enabled owner Access Target); any failure rolls back completely. `openai_text_capability` defaults to the owner `openai_accepted_format`; `is_active` defaults to `true`; with a target and no explicit `is_enabled`, the model defaults to enabled. An enabled model with an inactive nested target is rejected `422 model_initial_target_inactive`; an enabled model without any target stays `400 model_no_enabled_targets`. Legal Partial/None coverage succeeds with structured `configuration_warnings`; warnings are non-persisted and never echo secrets.

Validation rules:
- `model_id` must be unique within the effective profile scope.
- `api_family` is required on every model contract and remains the authoritative runtime compatibility field.
- `is_enabled` defaults to `false` when omitted. Enabling a model still requires at least one enabled access target in the stored graph.
- Create and update payloads reject `access_targets`, exact-facade fields, and retired model-owned context capability fields.
- Ordered same-profile, same-`api_family` model targets are managed through `/api/models/{id}/targets`. OpenAI capability coverage is operation-set based: valid Partial/None relations may be persisted with structured warnings, while invalid enum/nullability and family/profile/cycle violations remain hard errors. First-party UI disables zero-intersection choices but does not enforce exact mode equality for hand-written API requests.
- Model target self-reference and target cycles are rejected by the target management routes.
- Deleting a model referenced by another model target returns `409` until the target rows are removed or updated. Deleting an owner model deletes its Terminal Targets with the owning target rows.
- Model create/update responses are envelopes: `{ "model": ModelDetail, "configuration_warnings": [] }`.

##### Routing Diagnostics
```
GET /api/models/{model_config_id}/routing-diagnostics
```
Read-only, profile-scoped, `invalidates_planning=false`. Returns the authoritative two-stage static routing diagnostics for the model: `strategy`, `accepted_operations`, `stages` (`model_targets` first, `terminal_targets` fallback with `entered_when` semantics), per-target rows (authored stage position, enabled strategy index, coverage, per-operation dispositions `candidate | disabled | inactive | incompatible | no_eligible_leaf | truncated_by_single | structural_error`), `operation_coverage` (accepted, capability_covered, statically_routable, resolved_stage, compatible/access target ids) and `configuration_warnings`. OpenAI responses always carry the four registered model-bound operations; root-unaccepted operations have `accepted=false`. The analyzer is pure and static: it never reads or changes Ban/retry state, QPS/in-flight counters, current-state, round-robin cursors or endpoint secrets, and never sends provider requests. Diagnostics never echo endpoint keys or header/parameter values.

```
POST /api/models/{model_config_id}/routing-diagnostics/preview
```
Read-only overlay preview for the Model Edit dialog: body accepts optional `openai_accepted_format`, `loadbalance_strategy_id`, `is_enabled` (at least one required, unknown fields hard-rejected), overlays them on the committed graph and returns the same diagnostics DTO without writing anything.

`GET /api/models` embeds a compact `routing_summary` per model (`enabled_access_target_count`, `total_access_target_count`, `coverage`, `operation_groups`, `single_truncated_stages`, `warning_codes`) computed in one bounded batch; the frontend never re-derives coverage or eligibility.

##### Update Model
```
PUT /api/models/{id}
```
Request (all fields optional):
```json
{
  "api_family": "openai",
  "model_id": "gpt-4o-public-updated",
  "display_name": "GPT-4o Public (Updated)",
  "loadbalance_strategy_id": 9,
  "is_enabled": true
}
```
Update payloads use the same field contract as create and do not mutate access targets. Existing access targets and private Terminal Targets are preserved and remain managed by model-scoped target and connection routes. Response `200`: Updated model object. Returns `409` if `model_id` conflicts within the effective profile. A valid `openai_accepted_format` change is saved even when existing targets become Partial or None coverage; the response includes non-persisted `configuration_warnings` for the proposed state. Invalid enum/nullability and graph or profile violations remain hard errors.

##### Delete Model
```
DELETE /api/models/{id}
```
Response `200`: `{ "deleted": true }`. Returns `409` if other models still reference this model through model targets. When deletion succeeds, the owner model's private connection rows and their internal owning access-target rows are removed in the same operation.

---

#### 1.4 Endpoints (Profile-Scoped Credentials)

##### List Endpoints
```
GET /api/endpoints
```
Response `200`: Array of endpoint objects in the effective profile scope, ordered by `position ASC, id ASC`.

##### List All Connections (Dropdown)
```
GET /api/endpoints/connections
```
Response `200`: `{ "items": [...] }` containing connection summary rows for dropdown consumers.

##### Create Endpoint

```
POST /api/endpoints
```
Request:
```json
{
  "name": "Primary OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-abc123..."
}
```
Response `201`: Created endpoint object.

##### Duplicate Endpoint
```
POST /api/endpoints/{id}/duplicate
```
Response `201`: Created endpoint copy with a generated duplicate-safe name.

##### Update Endpoint
```
PUT /api/endpoints/{id}
```
Request:
```json
{
  "name": "Updated OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-new-key..."
}
```
Response `200`: Updated endpoint object.

##### Move Endpoint Position
```
PATCH /api/endpoints/{id}/position
```
Request:
```json
{
  "to_index": 0
}
```
Response `200`: Ordered array of endpoint objects after the move.

Behavior:
- `to_index` must be in the range `0..(endpoint_count - 1)` or the API returns `422`.
- A no-op move returns the current ordered list unchanged.
- The backend rewrites endpoint positions to contiguous `0..N-1` values after every successful move.

##### Delete Endpoint
```
DELETE /api/endpoints/{id}
```
Response `200`: `{ "deleted": true }`.
When connections still reference this endpoint, delete returns a typed `409 endpoint_in_use` with `endpoint_id` and a `references` array of direct Terminal Target ownership rows (connection id, access target id, terminal target name, model id/display name, api family, authored stage position, enable/active state, capability, pricing template, custom header count, custom request-parameter count). The same reference DTO is served by the batch read:
```
POST /api/endpoints/references/batch
{"endpoint_ids": [1, 2]}
```
which returns `items` in input order (1..100 unique ids); missing or cross-profile ids produce a typed `404` with the missing ids. If the locked recheck finds no references, delete proceeds. After a successful delete, later endpoints in the same profile are compacted so `position` remains contiguous. References never include endpoint API keys or header/parameter values.

#### 1.5 Terminal Targets and Model Access Targets

Terminal Targets are Prism's product term for model-private endpoint bindings within one profile. Terminal Targets are represented as `connections` / `connection_id` in the compatibility API and database schema. A compatibility connection carries its owner model's `api_family`, endpoint reference or inline endpoint create payload, pricing template, and optional admission limits. Endpoints remain reusable, so many Terminal Targets may point at the same endpoint. `model_access_targets.target_type="connection"` is an internal ownership and runtime routing edge, not a public assignment surface for connection IDs.

##### List Terminal Targets Through `/api/connections`
```
GET /api/connections
```
Response `200`: Array of compatibility connection objects in the effective profile. This is a read surface for Terminal Targets. Public `/api/connections` mutation routes reject writes and direct operators to model detail.

##### Get Terminal Target Through `/api/connections/{connection_id}`
```
GET /api/connections/{connection_id}
```
Response `200`: Single compatibility connection object in the effective profile. Returns `404` when the connection does not exist in that profile.

##### List Terminal Targets Attached to Models
```
POST /api/models/connections/batch
```
Request:
```json
{
  "model_config_ids": [1, 2, 3]
}
```
Response `200`: `items[]`, where each item contains a `model_config_id` and the Terminal Targets owned by that model's enabled or disabled internal connection targets, ordered by target position.

##### List Terminal Targets For One Model
```
GET /api/models/{model_config_id}/connections
```
Response `200`: Ordered array of Terminal Targets owned by the given model in the effective profile.

##### Create Terminal Target
```
POST /api/models/{model_config_id}/connections
```
Request (using existing endpoint):
```json
{
  "endpoint_id": 1,
  "is_active": true,
  "name": "Primary production key",
  "custom_headers": {
    "X-Custom-Org": "org-123"
  },
  "custom_request_parameters": {
    "provider": {
      "only": ["deepinfra/turbo"],
      "allow_fallbacks": false
    }
  },
  "openai_text_capability": "responses_only",
  "pricing_template_id": 2,
  "qps_limit": 3,
  "max_in_flight_non_stream": 6,
  "max_in_flight_stream": 2
}
```
Request (inline endpoint creation):
```json
{
  "endpoint_create": {
    "name": "New Endpoint",
    "base_url": "https://api.openai.com",
    "api_key": "sk-abc123..."
  },
  "is_active": true,
  "name": "Regional fallback",
  "openai_text_capability": "dual_native",
  "pricing_template_id": null,
  "qps_limit": null,
  "max_in_flight_non_stream": null,
  "max_in_flight_stream": null
}
```
Response `201`:
```json
{
  "connection": { "...": "created connection" },
  "access_targets": [ { "id": 42, "target_type": "connection", "connection_id": 15, "terminal_target_id": 15, "position": 0, "is_enabled": true } ],
  "configuration_warnings": []
}
```
Owner-scoped Connection create/update/delete responses are fixed envelopes carrying the canonical access-target rows (row id, target discriminator, connection id) and `configuration_warnings` computed on the proposed state. Warnings use stable codes (`openai_target_incompatible`, `openai_target_partial_coverage`, `openai_operation_uncovered`, `single_strategy_truncates_targets`) with severity and operation lists; they are non-persisted and never echo endpoint keys or header/parameter values.

Create semantics:
- Exactly one of `endpoint_id` or `endpoint_create` is required.
- The connection `api_family` is derived from the owner model. A conflicting request value is rejected.
- `priority` is rejected with `422`; Terminal Target ordering for a model is owned by `/api/models/{model_config_id}/targets` positions.
- Limiter fields are optional. `null` means unlimited. Positive integers apply per-connection request admission limits.
- `openai_text_capability` is the OpenAI text runtime capability source of truth for OpenAI-family Terminal Targets. It accepts `responses_only`, `chat_completions_only`, or `dual_native`, is required for OpenAI rows, and is compared with the owner model's accepted operation set. Non-OpenAI rows must omit it or persist `null`. Valid Partial/None coverage is accepted with non-persisted `configuration_warnings`; invalid enum/nullability remains a hard `422`.
- `custom_request_parameters` is an optional static top-level JSON object (`object | null`). Missing, `null`, and `{}` all persist as unconfigured (`NULL`); a non-empty object is validated (protected keys, 64 KiB compact limit, depth ≤ 16, members ≤ 256, safe integers) and canonicalized before write. Invalid values return `422` with `{"detail":"Invalid custom request parameters","field":"custom_request_parameters","path":...,"reason":...,"limit":...}`; malformed request JSON or unknown fields keep the generic `400`.

##### Update Terminal Target
```
PATCH /api/models/{model_config_id}/connections/{connection_id}
```
Request: Mutable compatibility connection metadata: `endpoint_id`, `endpoint_create`, `is_active`, `name`, `auth_type`, `custom_headers`, `custom_request_parameters`, `openai_text_capability`, `pricing_template_id`, `qps_limit`, `max_in_flight_non_stream`, `max_in_flight_stream`.

`custom_request_parameters` is a presence-aware whole-value replace: omitting the field keeps the current value, `null`/`{}` clears it to `NULL`, and a non-empty valid object replaces it wholesale; any violation fails the whole PATCH atomically.

`endpoint_create` is supported on update and is mutually exclusive with `endpoint_id`. `priority` is rejected with `422`. The owner model and connection `api_family` are immutable.

Response `200`: Updated Terminal Target object, represented as a compatibility connection. Public `PUT` or `PATCH /api/connections/{connection_id}` rejects mutation requests.

##### List Terminal Target References
```
GET /api/connections/{connection_id}/references
```
Response `200`: Owner references for the Terminal Target, wrapped with the requested compatibility `connection_id`. A valid connection has one owner:
```json
{
  "connection_id": 15,
  "items": [
    {
      "target_id": 42,
      "model_config_id": 7,
      "model_id": "gpt-4o",
      "api_family": "openai",
      "position": 0,
      "is_enabled": true
    }
  ]
}
```

##### Update Terminal Target Pricing Template

Pricing templates are assigned through the Terminal Target update route by setting `pricing_template_id`. Public connection-level pricing-template mutation routes reject writes.

##### Delete Terminal Target
```
DELETE /api/models/{model_config_id}/connections/{connection_id}
```
Response `200`: `{ "deleted": true, "access_targets": [], "configuration_warnings": [] }`.

##### Copy Terminal Target to Multiple Models
```
POST /api/models/{source_model_config_id}/connections/{connection_id}/copies
```
Request:
```json
{
  "destination_model_config_ids": [8, 9],
  "enable_copies": false
}
```
Response `201`:
```json
{
  "source_connection_id": 15,
  "items": [
    {
      "model_config_id": 8,
      "connection_summary": { "id": 25, "name": "Primary", "endpoint_id": 1, "is_active": true, "openai_text_capability": "dual_native", "pricing_template": null, "qps_limit": null, "max_in_flight_non_stream": null, "max_in_flight_stream": null, "custom_header_count": 0, "custom_request_parameter_count": 0 },
      "access_target": { "id": 52, "target_type": "connection", "connection_id": 25, "terminal_target_id": 25, "position": 0, "is_enabled": false }
    }
  ],
  "configuration_warnings": []
}
```
Batch copy is one transaction (all-or-nothing): destinations must be 1..100 unique positive ids, same profile and API family; the source connection must belong to the path owner. Copied fields are endpoint reference, name, active, auth type, capability, pricing reference, the three limits, and validated deep copies of custom headers and `custom_request_parameters`. IDs, positions, timestamps and all runtime state (Ban/retry/QPS window/in-flight/last success/latency/round-robin cursor) are never copied. New Access Targets append to the destination's global tail and default to `is_enabled = enable_copies` (`false` when omitted); the destination model's own enable state is never changed. `connection_summary` is redacted (counts only, including `custom_request_parameter_count`). Stable errors: `400 invalid_terminal_target_copy_destinations`, `422` over 100 destinations, `404 terminal_target_not_found` / `terminal_target_copy_destination_not_found`, `409 terminal_target_copy_api_family_conflict`.

Deletes the Terminal Target and its internal owner access-target row together, subject to enabled-model target validation. Public `DELETE /api/connections/{connection_id}` rejects mutation requests.

Rejected legacy mutation routes return `400` with guidance to use model-scoped Terminal Target routes instead: `POST /api/connections`, `PUT/PATCH/DELETE /api/connections/{connection_id}`, `PUT /api/connections/{connection_id}/pricing-template`, `PUT /api/models/{model_config_id}/connections/{connection_id}`, `PUT /api/models/{model_config_id}/connections/{connection_id}/pricing-template`, and `PATCH /api/models/{model_config_id}/connections/{connection_id}/priority`.

##### Model Target Routes
```
GET /api/models/{model_config_id}/targets
POST /api/models/{model_config_id}/targets
PUT /api/models/{model_config_id}/targets/{target_id}
PATCH /api/models/{model_config_id}/targets/{target_id}
PATCH /api/models/{model_config_id}/targets/{target_id}/position
DELETE /api/models/{model_config_id}/targets/{target_id}
```

Model target rows define a model's ordered access graph. Public authoring creates same-family model targets only; internal connection rows (Terminal Targets) share the same storage list and position space, while runtime evaluates the two target types as separate stages:
```json
{
  "target_type": "model",
  "target_model_id": "gpt-4o-backup",
  "position": 0,
  "is_enabled": true
}
```

Target semantics:
- Public `POST /api/models/{model_config_id}/targets` accepts `target_type="model"` with exact `target_model_id`, `position`, and `is_enabled`. Obsolete `weight` and `target_priority` keys reject on create, update, and patch payloads.
- Runtime routing consumes exact target-model IDs only. Target payloads do not accept regex matcher fields, capability-metadata expansion, weighted policy names, or hidden priority fields.
- Public target authoring rejects submitted `target_type="connection"`, `connection_id`, or `target_connection_id` values. Private connections are created and managed through `/api/models/{model_config_id}/connections`.
- `PUT` and `PATCH /api/models/{model_config_id}/targets/{target_id}` update target metadata within the owning model scope. For internal connection targets, `PATCH` accepts only `position` and `is_enabled`; pointer fields are immutable and obsolete weight fields must stay omitted.
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position` is the dedicated move route and accepts `to_index`. `to_index` is the zero-based index of the complete stored list; the response returns the full reordered list. The UI exposes stage-local controls and does not imply that this storage order changes the Model-then-Terminal runtime boundary.
- Existing internal `target_type="connection"` rows identify the source model that owns a private connection and provide the runtime terminal routing edge.
- Target positions are contiguous starting at `0` across both target types and provide the authored order within each runtime stage after type filtering. Position is an ordering key only, not a priority, tier, or weight replacement. Enable/disable never moves a row; delete compacts positions across both types; creates append to the shared storage tail unless an explicit position inserts at that global index.
- Target validation is Default-profile scoped, same-family, enabled-target aware, and cycle-safe.

##### Base URL Validation

On endpoint create (`POST`) and update (`PUT`), the `base_url` is:
1. **Normalized**: Trailing slashes are stripped (e.g., `https://api.example.com/` → `https://api.example.com`)
2. **Validated**: Rejected with HTTP 422 if scheme/host is missing or the URL includes a query string or fragment.
3. **Joined at runtime**: Path prefixes are allowed. Runtime appends the allowlisted operation path to the normalized endpoint path without version-segment de-duplication.

Valid examples:
- ✅ `https://api.openai.com`
- ✅ `https://api.openai.com/v1`
- ✅ `https://generativelanguage.googleapis.com`
- ✅ `https://generativelanguage.googleapis.com/v1beta`
- ❌ `https://api.openai.com/v1?timeout=30`
- ❌ `https://api.openai.com/v1#runtime`

#### 1.6 Pricing Templates

##### List Pricing Templates
```
GET /api/pricing-templates
```
Response `200`: Array of pricing template list items in the effective profile scope.

##### Get Pricing Template
```
GET /api/pricing-templates/{id}
```
Response `200`: Pricing template object in the effective profile scope.
Returns `404` when the template does not exist in the effective profile.

##### Create Pricing Template
```
POST /api/pricing-templates
```
Request:
```json
{
  "name": "GPT-4o Standard",
  "description": "Default OpenAI pricing",
  "pricing_currency_code": "USD",
  "input_price": "5.00",
  "output_price": "15.00",
  "cached_input_price": "2.50",
  "cache_creation_price": "0",
  "reasoning_price": "15.00"
}
```
Response `201`: Created pricing template object.

Pricing templates use five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`. Create and update ingress normalizes missing, `null`, empty, and whitespace-only values for any of those five fields to `"0"` before decimal validation. Explicit `"0"` is configured free pricing. It is not missing price data. `MISSING_PRICE_DATA` is reserved for absent, unusable, or invalid pricing snapshots, or for required FX data that cannot be applied.

##### Import Pricing Templates
```
POST /api/pricing-templates/import
```
Request:
```json
{
  "mode": "upsert_by_name",
  "templates": [
    {
      "name": "gpt-4o",
      "pricing_unit": "PER_1M",
      "pricing_currency_code": "USD",
      "input_price": "2.5",
      "output_price": "10",
      "cached_input_price": "1.25",
      "cache_creation_price": "0",
      "reasoning_price": "0",
      "description": "OpenAI GPT-4o"
    }
  ]
}
```
Response `200`: `{ "created": 1, "updated": 0, "skipped": [], "errors": [] }`.

`mode` is either `upsert_by_name` or `create_only`. Imports are Default-profile scoped and use one transaction: validation errors return `400` with row-level `errors[]`, and no templates are created or updated.

##### Update Pricing Template
```
PUT /api/pricing-templates/{id}
```
Request: Full replacement for mutable pricing template fields. Missing, `null`, empty, and whitespace-only price component fields normalize to `"0"` before validation. Optional `expected_updated_at` is an RFC3339 optimistic-concurrency guard; when supplied, the backend returns `409` if it does not match the current row `updated_at`.
Response `200`: Updated pricing template object.

##### Delete Pricing Template
```
DELETE /api/pricing-templates/{id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the template is still referenced by Terminal Targets; response `detail` includes a compatibility `connections` array with dependency details.

##### List Terminal Targets Using Template
```
GET /api/pricing-templates/{id}/connections
```
Response `200`: Usage payload with `template_id` and `items[]` (`connection_id`, `connection_name`, `model_config_id`, `model_id`, `endpoint_id`, `endpoint_name`).
---

#### 1.7 Settings API

##### Get Auth Settings
```
GET /api/settings/auth
```
Response `200`: Global operator-auth settings (`auth_enabled`, `username`, `has_password`, `proxy_key_limit`).

##### Update Auth Settings
```
PUT /api/settings/auth
```
Request fields:
- `auth_enabled`
- `username`
- `password`

Lifecycle contract:
- Disabling auth clears the current browser cookies in the response and invalidates stale management sessions immediately.
- Changing the operator username or password invalidates stale management sessions immediately, even when auth remains enabled.
- After invalidation, `GET /api/auth/session` returns `401`, while `GET /api/auth/status` continues to report the live global auth mode.

##### Proxy API Keys
```
GET /api/settings/auth/proxy-keys
POST /api/settings/auth/proxy-keys
PATCH /api/settings/auth/proxy-keys/{id}
POST /api/settings/auth/proxy-keys/{id}/rotate
DELETE /api/settings/auth/proxy-keys/{id}
```

Proxy-key lifecycle contract:
- List responses are arrays of proxy-key items with `id`, `name`, `key_prefix`, `key_preview`, `is_active`, `expires_at`, `last_used_at`, `last_used_ip`, `notes`, `rotated_from_id`, `created_at`, and `updated_at`.
- Create accepts `name`, optional `notes`, and optional RFC3339 `expires_at`. Response `201` is `{ "key": "<one-time-secret>", "item": { ... } }`.
- Update requires a non-empty `name` and accepts optional `notes`, `is_active`, and RFC3339 `expires_at`. Response `200` is the updated item. Omitted or JSON `null` `expires_at` preserves the current expiry; update does not expose a clear-expiry operation.
- Rotate is lineage-creating, not in-place mutation: the historical row becomes inactive, a successor row is created with `rotated_from_id` pointing at the predecessor, and response `200` is `{ "key": "<one-time-secret>", "item": { ... } }`.
- Delete returns `{ "deleted": true }`.

##### Get Costing Settings
```
GET /api/settings/costing
```
Response `200`:
```json
{
  "report_currency_code": "USD",
  "report_currency_symbol": "$",
  "timezone_preference": "Europe/Helsinki",
  "endpoint_fx_mappings": [
    {
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "fx_rate": "1.0"
    }
  ]
}
```

`/api/settings/auth*` routes are global management endpoints. `/api/settings/costing`, `/api/settings/timezone`, and `/api/settings/audit` are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

##### Update Costing Settings
```
PUT /api/settings/costing
```
Request:
```json
{
  "report_currency_code": "EUR",
  "report_currency_symbol": "€",
  "timezone_preference": "Europe/Helsinki",
  "endpoint_fx_mappings": [
    {
      "model_id": "gpt-4o",
      "endpoint_id": 1,
      "fx_rate": "0.92"
    }
  ]
}
```
Response `200`: Updated settings object.

##### Get Timezone Preference
```
GET /api/settings/timezone
```
Response `200`:
```json
{
  "profile_id": 1,
  "timezone_preference": "Europe/Helsinki"
}
```

##### Update Timezone Preference
```
PUT /api/settings/timezone
```
Request:
```json
{
  "timezone_preference": "America/New_York"
}
```
Response `200`: Updated timezone object.

There is no standalone `/api/settings/monitoring` route, `/api/monitoring/*` family, or Terminal Target probe route in the current live API contract. Current operator-facing observability and routing-health surfaces are provided through `/api/stats/*`, `/api/audit/*`, and `/api/loadbalance/*`.

---

#### 1.9 User-Agent Client Rules (System Global + User Profile-Scoped)

##### List User-Agent Client Rules
```
GET /api/config/user-agent-client-rules
```
Query parameters:
- `include_disabled` (boolean, default `true`): Whether to include disabled rules in the list.

Response `200`:
```json
[
  {
    "id": 1,
    "name": "Codex",
    "pattern": "codex",
    "enabled": true,
    "is_system": true,
    "profile_id": null,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

The list returns system rules plus any user rules in the effective profile.

##### Get User-Agent Client Rule
```
GET /api/config/user-agent-client-rules/{rule_id}
```
Response `200`: Single rule object.

##### Create User-Agent Client Rule
```
POST /api/config/user-agent-client-rules
```
Request:
```json
{
  "name": "My SDK",
  "pattern": "my-sdk",
  "enabled": true
}
```
Response `201`: Created rule object. `pattern` must be a valid regular expression.

##### Update User-Agent Client Rule
```
PATCH /api/config/user-agent-client-rules/{rule_id}
```
Request (all fields optional):
```json
{
  "enabled": false
}
```
Response `200`: Updated rule object.
Note: For system rules (`is_system: true`), only `enabled` is mutable. Attempting to change `name` or `pattern` returns `400`.

##### Delete User-Agent Client Rule
```
DELETE /api/config/user-agent-client-rules/{rule_id}
```
Response `200`: `{ "deleted": true }`.
Note: Delete is only available for effective-profile user rules. Attempting to delete a system rule through this route returns `404` because system rows are not in the profile-owned delete scope.

---

#### 1.10 Header Blocklist Rules (System Global + User Profile-Scoped)

##### List Header Blocklist Rules
```
GET /api/config/header-blocklist-rules
```
Query parameters:
- `include_disabled` (boolean, default `true`): Whether to include disabled rules in the list.

Response `200`:
```json
[
  {
    "id": 1,
    "name": "Cloudflare Ray",
    "match_type": "exact",
    "pattern": "cf-ray",
    "enabled": true,
    "is_system": true,
    "profile_id": null,
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z"
  }
]
```

##### Get Header Blocklist Rule
```
GET /api/config/header-blocklist-rules/{id}
```
Response `200`: Single rule object.

##### Create Header Blocklist Rule
```
POST /api/config/header-blocklist-rules
```
Request:
```json
{
  "name": "My Custom Header",
  "match_type": "prefix",
  "pattern": "x-custom-",
  "enabled": true
}
```
Response `201`: Created rule object. Returns `409` if a user rule with the same `match_type` and `pattern` already exists in the effective profile. Prefix patterns must end with `-`.

##### Update Header Blocklist Rule
```
PATCH /api/config/header-blocklist-rules/{id}
```
Request (all fields optional):
```json
{
  "name": "Updated Name",
  "enabled": false
}
```
Response `200`: Updated rule object.
Note: For system rules (`is_system: true`), only the `enabled` field can be modified. Attempting to change other fields returns `400`.

##### Delete Header Blocklist Rule
```
DELETE /api/config/header-blocklist-rules/{id}
```
Response `200`: `{ "deleted": true }`.
Note: Delete is only available for effective-profile user rules. Attempting to delete a system rule through this route returns `404` because system rows are not in the profile-owned delete scope.

---

#### 1.11 Removed Management Surface

The former CLIProxyAPI management control plane and runtime context-overflow promotion surfaces are not part of the current API.

---

### 2. Runtime Proxy API

Prism's runtime proxy is an explicit allowlist, not a full vendor API clone. It supports only the operations listed in this section through frozen Default profile id `1`. Other vendor routes, including stored-object, retrieve, delete, cancel, embedding, file, batch, and admin APIs, are outside Prism's runtime contract unless they appear in this allowlist.

Runtime proxy routes ignore management `X-Profile-Id` overrides and always resolve against frozen Default profile id `1`. Profile-scoped management reads and writes are pinned to Default profile id `1`; they do not switch proxy traffic.

After global CORS handling, the runtime branch applies HTTP proxy admission, then runtime proxy-key authentication, and only then the exact operation registry. HTTP admission and auth can therefore reject a `/v1` or `/v1beta` request before registry resolution. Registry rejections themselves do not read the request body or invoke planning, terminal-target admission, provider transport, telemetry, audit, feedback, or runtime side effects.

When operator auth is enabled, runtime proxy routes require a valid active, unexpired proxy API key. Prism checks `Authorization: Bearer <key>` first, then `X-API-Key`, then `X-Goog-Api-Key`. Missing keys return `401` with `Proxy API key required`; invalid, inactive, expired, or unknown keys return `401` with `Invalid proxy API key`. When auth is disabled, supported runtime routes continue without proxy-key authentication.

#### 2.1 Supported Runtime Operations

| Operation | Canonical operation name | Supported request |
|---|---|---|
| OpenAI models list | `openai.models` | `GET /v1/models` |
| OpenAI chat completions | `openai.chat_completions` | `POST /v1/chat/completions` |
| OpenAI Responses | `openai.responses` | `POST /v1/responses` |
| OpenAI Responses input tokens | `openai.responses.input_tokens` | `POST /v1/responses/input_tokens` |
| OpenAI Responses compact | `openai.responses.compact` | `POST /v1/responses/compact` |
| Anthropic Messages | `anthropic.messages` | `POST /v1/messages` |
| Anthropic token count | `anthropic.count_tokens` | `POST /v1/messages/count_tokens` |
| Gemini generate content | `gemini.generate_content` | `POST /v1beta/models/{model}:generateContent` |
| Gemini stream generate content | `gemini.stream_generate_content` | `POST /v1beta/models/{model}:streamGenerateContent` |
| Gemini token count | `gemini.count_tokens` | `POST /v1beta/models/{model}:countTokens` |

Each allowlisted row maps to one canonical operation name. Provider-forwarded runtime operations persist that name as `operation_name` in runtime telemetry. Operation names are part of the runtime contract, not aliases for broader vendor route groups. The Gemini `{model}` path binding is one non-empty path segment and cannot contain `/` or `:`. Nested Gemini model paths are not part of this runtime contract.

#### 2.2 Unsupported Routes and Methods

Unsupported runtime routes return a Prism JSON `404` response before Prism reads the request body, resolves a model, claims local Terminal Target admission state, contacts a provider, submits runtime side effects, or writes runtime persistence rows. The current error detail is `Runtime operation not found`.

Wrong methods on supported runtime paths return a Prism JSON `405` response before the same downstream seams run. The response includes the supported method in `Allow`, and the current error detail is `Method not allowed for runtime operation`.

Supported runtime operation request bodies are capped at `20 MiB`. Oversized requests return JSON `413` with `error: "request_body_too_large"` and `limit_bytes` before runtime planning or provider transport.

When any planned Terminal Target candidate has custom request parameters configured, the ingress body must be a valid JSON object (otherwise `400` with `Request body must be a JSON object when custom request parameters are configured`), the per-attempt merged body is re-validated against the same `20 MiB` limit (`413 request_body_too_large` before transport), and Gemini path-bound operations reject non-identity `Content-Encoding` with `415` (`Content-Encoding is not supported when custom request parameters are configured`) before buffering. These failures never reach admission, Ban Policy attempt counting, provider transport, or audit body capture.

#### 2.2A Routing Failures

Runtime planning evaluates the Model Target stage first and enters the direct Terminal Target stage only when the model stage has no eligible candidate. Each entered stage applies its own Ban Policy strategy to its ordered rows before provider transport. If no eligible Terminal Target is available inside the current retry window, Prism returns a routing-availability error before opening an upstream request. If all otherwise eligible attempts are blocked by admission counters, Prism returns `503` with `error: "admission_exhausted"` plus route-reason metadata before upstream transport. An OpenAI text request whose requested model does not accept the ingress operation rejects immediately with `400 openai_operation_not_supported`. Terminal Target connections that do not natively support the operation are skipped so later native attempts remain eligible; when no capability-compatible Terminal leaf exists anywhere in the authored graph, Prism returns `503 openai_no_compatible_terminal_target`; when compatible leaves exist but none is statically eligible (disabled row, inactive connection, strategy truncation), Prism returns `503 openai_no_eligible_terminal_target`. A statically eligible compatible route that is dynamically unavailable (Ban/retry/admission) keeps the ordinary `503` no-eligible-target response family. All three planning codes are emitted before provider transport, provider attempt, reservation, loadbalance event, usage/billing and audit body capture, and never leak Access Target, Connection or Endpoint IDs.

Request-log detail keeps flat final-target attribution fields such as `resolved_target_model_id`, `terminal_target_id`, `selected_terminal_target_id`, `endpoint_id`, and `operation_translation_mode`. Deleted model-owned routing metadata is not exposed on public detail responses.

#### 2.2B OpenAI native operation coverage

OpenAI text routing is native-only. The requested model's `openai_accepted_format` and each Terminal Target connection's `openai_text_capability` use `responses_only`, `chat_completions_only`, or `dual_native`; a candidate is native-compatible when the two operation sets intersect. A requested-model format with no support for the ingress operation returns `400 openai_operation_not_supported` before target resolution. A candidate with no intersection is skipped so later native attempts remain eligible; the static analyzer classifies the final rejection as `503 openai_no_compatible_terminal_target` when no capability-compatible Terminal leaf exists, or `503 openai_no_eligible_terminal_target` when compatible leaves exist but none is statically eligible. Responses adjunct operations, `openai.responses.input_tokens` and `openai.responses.compact`, are part of the Responses operation set.

Prism does not convert requests, non-stream responses, or streams between Chat Completions and Responses. Native attempts use the ingress operation's upstream path and preserve `operation_translation_mode = "none"`. The `operation_translation_mode` columns and request-log fields remain readable for historical rows that recorded the retired translation values.

##### 2.2B.1 Application capability matrix

The following application-spec example assumes these OpenAI text capabilities and matching access-target order:
- `gpt-5.5`: `dual_native`
- `gpt-5.4`: `dual_native`
- `deepseek-v4-flash`: `chat_completions_only`

Native request behavior is determined by operation-set intersection; `FULL` means all accepted operations are covered, `PARTIAL` means only some are covered, and `NONE` means no accepted operation is covered:

| Model accepted format | Target capability | Coverage | Native operations |
|---|---|---|---|
| `responses_only` | `responses_only` | `FULL` | Responses and adjunct operations |
| `responses_only` | `chat_completions_only` | `NONE` | No native operation |
| `responses_only` | `dual_native` | `FULL` | Responses and adjunct operations |
| `chat_completions_only` | `responses_only` | `NONE` | No native operation |
| `chat_completions_only` | `chat_completions_only` | `FULL` | Chat Completions |
| `chat_completions_only` | `dual_native` | `FULL` | Chat Completions |
| `dual_native` | `responses_only` | `PARTIAL` | Responses and adjunct operations |
| `dual_native` | `chat_completions_only` | `PARTIAL` | Chat Completions |
| `dual_native` | `dual_native` | `FULL` | Chat Completions, Responses and adjunct operations |

Valid Partial/None configurations are accepted by management writes and return non-persisted `configuration_warnings`; the first-party UI prevents selecting a `NONE` target and warns on `PARTIAL`. Unknown values, nullability violations and malformed payloads remain hard `4xx` errors. Prism never translates Chat Completions and Responses requests, response bodies, or streams; native attempts preserve `operation_translation_mode = "none"`.

#### 2.2C Retired Exact OpenAI Facade Routing

Exact OpenAI facade routing and its model fields are retired. Runtime planning uses the requested model's ordinary access-target graph, native Terminal Target capability checks, and the attached Ban Policy strategy. Prism no longer performs context-window preflight filtering or returns context-window-exceeded planning errors.

#### 2.2D Retired Overflow Replay

Model-scoped overflow replay and its authoring fields are retired. Runtime planning now uses the ordinary operation registry, access-target graph, native operation-set coverage, and the attached Ban Policy strategy. Public request-log and usage surfaces keep flat requested model, final target, Terminal Target, endpoint, and operation fields without nested retired routing metadata.

#### 2.3 OpenAI Operations

##### Models
```
GET /v1/models
```
Response without a `client_version` query parameter: local OpenAI-shaped `{"object":"list","data":[...]}` list of enabled `api_family="openai"` models for frozen Default profile id `1`.

Response when the `client_version` query parameter is present, including an empty value: local Codex client catalog `{"models":[...]}`. Prism seeds the metadata store from the embedded Codex catalog, refreshes it asynchronously from the OpenAI Codex repository at startup and every 24 hours, and keeps the last good catalog on fetch or validation failure. Exact model slugs found in the current catalog retain its metadata. Unknown OpenAI model ids inherit the `gpt-5.5` template as a usable fallback while overriding the slug, display name, description, and deterministic trailing priority. Models configured as `chat_completions_only` are absent because Codex uses the Responses wire. This response includes a weak content-derived `ETag`; an exact matching `If-None-Match` returns `304 Not Modified` with an empty body.

Prism does not contact configured model providers for either response shape. The plain list contains every enabled OpenAI model; the Codex branch additionally removes `chat_completions_only` models. Catalog refresh runs only in the background and never blocks this handler.

##### Chat Completions
```
POST /v1/chat/completions
```
Request (standard OpenAI format):
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "stream": false
}
```
Response: Native attempts are proxied from an upstream Chat Completions-capable OpenAI endpoint. Canonical operation name: `openai.chat_completions`.

##### Responses
```
POST /v1/responses
```
Request (OpenAI Responses generation format):
```json
{
  "model": "gpt-4o",
  "input": "Hello!",
  "stream": false
}
```
Response: Native attempts are proxied from an upstream Responses-capable OpenAI endpoint. Canonical operation name: `openai.responses`.

##### Responses Input Tokens
```
POST /v1/responses/input_tokens
```
Request uses the OpenAI Responses input-token counting format, including body-bound `model` and `input`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses.input_tokens`.

##### Responses Compact
```
POST /v1/responses/compact
```
Request uses the OpenAI Responses compaction format, including body-bound `model` and `input`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses.compact`.

#### 2.4 Anthropic Operations

##### Messages
```
POST /v1/messages
```
Request (standard Anthropic format):
```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}
```
Response: Proxied directly from the upstream Anthropic-compatible endpoint. Canonical operation name: `anthropic.messages`.

##### Token Count
```
POST /v1/messages/count_tokens
```
Request uses the upstream Anthropic-compatible token-count body, including body-bound `model`.
Response: Proxied directly from the upstream Anthropic-compatible endpoint. Canonical operation name: `anthropic.count_tokens`.

#### 2.5 Gemini Operations

##### Generate Content
```
POST /v1beta/models/{model}:generateContent
```
Request (standard Gemini native format):
```json
{
  "contents": [
    {
      "role": "user",
      "parts": [{"text": "Hello!"}]
    }
  ]
}
```
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.generate_content`.

##### Stream Generate Content
```
POST /v1beta/models/{model}:streamGenerateContent
```
Request uses the upstream Gemini native generate-content body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.stream_generate_content`.
The `{model}` binding must be one non-empty path segment and cannot contain `/` or `:`.

##### Count Tokens
```
POST /v1beta/models/{model}:countTokens
```
Request uses the upstream Gemini native token-count body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.count_tokens`.
For all Gemini runtime paths in this section, the `{model}` binding must be one non-empty path segment and cannot contain `/` or `:`.

#### 2.6 Streaming

Streaming stays operation-native: `openai.chat_completions`, `openai.responses`, and `anthropic.messages` use their upstream-compatible request body flags, while `gemini.stream_generate_content` uses `POST /v1beta/models/{model}:streamGenerateContent`. Streaming responses are proxied from a natively compatible upstream.
For Gemini, the `gemini.stream_generate_content` path is authoritative: `POST /v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`. `gemini.generate_content` remains the non-stream generate-content operation.

#### 2.7 Token Usage Extraction

The gateway extracts token usage from upstream responses and logs canonical disjoint token components to `request_logs`. Extraction is selected by the resolved canonical operation name and its hook collection. `input_tokens` is base input only, `output_tokens` is base output only, cache-read input, cache-creation input, and reasoning output are separate fields, and `total_tokens` uses the provider total when one is supplied.

**Non-streaming responses:**
| Canonical operation name | Response format | Extraction path |
|---|---|---|
| `openai.chat_completions` | `{"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `openai.responses`, `openai.responses.compact` | `{"usage": {"input_tokens": N, "output_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `openai.responses.input_tokens` | `{"input_tokens": N, "total_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |
| `anthropic.messages` | `{"usage": {"input_tokens": N, "cache_read_input_tokens": N, "cache_creation_input_tokens": N, "output_tokens": N}}` | Base input, cache-read input, cache-creation input, and base output stay separate; total is derived when upstream omits it |
| `anthropic.count_tokens` | `{"input_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |
| `gemini.generate_content`, `gemini.stream_generate_content` when handled as non-stream JSON | `{"usageMetadata": {"promptTokenCount": N, "cachedContentTokenCount": N, "candidatesTokenCount": N, "thoughtsTokenCount": N, "totalTokenCount": N}}` | Base input subtracts cache-read input; base output subtracts reasoning output; provider `totalTokenCount` stays authoritative |
| `gemini.count_tokens` | `{"totalTokens": N}` or `{"total_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null |

**Streaming responses:**
The gateway accumulates SSE chunks during streaming and extracts usage from operation-specific terminal events:
| Canonical operation name | Usage events | Extraction |
|---|---|---|
| `openai.chat_completions` | Final usage chunk before `[DONE]` | Same canonical disjoint fields as non-streaming usage |
| `openai.responses` | `response.completed` event with a `usage` object when provided by upstream | Same canonical disjoint fields as non-streaming usage |
| `anthropic.messages` | `message_start` usage plus cumulative `message_delta.usage.output_tokens` | Base input, cache-read input, cache-creation input, and final base output stay separate |
| `gemini.stream_generate_content` | Stream terminal or final chunk carrying `usageMetadata` | Same canonical disjoint fields as Gemini non-stream `usageMetadata` |

If token data cannot be extracted from the provider response, runtime usage token fields are logged as `null`. Completed streams that lack required usage keep `MISSING_TOKEN_USAGE`; interrupted or no-terminal streams with missing required tokens use `STREAM_USAGE_UNAVAILABLE` when their classified stream outcome made terminal usage unavailable. Aggregate `cached_tokens` is derived-only from cache-read plus cache-creation input tokens and is not a persisted runtime component.

---

### 3. Health Check

```
GET /health
```
Response `200` returns liveness, readiness, and startup state together with the current backend release version:
```json
{
  "status": "ok",
  "version": "<current release version>",
  "liveness": "ok",
  "readiness": "ready",
  "startup": "complete"
}
```

The health contract is not version only. It is the operator-facing target for backend readiness, startup state, and live in-app health views.

---

### 4. Statistics API

Stats APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

#### 4.0 Dashboard Stats
```
GET /api/stats/dashboard
```
This is the canonical overview dashboard read path. It returns one backend-computed, stats-only aggregate snapshot for the effective profile, including overview metrics, API-family rows, top-spending models, and a `routing_health_map` response field. It does not include recent request rows, request-log IDs, or request-log cursor data. Recent activity is served by `GET /api/stats/dashboard/recent-activity`. The current production dashboard does not render `routing_health_map`.

Query parameters: none. Legacy `window` query values are ignored. The endpoint always returns the canonical aggregate snapshot and does not expose the old top-level `window`, `covers`, `freshness`, or `metrics` shape. Snapshot freshness is ordered by lexicographic `snapshot_revision`; `source_watermark` is diagnostic only.

Response `200`:
```json
{
  "generated_at": "2026-04-19T12:00:00Z",
  "snapshot_revision": "01JZ8Y3K2N4P6R8T0V1W2X3Y4Z",
  "source_watermark": {
    "latest_usage_event_created_at": "2026-04-19T11:59:58Z",
    "latest_usage_event_id": 345
  },
  "coverage_24h": {
    "from": "2026-04-18T12:00:00Z",
    "to": "2026-04-19T12:00:00Z"
  },
  "coverage_30d": {
    "from": "2026-03-20T12:00:00Z",
    "to": "2026-04-19T12:00:00Z"
  },
  "health": {
    "lag_seconds": 0,
    "stale": false,
    "stale_after_seconds": 120
  },
  "metric_snapshot": {
    "active_models": 12,
    "average_rpm": 0.7,
    "average_rpm_request_total": 42,
    "avg_latency": 523,
    "error_rate": 2.38,
    "p95_latency": 900,
    "priced_request_count": 40,
    "stream_share": 18.5,
    "success_rate": 97.62,
    "total_cost": 1250000,
    "total_models": 14,
    "total_requests": 42,
    "unpriced_request_count": 2
  },
  "api_family_rows": [
    { "key": "openai", "total_requests": 42, "success_rate": 97.62 }
  ],
  "top_spending_models": [],
  "routing_health_map": {
    "nodes": [],
    "links": [],
    "endpointCount": 0,
    "modelCount": 0,
    "activeConnectionTotal": 0,
    "activeTerminalTargetTotal": 0,
    "trafficRequestTotal24h": 0
  }
}
```

`routing_health_map` is assembled by the backend from Default-profile model, access-target, endpoint, connection, and final-attributed usage-event data.

#### 4.0A Dashboard Recent Activity
```
GET /api/stats/dashboard/recent-activity?limit=N
```
This endpoint is the separate request-history-backed activity feed for dashboard bootstrap and repair. It is not embedded in the dashboard snapshot. The default limit is `12`; the maximum limit is `50`. Rows are ordered by `(created_at DESC, request_log_id DESC)`.

Response `200`:
```json
{
  "generated_at": "2026-04-19T12:00:00Z",
  "activity_watermark": {
    "latest_request_log_created_at": "2026-04-19T11:59:59Z",
    "latest_request_log_id": 101
  },
  "items": [
    {
      "request_log_id": 101,
      "created_at": "2026-04-19T11:59:59Z",
      "model_id": "gpt-4o",
      "model_label": "GPT-4o",
      "resolved_target_model_id": "gpt-4o-mini",
      "resolved_target_model_label": "GPT-4o mini",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "status_code": 200,
      "response_time_ms": 523,
      "ttft_ms": 120,
      "completion_duration_ms": 403,
      "is_stream": true,
      "stream_outcome": "completed",
      "total_tokens": 1234,
      "total_cost_user_currency_micros": 1250000,
      "priced_flag": true,
      "unpriced_reason": null,
      "report_currency_symbol": "$"
    }
  ]
}
```

Recent activity links into request-log investigation by `request_log_id`. It does not define snapshot freshness, and activity publication does not force a dashboard snapshot rebuild.

#### 4.1 Usage Snapshot
```
GET /api/stats/usage-snapshot
```
This is the REST analytics snapshot contract for API callers, debugging, and the `/observe?tab=analytics` UI polling path.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Snapshot range preset. Supported values: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |

The snapshot is backed by `backend/internal/httpapi/management/stats/service.go` together with the aggregation types and query helpers in `backend/internal/domain/stats/snapshot.go` and `backend/internal/domain/stats/types.go`.

The snapshot is aggregated from persisted usage-event rows. Endpoint aggregates read the stored `usage_request_events.endpoint_label_snapshot` value and expose it as public `endpoint_label`, so historical labels survive later endpoint renames or deletion. `/api/stats/dashboard` is the canonical overview aggregate and includes a backend-computed `routing_health_map` response field; the current dashboard leaves that field unrendered. Exact request investigation remains on `/observe/requests`, while dashboard and other pages continue to use the shared stats routes below.

Response `200` includes `latency_trends` alongside `request_trends`, `token_usage_trends`, `token_type_breakdown`, and `cost_overview`. `latency_trends.hourly[]` and `latency_trends.daily[]` use the same series key/label shape as request trends; each point exposes `bucket_start`, `p50_ms`, and `p95_ms`. Empty latency buckets keep the bucket and return `null` percentile values.

`GET /api/stats/requests/operations` is not part of the current management API.

#### 4.1A Endpoint Model Statistics
```
GET /api/stats/endpoints/{endpoint_id}/models
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Time preset: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |
| `from_time` | datetime | — | Optional explicit start time |
| `to_time` | datetime | — | Optional explicit end time |

Response `200`: Array of per-model endpoint statistics. Each item includes `model_id`, `model_label`, request counts, success rates, TTFT percentiles, token totals, total cost, and average output rate for the selected endpoint scope.

#### 4.2 List Request Logs
```
GET /api/stats/requests
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `ingress_request_id` | string | — | Exact incoming-request grouping ID shared by per-attempt rows |
| `model_id` | string | — | Filter by model ID |
| `status_family` | string | — | Filter by status family (`2xx`, `4xx`, or `5xx`) |
| `status_code` | integer | — | Exact response status-code filter |
| `error_text` | string | — | Case-insensitive substring match against `error_detail` |
| `priced` | boolean | — | Filter by retained request-log `priced_flag` (`true` or `false`) |
| `unpriced_reason` | string | — | Exact unpriced reason filter: `PRICING_DISABLED`, `MISSING_TOKEN_USAGE`, `STREAM_USAGE_UNAVAILABLE`, or `MISSING_PRICE_DATA` |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `client_rule_id` | integer | none | Filter by caller client, matched against `caller_user_agent` only through enabled User-Agent Client Rules |
| `resolved_target_model_id` | string | none | Filter by final target model selected for the attempt |
| `limit` | integer | 50 | Result limit; must be positive |
| `offset` | integer | 0 | Pagination offset |

Frontend request-log route contract:
- `/observe/requests` defaults to the last 24 hours by deriving `from_time` from `time_range=24h`; generated URLs omit `time_range=24h` because it is the page default.
- The page's `status=success` URL alias maps to backend `status_family=2xx`; direct `status_family=2xx`, `4xx`, and `5xx` are also supported backend filters.
- The page CSV export is client-only and exports only the rows already loaded on the current table page. There is no full-range server-side CSV export endpoint.

Response `200`:
```json
{
  "filter_options": {
    "endpoints": [
      {
        "endpoint_id": 12,
        "endpoint_label": "Primary OpenAI"
      }
    ],
    "models": [
      {
        "model_id": "gpt-4o",
        "model_label": "GPT-4o"
      }
    ],
    "clients": [
      {
        "client_rule_id": 7,
        "client_label": "Codex"
      }
    ],
    "resolved_target_models": [
      {
        "resolved_target_model_id": "gpt-4o",
        "model_label": "GPT-4o"
      }
    ]
  },
  "items": [
    {
      "id": 1,
      "model_id": "gpt-4o",
      "model_label": "GPT-4o",
      "resolved_target_model_id": "gpt-4o",
      "resolved_target_model_label": "GPT-4o",
      "api_family": "openai",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "connection_id": 1,
      "terminal_target_id": 1,
      "status_code": 200,
      "response_time_ms": 1234,
      "ttft_ms": 320,
      "completion_duration_ms": 914,
      "is_stream": false,
      "stream_outcome": "not_streaming",
      "stream_error_kind": null,
      "output_tokens": 42,
      "total_tokens": 57,
      "total_cost_user_currency_micros": 1250,
      "priced_flag": true,
      "unpriced_reason": null,
      "reasoning_effort": "low",
      "report_currency_symbol": "$",
      "caller_client_display": "Codex",
      "upstream_client_display": "OpenAI SDK",
      "user_agent_overridden": false,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

The list route is the slim browse contract used by `/observe/requests` and other row-summary consumers. It keeps one row per upstream attempt, returns `filter_options.endpoints` for the endpoint dropdown, `filter_options.models` for the requested-model dropdown, `filter_options.clients` for caller client filtering, and `filter_options.resolved_target_models` for final-target filtering. It includes requested-model labels, final-target labels, `stream_outcome`, and `stream_error_kind` for display. The current request-log page uses page sizes `100`, `300`, and `500`, with `100` as the frontend default. This retained-history route is the operator drill-in surface for investigation, not a dashboard aggregate or metrics endpoint.

`filter_options` always includes `endpoints`, `models`, `clients`, and `resolved_target_models`. `filter_options.models` is request-log scoped and contains `{ model_id, model_label }` entries. `filter_options.clients` contains `{ client_rule_id, client_label }` entries built from enabled User-Agent Client Rules. `client_rule_id` filtering is caller-only and matches `caller_user_agent`; it never matches `upstream_user_agent`. `filter_options.resolved_target_models` contains `{ resolved_target_model_id, model_label }` entries for final-target filtering. Empty option sets are returned as empty arrays instead of omitted fields. `ingress_request_id` groups multiple attempt rows that belong to one incoming runtime request. `model_id` stays the requested model and `resolved_target_model_id` captures the final target model for that attempt, while request-log row and detail payloads use `resolved_target_model_label` for the matching display label.

Exact single-request investigation now lives on `GET /api/stats/requests/{request_id}` instead of the paginated list-query surface.

#### 4.3 Get Request Log Detail
```
GET /api/stats/requests/{request_id}
```

Response `200`:
```json
{
  "summary": {
    "id": 1,
    "created_at": "2025-01-15T10:30:00Z",
    "model_id": "gpt-4o",
    "model_label": "GPT-4o",
    "resolved_target_model_id": "gpt-4o",
    "resolved_target_model_label": "GPT-4o",
    "api_family": "openai",
    "status_code": 200,
    "response_time_ms": 1234,
    "ttft_ms": 320,
    "completion_duration_ms": 914,
    "is_stream": false,
    "stream_outcome": "not_streaming",
    "stream_error_kind": null,
    "stream_error_detail": null
  },
  "request": {
    "operation_name": "openai.chat_completions",
    "upstream_operation_name": "openai.chat_completions",
    "operation_translation_mode": "none",
    "request_path": "/v1/chat/completions",
    "upstream_request_path": "/v1/chat/completions",
    "ingress_request_id": "ingress_req_42",
    "attempt_number": 2,
    "provider_correlation_id": "req_upstream_abc123",
    "proxy_api_key_id": null,
    "proxy_api_key_name_snapshot": null,
    "caller_user_agent": "codex/1.0",
    "upstream_user_agent": "OpenAI/Python 1.0",
    "caller_client_display": "Codex",
    "upstream_client_display": "OpenAI SDK",
    "user_agent_overridden": false,
    "error_detail": null,
    "request_generation_params": {"temperature": 0.7},
    "request_generation_params_status": "captured"
  },
  "routing": {
    "profile_id": 1,
    "endpoint_label": "Primary OpenAI",
    "endpoint_id": 12,
    "terminal_target_id": 1,
    "selected_terminal_target_id": 1,
    "endpoint_base_url": "https://api.openai.com",
    "endpoint_description": "Primary OpenAI",
    "audit_enabled_at_request": false,
    "audit_capture_bodies_at_request": false
  },
  "usage": {
    "input_tokens": 15,
    "output_tokens": 42,
    "total_tokens": 57,
    "success_flag": true,
    "billable_flag": true,
    "priced_flag": true,
    "unpriced_reason": null,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0,
    "reasoning_tokens": 0
  },
  "costing": {
    "input_cost_micros": 500,
    "output_cost_micros": 750,
    "cache_read_input_cost_micros": 0,
    "cache_creation_input_cost_micros": 0,
    "reasoning_cost_micros": 0,
    "total_cost_original_micros": 1250,
    "total_cost_user_currency_micros": 1250,
    "currency_code_original": "USD",
    "report_currency_code": "USD",
    "report_currency_symbol": "$",
    "fx_rate_used": "1",
    "fx_rate_source": "DEFAULT_1_TO_1"
  },
  "pricing": {
    "pricing_snapshot_unit": "PER_1M",
    "pricing_snapshot_input": "33.333333",
    "pricing_snapshot_output": "17.857143",
    "pricing_snapshot_cache_read_input": "0",
    "pricing_snapshot_cache_creation_input": "0",
    "pricing_snapshot_reasoning": "0",
    "pricing_config_version_used": 1
  }
}
```

Request-log detail uses the same canonical disjoint token components as runtime persistence: base input, base output, cache-read input, cache-creation input, reasoning output, and provider or derived total. Pricing snapshots store the five concrete pricing strings used for the attempt. Explicit `"0"` prices are configured free pricing and produce zero component cost without marking the row unpriced. Public request-log detail routing exposes `terminal_target_id` and `selected_terminal_target_id`; it does not expose `routing.connection_id` on the detail surface.

Request-log detail keeps ingress and upstream attribution separate. `request.operation_name` and `request.request_path` are ingress-led. `request.upstream_operation_name`, `request.operation_translation_mode`, and `request.upstream_request_path` describe the provider-facing operation selected for the attempt. Current OpenAI attempts use `operation_translation_mode = "none"` and keep ingress and upstream operation shapes equal. Historical rows retain and expose any translation mode recorded before sibling conversion was removed.

Ordinary target-graph attempts preserve the top-level requested and resolved model fields without rewriting client-visible response-body identity. Request-log detail exposes resolved model and selected Terminal Target attribution.

Response `404`: returned when the request ID is missing or out of scope for the effective profile.

Stream telemetry values are stable strings. `stream_outcome` is one of `not_streaming`, `completed`, `provider_incomplete`, `client_disconnected`, `upstream_read_error`, `upstream_ended_without_terminal`, or `unknown`. `stream_error_kind` is nullable and, when present, is one of `client_write_failed`, `request_context_canceled`, `upstream_read_failed`, or `missing_terminal_event`. `stream_error_detail` appears only on exact request-log detail responses; it is sanitized diagnostic text, not provider content, headers, or secrets.

The request-log sheet consumes this grouped detail contract as overview-only data. Linked audit payload resolution is isolated to the dedicated `/observe/requests/{request_id}/audit` page, which uses `request_log_id` plus a UTC window derived from `summary.created_at`. The derived frontend window is `created_at` minus 12 hours through `created_at` plus 12 hours, serialized explicitly as canonical audit `from` and `to` query parameters.

The request-history, spending, throughput, usage-snapshot, model-metrics, connection-success-rate, and dashboard aggregate APIs in this section remain product-facing PostgreSQL-backed surfaces. Prism no longer starts metrics or tracing exporters and does not surface a backend-local `/metrics` compatibility route.

#### 4.4 Get Aggregated Statistics
```
GET /api/stats/summary
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |
| `group_by` | string | — | Group results by: `model`, `api_family`, `endpoint` |
| `model_id` | string | — | Filter by model ID |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |

Response `200`:
```json
{
  "total_requests": 1500,
  "success_count": 1450,
  "error_count": 50,
  "success_rate": 96.67,
  "avg_response_time_ms": 850,
  "p95_response_time_ms": 2100,
  "total_input_tokens": 50000,
  "total_output_tokens": 120000,
  "total_tokens": 170000,
  "groups": [
    {
      "key": "gpt-4o",
      "total_requests": 800,
      "success_count": 790,
      "error_count": 10,
      "avg_response_time_ms": 750,
      "total_tokens": 90000
    }
  ]
}
```

#### 4.5 Model Metrics (Batch)
```
POST /api/stats/models/metrics
```
Request:
```json
{
  "model_ids": ["gpt-4o", "claude-3-5-sonnet"],
  "summary_window_hours": 24,
  "spending_preset": "last_30_days"
}
```
Response `200`: `items[]`, where each item contains `model_id`, `success_rate`, `request_count_24h`, `p95_latency_ms`, and `spend_30d_micros`.

#### 4.6 Get Connection Success Rates
```
GET /api/stats/connection-success-rates
```
Returns success rate data for all connections, computed from `request_logs`.

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |

Response `200`:
```json
[
  {
    "connection_id": 1,
    "total_requests": 150,
    "success_count": 148,
    "error_count": 2,
    "success_rate": 98.67
  },
  {
    "connection_id": 2,
    "total_requests": 0,
    "success_count": 0,
    "error_count": 0,
    "success_rate": null
  }
]
```

Fields:
- `connection_id` (int): The connection ID
- `total_requests` (int): Total requests routed through this connection
- `success_count` (int): Requests with 2xx status codes
- `error_count` (int): Requests with non-2xx status codes
- `success_rate` (float | null): Success percentage (0-100), `null` if no requests

#### 4.7 Get Throughput Report
```
GET /api/stats/throughput
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range |
| `to_time` | datetime | now | End of time range |
| `model_id` | string | — | Filter by model ID |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |

Response `200`: Throughput summary plus time buckets (`average_rpm`, `peak_rpm`, `current_rpm`, `total_requests`, `time_window_seconds`, `buckets[]`).

#### 4.8 Global Log Retention Settings
```
GET /api/settings/log-retention
PUT /api/settings/log-retention
```

These routes are global and do not use `X-Profile-Id`. They store the instance-wide normal retention policy. Request-log, audit-log, statistics, and load-balance list/detail APIs are pinned to Default profile id `1`, but retention settings do not.

Request and response fields:
| Field | Type | Description |
|---|---|---|
| `request_logs_retention_days` | integer or null | Global request-log retention days. `null` disables the stored policy. |
| `audit_logs_retention_days` | integer or null | Global audit-log retention days. `null` disables the stored policy. |
| `statistics_retention_days` | integer or null | Global `usage_request_events` retention days. `null` disables the stored policy. |
| `loadbalance_events_retention_days` | integer or null | Global load-balance event retention days. `null` disables the stored policy. |

Every non-null retention-day value must be `>= 1`; the database enforces this lower bound for all four fields.

Response `200`:
```json
{
  "request_logs_retention_days": 30,
  "audit_logs_retention_days": 90,
  "statistics_retention_days": 90,
  "loadbalance_events_retention_days": 30
}
```

#### 4.8A Create Global Log Retention Job
```
POST /api/maintenance/log-retention/jobs
```
Headers:
| Header | Required | Description |
|---|---|---|
| `Idempotency-Key` | no | Optional stable client key for this global retention job |

Request:
```json
{
  "table": "request_logs",
  "cutoff": "2026-04-01T00:00:00Z",
  "delete_all": false,
  "reason": "operator retention cleanup"
}
```

Allowed `table` values are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`. Provide either `cutoff` or `delete_all=true`. If both are omitted, Prism computes `cutoff` from the stored global policy for that table; if no policy exists, it returns `400`.

Response `202`:
```json
{
  "job_id": "job_0123456789abcdef01234567",
  "state": "queued",
  "status_url": "/api/management/jobs/job_0123456789abcdef01234567",
  "scope": {
    "table": "request_logs",
    "cutoff": "2026-04-01T00:00:00Z",
    "delete_all": false
  }
}
```

The response sets `Location` to the same job status URL. The job type is `log_retention`, uses `profile_id = 0`, and applies across the instance.

Retention drops whole daily child partitions whose upper bound is `<= cutoff`. Only the single child partition that overlaps the cutoff receives a bounded row delete, followed by `VACUUM (ANALYZE, PROCESS_TOAST TRUE)` on that boundary child.

Audit rows keep weak request metadata. Request-log retention does not clear `request_log_id`, `request_log_created_at`, or `ingress_request_id`; audit reads return `request_log_missing=true` only when both request-log link fields are non-null and the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves.

#### 4.9 Get Spending Reports
```
GET /api/stats/spending
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | — | Time preset: `today`, `24h`, `last_7_days`, `7d`, `last_30_days`, `30d`, `custom`, `all` |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `model_id` | string | — | Filter by model ID |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `connection_id` | integer | — | Filter by connection ID |
| `group_by` | string | `none` | Group by: `none`, `day`, `week`, `month`, `api_family`, `model`, `endpoint`, `model_endpoint` |
| `limit` | integer | 50 | Result limit; must be positive |
| `offset` | integer | 0 | Pagination offset |
| `top_n` | integer | 5 | Number of top spenders to return; must be positive |

Response `200`:
```json
{
  "summary": {
    "total_cost_micros": 1250000,
    "successful_request_count": 1500,
    "priced_request_count": 1450,
    "unpriced_request_count": 50,
    "total_input_tokens": 50000,
    "total_output_tokens": 120000,
    "total_cache_read_input_tokens": 10000,
    "total_cache_creation_input_tokens": 1500,
    "total_reasoning_tokens": 2000,
    "total_tokens": 182000,
    "avg_cost_per_successful_request_micros": 833
  },
  "groups": [
    {
      "key": "gpt-4o",
      "total_cost_micros": 850000,
      "total_requests": 800,
      "priced_requests": 790,
      "unpriced_requests": 10,
      "total_tokens": 90000
    }
  ],
  "groups_total": 12,
  "top_spending_models": [
    {
      "model_id": "gpt-4o",
      "model_label": "GPT 4o",
      "total_cost_micros": 850000
    }
  ],
  "top_spending_endpoints": [
    {
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "total_cost_micros": 740000
    }
  ],
  "unpriced_breakdown": {
    "PRICING_DISABLED": 30,
    "STREAM_USAGE_UNAVAILABLE": 12,
    "MISSING_TOKEN_USAGE": 8
  },
  "report_currency_code": "USD",
  "report_currency_symbol": "$"
}
```

`top_spending_models` rows carry both the stable `model_id` and the resolved `model_label` used for operator-facing displays. `model_label` reflects the current canonical model configuration label when one exists and otherwise falls back to `model_id`.

Spending summaries aggregate canonical disjoint token components independently. `total_input_tokens` is base input only, `total_output_tokens` is base output only, `total_cache_read_input_tokens`, `total_cache_creation_input_tokens`, and `total_reasoning_tokens` are separate split totals, and `total_tokens` uses provider totals when available.

Unpriced reasons distinguish pricing configuration gaps from observed usage gaps. `MISSING_PRICE_DATA` means the pricing template or pricing snapshot is absent, unusable, or invalid, or required FX data was missing or invalid. Explicit `"0"` prices mean configured free pricing and do not trigger `MISSING_PRICE_DATA`. `MISSING_TOKEN_USAGE` means a completed stream or non-stream response lacked required upstream token usage. `STREAM_USAGE_UNAVAILABLE` means a classified stream outcome made terminal usage unavailable and required tokens were absent. Prism doesn't estimate tokens or cost for usage gaps.

---

### 5. Audit API

Audit APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

#### 5.0 API-Family Audit Settings
```
GET /api/settings/audit
PUT /api/settings/audit
```

`/api/settings/audit` is pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored. `GET` returns exactly three rows in stable order: `openai`, `anthropic`, `gemini`. Missing persisted rows default to `audit_enabled=false` and `audit_capture_bodies=false`.

Response `200`:
```json
{
  "profile_id": 1,
  "settings": [
    { "api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false },
    { "api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false },
    { "api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false }
  ]
}
```

`PUT` is a full replacement. The request must contain exactly one row for each supported family. Unknown families, duplicates, missing families, and `audit_enabled=false` with `audit_capture_bodies=true` reject before persistence.

Request:
```json
{
  "settings": [
    { "api_family": "openai", "audit_enabled": true, "audit_capture_bodies": true },
    { "api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false },
    { "api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false }
  ]
}
```

Body capture is valid only when audit is enabled for that API family. Runtime loads the selected policy by profile and model `api_family` into request planning snapshots, then persists the request-time booleans in existing `audit_enabled_at_request` and `audit_capture_bodies_at_request` fields.

#### 5.1 List Audit Logs
```
GET /api/audit/logs
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `request_log_id` | integer | none | Filter audit rows linked to one request log |
| `model_id` | string | none | Filter by model ID |
| `status_code` | integer | none | Filter by response status code |
| `endpoint_id` | integer | none | Filter by endpoint ID |
| `connection_id` | integer | none | Filter by connection ID |
| `from` | datetime | required | Inclusive start of bounded time range (RFC 3339) |
| `to` | datetime | required | Exclusive end of bounded time range (RFC 3339) |
| `limit` | integer | 50 | Positive result count; values above `200` are silently capped to `200` |
| `cursor` | string | none | Opaque keyset cursor returned as `next_cursor` |
| `sort` | string | `desc` | Only `desc` is supported |

The list API returns one row per upstream attempt. If a proxy request fails over across connections, each attempt has its own audit row. The `from` and `to` window is required and may not exceed 7 days, including when `request_log_id` is supplied. The backend has no fallback, default audit window, or legacy time-window aliases for request-log lookups. When `request_log_id` identifies an existing request whose request-time audit flag is disabled, the list returns `409` with `Audit capture unavailable for this request`. Unsupported query keys return `400` with `audit_filter_unsupported`.

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 1,
      "request_log_id": 42,
      "request_log_created_at": "2025-01-15T10:30:00Z",
      "ingress_request_id": "ingress_req_42",
      "request_log_missing": false,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "connection_id": 1,
      "endpoint_base_url": "https://api.openai.com",
      "endpoint_description": "Primary production key",
      "request_method": "POST",
      "request_url": "https://api.openai.com/v1/chat/completions",
      "request_headers": "{\"content-type\": \"application/json\", \"authorization\": \"[REDACTED]\"}",
      "request_body_preview": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello!\"}]}",
      "request_body_stored": true,
      "response_status": 200,
      "response_body_stored": true,
      "is_stream": false,
      "duration_ms": 1234,
      "audit_enabled_at_request": true,
      "audit_capture_bodies_at_request": true,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "next_cursor": "eyJ2IjoxLCJsYXN0X2NyZWF0ZWRfYXQiOiIyMDI1LTAxLTE1VDEwOjMwOjAwWiIsImxhc3RfaWQiOjF9.signature",
  "has_more": true,
  "window": {
    "from": "2025-01-15T00:00:00Z",
    "to": "2025-01-16T00:00:00Z"
  },
  "limit": 50,
  "sort": "desc"
}
```

The list API returns `request_body_preview` instead of the full body. It keeps at most the first 200 Unicode code points and does not append an ellipsis or another truncation marker. Use the detail API for full content.
If body capture was off at request time, `request_body_preview` is `null` even though the audit metadata still exists. `response_body_stored` means captured response bytes were stored, independent of `is_stream`; rows with `response_body_stored=false` have no stored response body. Audit rows preserve `request_log_id`, `request_log_created_at`, and `ingress_request_id` after request-log retention. `request_log_missing=true` means both request-log link fields are present but the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves. If either link field is null, `request_log_missing` is false.
Rows are ordered by `(created_at DESC, id DESC)`. Pagination is keyset-based: when `has_more=true`, pass the returned `next_cursor` with the same window, sort, and filters. The audit list response does not include `total` or `offset`.

#### 5.2 Get Audit Log Detail
```
GET /api/audit/logs/{id}
```
Response `200`:
```json
{
  "id": 1,
  "profile_id": 1,
  "request_log_id": 42,
  "request_log_created_at": "2025-01-15T10:30:00Z",
  "ingress_request_id": "ingress_req_42",
  "request_log_missing": false,
  "model_id": "gpt-4o",
  "endpoint_id": 12,
  "connection_id": 1,
  "endpoint_base_url": "https://api.openai.com",
  "endpoint_description": "Primary production key",
  "request_method": "POST",
  "request_url": "https://api.openai.com/v1/chat/completions",
  "request_headers": "{\"content-type\": \"application/json\", \"authorization\": \"[REDACTED]\"}",
  "request_body": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello!\"}],\"temperature\":0.7}",
  "request_body_stored": true,
  "response_status": 200,
  "response_headers": "{\"content-type\": \"application/json\", \"x-request-id\": \"req_abc123\"}",
  "response_body": "{\"id\":\"chatcmpl-abc\",\"choices\":[...],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}",
  "response_body_stored": true,
  "is_stream": false,
  "duration_ms": 1234,
  "audit_enabled_at_request": true,
  "audit_capture_bodies_at_request": true,
  "created_at": "2025-01-15T10:30:00Z"
}
```

When body capture is enabled, request bodies can be stored for every audited upstream attempt. Only the final attempt can store a non-empty `response_body`, and `response_body_stored=true` means that final response bytes were retained; `is_stream` does not prevent that storage. Captured OpenAI bodies are the native upstream payloads or SSE bytes.
If body capture is disabled for a request, both `request_body` and `response_body` are `null`. Rows with `response_body_stored=false` have no stored response body, including old rows that were written before streaming response capture was available.

Response `404`: Audit log not found.

Response `409`: `Audit capture unavailable for this request` when the requested audit detail has audit disabled at request time.

#### 5.3 Audit Log Retention

Audit log list and detail APIs remain pinned to Default profile id `1`. Normal audit-log cleanup is global and uses `POST /api/maintenance/log-retention/jobs` with `table = "audit_logs"`, or the stored `audit_logs_retention_days` value from `/api/settings/log-retention`.

The retired audit cleanup endpoints are not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

Audit rows retain weak request metadata in `request_log_id`, `request_log_created_at`, and `ingress_request_id`; retention does not clear those fields. `request_log_missing=true` requires both request-log link fields and reports that the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves, so a request detail link can be unavailable after request-log retention expires before audit-log retention.

#### 5.3A Management Job Status and Cancel
```
GET /api/management/jobs
GET /api/management/jobs/{job_id}
POST /api/management/jobs/{job_id}/cancel
```

`GET /api/management/jobs` returns recent Default-profile jobs only. Audit-delete jobs are profile-scoped. Global log-retention jobs use `profile_id = 0`; they are not included in the list response, but the `status_url` returned by `POST /api/maintenance/log-retention/jobs` can be used to read them by ID through `GET /api/management/jobs/{job_id}`:
```json
{
  "items": [
    {
      "id": "job_0123456789abcdef01234567",
      "type": "audit_delete",
      "state": "queued",
      "requested_by": "profile:1",
      "requested_at": "2026-04-19T12:00:00Z",
      "started_at": null,
      "finished_at": null,
      "scope": { "before": "2025-01-01T00:00:00Z" },
      "reason": "operator retention cleanup",
      "progress": {
        "rows_matched_estimate": 0,
        "rows_deleted": 0,
        "batches_completed": 0,
        "last_cursor": ""
      },
      "attempt_count": 0,
      "last_heartbeat_at": null,
      "cancel_requested": false,
      "error_code": null,
      "error_message": null
    }
  ],
  "has_more": false,
  "next_cursor": null
}
```

`GET /api/management/jobs/{job_id}` first resolves the Default-profile job and then falls back to a global log-retention job with the same id. `POST /api/management/jobs/{job_id}/cancel` follows the same resolution order, marks the resolved job for cancellation, and returns `202` with the job object. Unknown or out-of-scope jobs return `404`.

#### 5.4 Redaction Rules

Audit redaction is deliberately narrow. Before stored upstream **request** headers are serialized, Prism replaces the values of exactly `authorization`, `x-api-key`, and `x-goog-api-key` (case-insensitive) with `[REDACTED]`. It does not preserve an `Authorization` scheme and does not redact other request-header names by pattern.

Stored upstream response headers are serialized as captured without header redaction. Captured request and response bodies are also stored as captured and can contain user-provided secrets or PII. Body capture is request-time provenance via `audit_capture_bodies_at_request`; when disabled, both `request_body` and `response_body` are `null`.

#### 5.5 Body Size Limits

When body capture is enabled, Prism can store the captured request body for each audited upstream attempt and the captured response body for the final attempt only. Audit list previews are limited to the first 200 Unicode code points without an appended truncation marker; detail bodies are not truncated by this preview helper.

---

### 6. Loadbalance API

Loadbalance APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

#### 6.1 List Loadbalance Strategies
```
GET /api/loadbalance/strategies
```
Response `200`: Array of strategy objects in the effective profile scope.

#### 6.2 Create Loadbalance Strategy Defaults
```
POST /api/loadbalance/strategies/defaults
```
No request body.

This endpoint is pinned to Default profile id `1` and creates the canonical explicit Ban Policy defaults for that profile only: `Default single routing`, `Default fill-first routing`, and `Default round-robin routing`.

Response `200`:
```json
{
  "items": [
    {
      "id": 12,
      "profile_id": 1,
      "name": "Default fill-first routing",
      "legacy_strategy_type": "fill-first",
      "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
      "ban_mode": "off",
      "retry_base_delay_ms": 60000,
      "retry_backoff_multiplier": 2.0,
      "retry_jitter_ratio": 0.2,
      "retry_max_delay_ms": 900000,
      "cycle_retry_attempt_limit": 3,
      "ban_cumulative_retry_attempt_threshold": 0,
      "ban_duration_seconds": 0
    }
  ],
  "created_count": 1,
  "created_names": ["Default fill-first routing"],
  "existing_names": ["Default single routing"]
}
```

The response includes the full current strategy list in `items` plus creation metadata so the caller can tell which canonical rows were created versus already present.

Returns `409` when one or more canonical default names are already occupied by non-canonical strategies in Default profile id `1`. In that case, `detail` is an object with `message` and `conflicting_names`.

#### 6.3 Create Loadbalance Strategy
```
POST /api/loadbalance/strategies
```
Request:
```json
{
  "name": "round-robin-primary",
  "legacy_strategy_type": "round-robin",
  "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
  "ban_mode": "temporary",
  "retry_base_delay_ms": 45000,
  "retry_backoff_multiplier": 3.5,
  "retry_jitter_ratio": 0.2,
  "retry_max_delay_ms": 720000,
  "cycle_retry_attempt_limit": 3,
  "ban_cumulative_retry_attempt_threshold": 6,
  "ban_duration_seconds": 1800
}
```
Response `201`: Created strategy object.

Validation rules:
- `name` must be unique within the effective profile scope.
- `legacy_strategy_type` must be `single`, `fill-first`, or `round-robin`.
- `failure_status_codes` values must be unique valid HTTP status integers (`100..599`); the backend sorts them before persistence and response serialization.
- Retry-window delay, backoff, jitter, max delay, and cycle retry attempt limit must stay within backend bounds.
- `cycle_retry_attempt_limit` is optional; omitted create/update payloads default it to `3`. When provided, it must be from `1` to `50`.
- `ban_mode` is `off`, `temporary`, or `until_reset`.
- `ban_mode = "off"` requires `ban_cumulative_retry_attempt_threshold = 0` and `ban_duration_seconds = 0`.
- `ban_mode = "temporary"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds` from `1` to `86400`.
- `ban_mode = "until_reset"` requires `ban_cumulative_retry_attempt_threshold` from `1` to `500`, `ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit`, and `ban_duration_seconds = 0`.
- Runtime retry-cycle exhaustion is inclusive: `cycle_retry_attempts >= cycle_retry_attempt_limit` schedules the retry-window transition.
- Runtime banning is inclusive and explicit: `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`. Prism never derives the ban threshold from `cycle_retry_attempt_limit`.
- Upstream request timing is controlled by the shared backend timeout settings rather than per-strategy fields.

#### 6.4 Get Loadbalance Strategy
```
GET /api/loadbalance/strategies/{strategy_id}
```
Response `200`: Strategy object in the effective profile scope.
Returns `404` when the strategy does not exist in the effective profile.

#### 6.5 Update Loadbalance Strategy
```
PUT /api/loadbalance/strategies/{strategy_id}
```
Request: Full replacement of mutable strategy fields using the same shape as create.
Response `200`: Updated strategy object.

Strategy responses include the persisted explicit Ban Policy strategy document:

```json
{
  "id": 12,
  "profile_id": 1,
  "name": "round-robin-primary",
  "legacy_strategy_type": "round-robin",
  "failure_status_codes": [403, 422, 429, 500, 502, 503, 504, 529],
  "ban_mode": "temporary",
  "retry_base_delay_ms": 45000,
  "retry_backoff_multiplier": 3.5,
  "retry_jitter_ratio": 0.2,
  "retry_max_delay_ms": 720000,
  "cycle_retry_attempt_limit": 3,
  "ban_cumulative_retry_attempt_threshold": 6,
  "ban_duration_seconds": 1800,
  "attached_model_count": 2,
  "created_at": "2026-03-25T08:00:00Z",
  "updated_at": "2026-03-25T08:05:00Z"
}
```

#### 6.6 Delete Loadbalance Strategy
```
DELETE /api/loadbalance/strategies/{strategy_id}
```
Response `200`: `{ "deleted": true }`.
Returns `409` when the strategy is still attached to one or more models; the response detail includes `attached_model_count`.

#### 6.7 List Current Loadbalance State for a Model
```
GET /api/loadbalance/current-state
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `model_config_id` | integer | — | Model config ID in the effective profile (required, `>=1`) |

Response `200`:
```json
{
  "items": [
    {
      "connection_id": 12,
      "window_started_at": "2026-03-30T08:00:00Z",
      "window_request_count": 4,
      "in_flight_non_stream": 1,
      "in_flight_stream": 0,
      "cycle_retry_attempts": 2,
      "cumulative_retry_attempts": 5,
      "next_retry_at": "2026-03-30T08:02:00Z",
      "last_retry_delay_ms": 60000,
      "ban_mode": "temporary",
      "banned_until_at": "2026-03-30T08:30:00Z",
      "last_failure_kind": "transient_http",
      "last_success_at": null,
      "last_success_response_headers_latency_ms": 540,
      "state": "banned",
      "created_at": "2026-03-30T08:00:00Z",
      "updated_at": "2026-03-30T08:01:00Z"
    }
  ]
}
```

Returns `404` when the model config does not exist in the effective profile.

`state` is derived from the connection-global Ban Policy runtime state and is one of `available`, `retry_wait`, or `banned`. `until_reset` bans stay `banned` until the current-state reset endpoint clears them; temporary bans stay `banned` until `banned_until_at`; retry windows stay `retry_wait` until `next_retry_at`; otherwise the connection is `available`. Current-state items expose QPS and in-flight admission counters plus live retry-cycle counters for each private connection directly owned by the model. They intentionally omit `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` because current state is connection-global, while policy thresholds belong to the model strategy snapshot recorded on events.

#### 6.8 Reset Current Loadbalance State for a Connection
```
POST /api/loadbalance/current-state/{connection_id}/reset
```
Response `200`:
```json
{
  "connection_id": 12,
  "cleared": true,
  "state": {
    "connection_id": 12,
    "state": "available",
    "last_success_response_headers_latency_ms": 540
  }
}
```

Reset is a narrow cooldown reset: it clears `cycle_retry_attempts`, `cumulative_retry_attempts`, `next_retry_at`, `last_retry_delay_ms`, `ban_mode`, `banned_until_at` and `last_failure_kind`, and updates `updated_at`. It preserves the QPS window/count, both in-flight counters, `last_success_at`, `last_success_response_headers_latency_ms`, round-robin cursors and `created_at`. `cleared=false` with `state=null` is returned when the process has no local state; `cleared=false` with an unchanged snapshot is returned when there is nothing left to clear. Unknown or cross-profile connection ids return `404`. The route does not invalidate the runtime planning cache. This state is intentionally ephemeral and is lost on backend restart; retained SQL runtime-state tables are compatibility schema, not the production hot path.

#### 6.9 List Loadbalance Events
```
GET /api/loadbalance/events
```
Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `model_id` | string | — | Filter by model ID (required) |
| `limit` | integer | 50 | Max results (1-200) |
| `offset` | integer | 0 | Pagination offset |

Response `200`:
```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 1,
      "connection_id": 1,
      "event_type": "banned",
      "failure_kind": "transient_http",
      "cycle_retry_attempts": 2,
      "cumulative_retry_attempts": 6,
      "cycle_retry_attempt_limit": 3,
      "ban_cumulative_retry_attempt_threshold": 6,
      "next_retry_at": "2026-03-30T08:02:00Z",
      "last_retry_delay_ms": 60000,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "ban_mode": "until_reset",
      "banned_until_at": null,
      "last_success_at": null,
      "summary": {
        "event": "Connection was banned",
        "reason": "The retryable HTTP failure pushed cumulative retry attempts to 6, meeting the configured cumulative ban threshold of 6 attempts.",
        "operation": "Prism removed this connection globally until the ban expires or an operator resets it.",
        "cooldown": "1 minute"
      },
      "created_at": "2026-03-30T08:01:00Z"
    }
  ],
  "total": 15,
  "limit": 50,
  "offset": 0
}
```

Loadbalance event types are `retry_scheduled`, `retry_exhausted`, `banned`, `unbanned`, `recovered`, and `admission_rejected`. They record retry-cycle attempts, cumulative attempts, next retry timing, last retry delay, optional ban metadata, optional success time, and the model, endpoint, and vendor snapshots for operator review. Events produced by Ban Policy evaluation also expose immutable policy snapshots as `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold`, so historical event detail explains the threshold that was active when the event was written.

#### 6.10 List Loadbalance Incidents
```
GET /api/loadbalance/incidents
```

Query parameters:
| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | integer | 50 | Max recent incident events |
| `since_hours` | integer | 24 | Recent event lookback window |

Response `200`:
```json
{
  "active_bans": [
    {
      "connection_id": 3,
      "ban_mode": "temporary",
      "banned_until_at": "2026-03-30T08:30:00Z",
      "cumulative_retry_attempts": 7,
      "next_retry_at": null,
      "state": "banned",
      "created_at": "2026-03-30T08:00:00Z",
      "updated_at": "2026-03-30T08:01:00Z"
    }
  ],
  "recent_events": [
    {
      "id": 22,
      "profile_id": 1,
      "connection_id": 3,
      "event_type": "banned",
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "summary": {
        "event": "Connection was banned",
        "reason": "The retryable HTTP failure pushed cumulative retry attempts to 7.",
        "operation": "Prism removed this model-private connection from routing until the ban expires or an operator resets it.",
        "cooldown": "1 minute"
      },
      "created_at": "2026-03-30T08:01:00Z"
    }
  ],
  "generated_at": "2026-03-30T08:05:00Z"
}
```

`active_bans` is the current Ban Policy runtime state for the effective profile. `recent_events` uses the loadbalance event item shape and includes recent `banned`, `unbanned`, `recovered`, and `retry_exhausted` rows without requiring a `model_id` filter.

#### 6.11 Get Loadbalance Event Detail
```
GET /api/loadbalance/events/{id}
```
Response `200`: Single event object with the same Ban Policy retry-window metadata, policy snapshot fields, and summary fields as the list item.

#### 6.12 Loadbalance Event Retention

Loadbalance event list and detail APIs remain pinned to Default profile id `1`. Normal cleanup is global and uses `POST /api/maintenance/log-retention/jobs` with `table = "loadbalance_events"`, or the stored `loadbalance_events_retention_days` value from `/api/settings/log-retention`.

The retired load-balance cleanup endpoint is not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

---

### 7. Auth API

Auth APIs are global and do not require `X-Profile-Id`.

#### 7.1 Auth Status
```
GET /api/auth/status
```
Response `200`:
```json
{
  "auth_enabled": true
}
```

#### 7.2 Public Bootstrap
```
GET /api/auth/public-bootstrap
```
Initializes session and returns basic auth state for the login page.
Response `200`:
```json
{
  "authenticated": false,
  "auth_enabled": true,
  "username": null
}
```

#### 7.3 Login
```
POST /api/auth/login
```
Request:
```json
{
  "username": "admin",
  "password": "password123",
  "session_duration": "7_days"
}
```
Response `200`: Session object. Sets the configured access-token and refresh-token cookies.
```json
{
  "authenticated": true,
  "auth_enabled": true,
  "username": "admin"
}
```

#### 7.4 Logout
```
POST /api/auth/logout
```
Clears session cookies and revokes the current refresh token.
Response `200`:
```json
{
  "authenticated": false,
  "auth_enabled": true,
  "username": null
}
```

#### 7.5 Refresh Session
```
POST /api/auth/refresh
```
Uses the `refresh_token` cookie to issue a new session. Implements token family rotation. Response `200` uses the same session object shape as login.

#### 7.6 Get Session
```
GET /api/auth/session
```
Returns the current authenticated session state.
Response `200`:
```json
{
  "authenticated": true,
  "auth_enabled": true,
  "username": "admin"
}
```

---

### 8. Error Responses

Prism does not have one universal error envelope. Management handlers normally return:
```json
{
  "detail": "human-readable detail"
}
```

Profile-resolution failures add a stable `code`:
```json
{
  "code": "profile_scope_profile_not_found",
  "detail": "Profile 1 not found"
}
```

Audit and statistics domain-validation errors use a nested structured envelope. `details` is present only when the domain error includes structured detail:
```json
{
  "error": {
    "code": "audit_window_too_large",
    "message": "Audit event windows may not exceed 7 days.",
    "details": {
      "max_window_seconds": 604800
    }
  }
}
```

Runtime handlers return `detail`, and include `error` only when the runtime error has a machine-readable code:
```json
{
  "error": "admission_exhausted",
  "detail": "No eligible Terminal Target passed admission."
}
```

The request-size guard is a separate envelope with `error`, `message`, and `limit_bytes`, as documented above.

| Status Code | Meaning |
|---|---|
| 400 | Bad request (invalid input) |
| 401 | Management session or runtime proxy key is missing or invalid |
| 405 | Wrong method for an allowlisted runtime path |
| 404 | Resource not found |
| 409 | Conflict (duplicate scoped identifier) |
| 413 | Request body exceeds the mounted route limit |
| 502 | Upstream service error |
| 503 | Runtime routing/admission unavailable or another service snapshot is unavailable |

---

### 10. API Reference Source

This markdown document is the source of truth for current runtime and management API semantics.

## 15. Data Model Reference


Scope: profile-isolated runtime and management model with pricing templates, profile-scoped explicit Ban Policy routing, retained compatibility hot-state schema, process-local runtime state, endpoint label snapshots, and user-agent client rules.

### 1. Entity Relationship Diagram

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
  last_success_response_headers_latency_ms
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

### 2. Table Definitions

#### 2.1 `profiles`

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

#### 2.2 `model_configs` (profile-scoped)

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
- OpenAI relation coverage is operation-set based. Valid Partial/None capability relations may be persisted with non-persisted `configuration_warnings`; the first-party UI prevents selecting a zero-intersection target and warns on Partial. Invalid enum/nullability remains a hard management `422`; there is no exact-mode equality or startup preflight invariant.
- Public model authoring uses ordered rows in `model_access_targets` to reach same-family model targets. Internal connection target rows own and route to Terminal Targets, Prism's product-facing model-private endpoint bindings.
- Runtime compatibility is checked against `api_family`.
- Exact facade routing, model-owned context capability, and overflow-promotion authoring fields are retired.

#### 2.3 `model_access_targets` (profile-scoped model access metadata)

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
- Positions are normalized and validated as contiguous `0..N-1` across both target types in management contracts; creates append to the shared storage tail, delete compacts across types, enable/disable never moves a row, and `PATCH .../position` moves within that storage list. Runtime still filters the stored rows into separate Model Target and Terminal Target stages.
- Position is an ordering key only, not priority, tier, or weight. Duplicate positions reject before write.
- Obsolete public payload keys `weight` and `target_priority` reject in management model APIs. The fresh schema has no columns for those values. `connections.priority` remains a legacy read-compatibility column and never participates in access-target ordering.
- Runtime routing sorts enabled rows by flat `position` and stable IDs, then evaluates Model Target rows first and direct Terminal rows only as fallback. `single`, `fill-first`, and `round-robin` act independently within the entered stage; Model Target rows recurse through the child model's own strategy, and direct Terminal rows contribute their own attempt.
- Go management validation rejects self-reference, cross-profile targets, cross-api-family targets, and cycles; these relationship semantics are not enforced by database triggers.

#### 2.4 `loadbalance_strategies` (profile-scoped reusable routing behavior)

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

#### 2.5 `endpoints` (profile-scoped credentials)

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

#### 2.6 `connections` (profile-scoped Terminal Target storage)

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
| custom_request_parameters | JSONB | NULLABLE | Optional static top-level JSON object overlaid onto every upstream attempt body; `NULL`/`{}`/`null` all mean unconfigured; CHECK constraint `connections_custom_request_parameters_object` requires `NULL` or a JSON object root |
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
- `openai_text_capability` is the connection-owned OpenAI text runtime capability source of truth for planning. `responses_only` supports native Responses generation and Responses adjunct operations, `chat_completions_only` supports native Chat Completions, and `dual_native` supports both native text generation shapes. The requested model's `openai_accepted_format` gates the ingress operation with `400 openai_operation_not_supported`; incompatible Terminal Target connections are skipped in authored order so later native attempts remain eligible, and the final planning rejection is classified as `503 openai_no_compatible_terminal_target` (no compatible leaf in the authored graph) or `503 openai_no_eligible_terminal_target` (compatible leaves exist but none is statically eligible). Ordinary availability exhaustion remains `503`.
- `openai_probe_endpoint_variant` is retained for existing rows; live Terminal Target authoring uses `openai_text_capability` for OpenAI runtime planning.

#### 2.7 `pricing_templates` (profile-scoped reusable token pricing)

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


#### 2.8 `header_blocklist_rules` (mixed scope)

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

#### 2.9 `user_settings` (profile-scoped singleton)

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

#### 2.10 `endpoint_fx_rate_settings` (profile-scoped)

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

#### 2.11 `request_logs` (partitioned immutable profile attribution)

Telemetry rows have immutable profile attribution captured at request start. Captured upstream attempts in materialized execution envelopes produce one row each. Telemetry-eligible target-resolution or native-compatibility planning failures carrying `PlanningFailure`, plus execution failures accepted by the runtime telemetry path, produce synthetic failure rows without an endpoint or connection. The table is range-partitioned by UTC `created_at` day. The partition-compatible primary key is `(created_at, id)`, with `id` still sequence-backed for lookup convenience.

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
| operation_translation_mode | VARCHAR(80) | NULLABLE | Current writes use `none`/NULL; retired translation values remain readable on historical rows |
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
- Target-resolution errors attach `PlanningFailure` for HTTP `503` or the `openai_operation_not_supported` planning rejection; those telemetry-eligible planning failures, plus execution failures that enter the runtime failure telemetry path (currently `admission_exhausted`), can write a synthetic row with no `endpoint_id` or `connection_id`.
- Earlier errors such as malformed request bodies, unknown models, and API-family mismatches do not carry `PlanningFailure` and do not write synthetic history.
- When all launched transport attempts fail and execution returns its terminal `502`, the current executor drops its captured attempt list and does not materialize request or usage history for that failure.
- Unsupported or wrong-method requests rejected by the operation registry write no request log, audit log, usage event, or telemetry-outbox row.
- `ingress_request_id` groups the rows created by one incoming runtime request.
- `attempt_number` preserves retry/failover ordering within that group.
- `model_id` records the requested model ID while `resolved_target_model_id` records the final target model ID selected for that attempt.
- `operation_name` is nullable in the schema for compatibility, but materialized rows for registered operations, including synthetic failures, carry a non-empty canonical operation name. Registry rejection creates no row and therefore has no persisted operation name.
- `operation_name` and `request_path` remain ingress-led. `upstream_operation_name`, `operation_translation_mode`, and `upstream_request_path` are additive upstream attribution. Current OpenAI attempts are native and use `none`/NULL; historical translation values remain intact.
- `selected_terminal_target_id` can differ from `connection_id` when the planner selected one terminal target but execution later failed over to another attempt.
- `stream_error_detail` is exposed only by exact request-log detail reads. List and dashboard recent-activity payloads expose `stream_outcome` and `stream_error_kind` without detail text.
- Prism prices only observed usage. `STREAM_USAGE_UNAVAILABLE` marks interrupted or no-terminal stream rows where required tokens are absent; completed streams missing required usage keep `MISSING_TOKEN_USAGE`.
- Token usage fields are canonical disjoint components. `input_tokens` is base input only, `output_tokens` is base output only, and cache-read input, cache-creation input, and reasoning output stay in their split fields.
- Pricing snapshots persist the five concrete pricing strings used for the attempt. Explicit `"0"` prices mean configured free pricing, while absent or invalid pricing snapshots and missing FX data stay unpriced with `MISSING_PRICE_DATA`.

#### 2.12 `usage_request_events` (partitioned immutable usage attribution)

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
| operation_translation_mode | VARCHAR(80) | NULLABLE | Current finalized attempts use `none`/NULL; historical translation values are preserved |
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

#### 2.13 `audit_logs` (partitioned immutable profile attribution)

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
- OpenAI audit capture stores native upstream request and response bodies.
- Request and response bodies are not redacted. Other request-header values and all response-header values can also contain sensitive data.

#### 2.14 `profile_api_family_audit_settings` (profile-scoped audit policy)

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

#### 2.15 `loadbalance_events` (partitioned immutable profile attribution)

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

#### 2.16 `log_retention_settings` (global singleton)

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

#### 2.17 `management_jobs` (durable management work queue)

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

#### 2.18 `management_job_events`

Append-only event stream for management job status and progress.

| Column | Type | Constraints | Description |
|---|---|---|---|
| id | BIGINT | PK, sequence-backed | Unique identifier |
| job_id | TEXT | FK -> management_jobs.id, NOT NULL, ON DELETE CASCADE | Owning job |
| event_type | TEXT | NOT NULL | Event kind such as `created` or `cancel_requested` |
| message | TEXT | NOT NULL, DEFAULT empty string | Safe operator-facing event message |
| rows_deleted | BIGINT | NOT NULL, DEFAULT 0 | Rows deleted by the event batch |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT now() | Event timestamp |

#### 2.19 `routing_connection_runtime_state` (retained compatibility schema, `UNLOGGED`)

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
| last_success_response_headers_latency_ms | INTEGER | NULLABLE | Last successful upstream attempt latency from request start to response-headers receipt (ms); not a percentile, TTFT or total time |
| created_at | TIMESTAMPTZ | NOT NULL | Row creation timestamp; application-managed |
| updated_at | TIMESTAMPTZ | NOT NULL | Last mutation timestamp; application-managed |

Constraints:
- `UNIQUE(profile_id, connection_id)`.
- Admission and retry counters are non-negative.
- `ban_mode` is restricted to `off`, `temporary`, or `until_reset`.
- `last_failure_kind` is restricted to `transient_http`, `connect_error`, or `timeout` when present.

The columns document the retained schema only. They do not describe the current production state source.

#### 2.20 `routing_connection_runtime_leases` (retained compatibility schema, `UNLOGGED`)

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

#### 2.21 `app_auth_settings` (singleton)

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

#### 2.22 `refresh_tokens`

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

#### 2.23 `proxy_api_keys`

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

#### 2.24 Additional Live Platform Tables

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

### 3. Selected Indexes, Constraints, and Foreign Keys

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

### 4. Relationship and Ownership Rules

- Profile-scoped entities include `model_configs`, `model_access_targets`, `loadbalance_strategies`, `endpoints`, `connections`, `pricing_templates`, `user_settings`, `endpoint_fx_rate_settings`, `profile_api_family_audit_settings`, `runtime_telemetry_outbox`, requesting-profile `audit_delete` jobs, user `header_blocklist_rules`, and user `user_agent_client_rules`.
- `routing_connection_runtime_state`, `routing_connection_runtime_leases`, and `loadbalance_round_robin_state` retain profile identifiers as compatibility schema, but they are not the production runtime-state source.
- `app_auth_settings` is the singleton auth root for `refresh_tokens` and `proxy_api_keys`.
- `request_logs`, `usage_request_events`, `audit_logs`, and `loadbalance_events` keep immutable `profile_id` attribution and are not rewritten when the runtime profile snapshot changes.
- `request_logs.ingress_request_id` is the canonical operator drill-in key for grouped request investigation.
- `audit_logs` intentionally has no foreign key to partitioned `request_logs`; its request identifiers are weak historical metadata.
- Cross-profile resource lookups are treated as not found (`404`) because management scope is pinned to Default profile id `1`.
- Private connection create/update must enforce profile consistency between the connection and endpoint references. The single owner is enforced through `model_access_targets.target_connection_id`.

### 5. Deletion and Retention Semantics

- Profile deletion routes are not exposed while multi-profile management is frozen.
- Historical telemetry/audit retention is independent; routine profile delete does not erase historical attribution rows.
- Proxy-key hard deletion clears foreign-key IDs from request and usage history but leaves stored name snapshots intact.
- Partition retention can drop whole UTC-day child tables and delete only the cutoff-overlapping boundary rows; deletion counts do not estimate rows removed by partition drops.

### 6. Runtime Isolation Notes

- Proxy routing always resolves against frozen Default profile id `1`.
- Production creates a fresh `LocalRuntimeStateStore` on process startup. Connection admission counters, Ban Policy state, latency signals, connection round-robin cursors, and access-target round-robin cursors live in process memory.
- The process-local store scopes connection state by profile and connection, and round-robin state by profile/model or profile/source-model/strategy/target-set. A normal restart, crash, or replacement process loses all of this state.
- Production does not reload, reconcile, compact, or repair process-local state from `routing_connection_runtime_state`, `routing_connection_runtime_leases`, or `loadbalance_round_robin_state`.
- Management current-state and active-ban reads query the process-local provider. Reset operations delete process-local connection state and the associated model round-robin cursor.
- Failures are classified as `transient_http`, `connect_error`, or `timeout`; retryable HTTP responses use the same retry-window delay/backoff/jitter policy path as transport failures.
- Ban Mode thresholding uses cumulative retry attempts for the private connection owned by the terminal model path.
- Non-retryable client errors do not force-clear existing process-local current state; successful `2xx` responses clear local retry and ban state for the connection.
- Header blocklist at runtime is resolved as: all enabled system rules + enabled user rules for frozen Default profile id `1`.

### 7. Invariant Notes

- Runtime hot state is process-local and is reset on every process start.
- The baseline migration remains the exact source for PostgreSQL column types, sequences, constraints, indexes, and foreign keys.
