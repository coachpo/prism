# Prism Workflows Reference

This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in `frontend/src/app/router/appRouter.tsx`, `frontend/src/app/router/rewriteRoutes.ts`, the live Go backend API surface, and the markdown API reference.

Validated again against current repo surfaces on 2026-07-10:
- `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` are all `1.0.4`, which is the current backend/frontend version surface.
- The protected frontend route shell mounts observe, request-log, model, route, settings, proxy-key, and pricing workflows; analytics lives under `/observe`.

## Evidence Sources

- Frontend route surface: `frontend/src/app/router/appRouter.tsx` and `frontend/src/app/router/rewriteRoutes.ts`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/useShellNavigation.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Default-profile scoping: `frontend/src/lib/api/core.ts`, `frontend/src/lib/api/profileScope.ts`
- Backend router assembly: `backend/internal/httpapi/management/`, `backend/internal/httpapi/runtime/`, and `backend/internal/platform/http/server.go`
- Backend API reference: `docs/API_SPEC.md`
- Request-log details: `docs/REQUESTS_PAGE.md`

## Runtime URLs

- Frontend: `http://localhost:5173`
- Backend: `http://localhost:8000` for a fresh repo-local bootstrap seed; existing selected bootstrap files can choose another backend port
- Health: `http://localhost:8000/health` for that fresh seed

## Shared Scope Rules

- Public auth routes are `/auth/login`.
- Protected shell routes cover `/observe`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/models`, `/models/:id`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, and `/control/proxy-keys`; analytics is under `/observe?tab=analytics`.
- Profile-scoped management requests are pinned to Default profile id `1`. `X-Profile-Id` is still accepted for compatibility, but the backend ignores its value.
- Global management routes omit `X-Profile-Id` and include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, and `POST /api/maintenance/log-retention/jobs`.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and resolves against frozen Default profile id `1`.

## 1. Sign In And Session Bootstrap

**User entrypoints**

- `/auth/login`

**Frontend flow**

1. `AuthProvider` chooses public bootstrap mode for auth-only routes.
2. The login page loads auth state before showing the form.
3. The login form offers `session`, `7_days` (the backend default), and `30_days` session durations.
4. Successful login returns to the safe local `redirect` path captured from the full protected pathname, query string, and hash; without a valid redirect it falls back to `/observe`.
5. Passive and proactive refresh keep the operator session alive while the tab stays active.

**UI-driven backend touchpoints**

- `GET /api/auth/public-bootstrap`
- `GET /api/auth/status`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `POST /api/auth/refresh`
- `GET /api/auth/session`

## 2. Shell Bootstrap

**User entrypoints**

- Any protected route

**Frontend flow**

1. `AuthProvider` confirms the operator session.
2. The shell renders sidebar groups, breadcrumbs, language/theme controls, and the version label.
3. Default-profile pages send the pinned compatibility `X-Profile-Id: 1` header from the shared API client.

**UI-driven backend touchpoints**

- `GET /api/auth/status`
- Default-profile-scoped `/api/*` routes with accepted-but-ignored `X-Profile-Id`

## 3. Dashboard And Statistics

**User entrypoints**

- `/observe`
- `/observe?tab=analytics`

**Frontend flow**

1. Dashboard overview bootstrap loads KPI cards, spending summaries, and average RPM/metric snapshots from the stats-only aggregate snapshot.
2. Dashboard overview bootstrap loads recent activity from the separate recent-activity feed.
3. The dashboard also loads current loadbalance incident state for active-ban and recent-event alerts. The backend snapshot's `routing_health_map` is a response field only and is not rendered by the current dashboard.
4. The dashboard polls REST stats endpoints every 30 seconds for aggregate and recent-activity reconciliation.
5. Quick actions send operators into the analytics tab or `/observe/requests` for deeper analysis.
6. The analytics tab stays aggregate-focused and uses its own snapshot presets rather than request-level drill-down.

**Backend touchpoints**

- `GET /api/stats/dashboard` for the overview aggregate snapshot; its backend-computed `routing_health_map` response field is not rendered by the current dashboard
- `GET /api/stats/dashboard/recent-activity` for bounded request-history-backed dashboard activity
- `GET /api/loadbalance/incidents` for active bans and recent loadbalance incidents
- `GET /api/stats/usage-snapshot` for API callers, debugging, analytics polling, and manual refresh
- `GET /api/stats/endpoints/{endpoint_id}/models` for analytics endpoint drilldown rows

## 4. Model Management And Model Detail

**User entrypoints**

- `/models`
- `/models/:id`

**Frontend flow**

1. Operators list, search, create, edit, and delete model configs.
2. Model create and edit dialogs manage model metadata, OpenAI accepted format, loadbalance strategy, and enabled state.
3. Model detail owns ordered same-family access-target authoring and Terminal Target management for the model's private endpoint bindings.
4. Request logs preserve the requested model while final-target fields show the terminal model reached through the access graph.

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

## 5. Endpoints, Loadbalance Strategies, And Pricing Templates

**User entrypoints**

- `/route/endpoints`
- `/route/ban-policies`
- `/route/pricing`

**Frontend flow**

1. Endpoints define reusable upstream credentials and base URLs that Terminal Targets can share.
2. The Ban Policies page exposes only `Strategies`, `Current State`, and `Events`; it does not render incidents. The dashboard consumes `/api/loadbalance/incidents` for its incident banner and recent-event alerts.
3. Pricing templates define reusable cost models attached to Terminal Targets with five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`.
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
- `PATCH /api/endpoints/{endpoint_id}/position`
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

## 6. Request Investigation

**User entrypoints**

- `/observe/requests`
- Dashboard deep links into `/observe/requests`

**Frontend flow**

1. Operators browse request history with server-backed filters.
2. Exact request investigation opens the overview detail sheet through `request_id`.
3. `ingress_request_id` groups all upstream attempts for one incoming proxy request.
4. The request-log UI keeps the requested `model_id` separate from the final `resolved_target_model_id` so operators can see authoring intent and execution target at the same time.
5. The Client filter sends `client_rule_id` to the backend and matches caller User-Agent Client Rules against `caller_user_agent` only.
6. Audit payloads load only on the dedicated `/observe/requests/:requestId/audit` page; the detail sheet remains overview-only.

**Backend touchpoints**

- `GET /api/stats/requests`
- `GET /api/stats/requests/{request_id}`
- `GET /api/audit/logs`
- `GET /api/audit/logs/{log_id}`
- `GET /api/settings/timezone`

For the page-specific query contract and UI behavior, see `docs/REQUESTS_PAGE.md`.

## 7. Settings And Access Control

**User entrypoints**

- `/system/settings`
- `/control/proxy-keys`

**Frontend flow**

1. Settings visibly splits into `全局` and `实例` tabs. Their internal query values remain `profile` and `global`.
2. The `全局` tab covers reporting currency and FX mappings, timezone, audit/privacy defaults, and config rules. Rows with missing FX data remain pricing failures; explicit `"0"` component prices are configured free pricing and do not become `MISSING_PRICE_DATA`.
3. The `实例` tab covers operator authentication, log-retention policies, and deletion requests.
4. Startup settings remain in the plaintext bootstrap file selected by `PRISM_CONFIG_PATH`; edit `config.json` directly and restart Prism to apply changes.
5. Saving a transition from disabled to enabled authentication broadcasts a localStorage auth-state update, refreshes local auth state, and redirects the current tab to `/auth/login`; other open tabs re-bootstrap from the broadcast.
6. Proxy API keys are managed on their own route and stay global rather than profile-scoped. They can be issued while auth is disabled, but runtime proxy-key authentication is enforced only after operator auth is enabled.

Mail bootstrap fields remain parse-compatible for existing `config.json` files, but Prism no longer sends mail. Fresh bootstrap seeds use backend `8000`, frontend `5173`, and PostgreSQL `15432`, but `./start.sh` follows the existing bootstrap file's configured `server.port` when one already exists. `runtime.transport.requestTimeout` is seeded as `"300s"`, and `runtime.sideEffects.attemptTimeout` is seeded as `"10s"`. Direct external `config.json` edits are not watched automatically, and existing valid files are not rewritten by the launcher. To reset startup defaults, stop Prism, remove or relocate the bootstrap file, and restart.

OpenAI text routing is native-only. Operators set runtime support on each Terminal Target through `openai_text_capability`, using `responses_only`, `chat_completions_only`, or `dual_native`; incompatible Chat Completions/Responses attempts are skipped rather than translated.

**Backend touchpoints**

- `GET /api/settings/costing`
- `PUT /api/settings/costing`
- `GET /api/settings/timezone`
- `PUT /api/settings/timezone`
- `GET /api/settings/audit`
- `PUT /api/settings/audit`
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
- `GET /api/settings/auth`
- `PUT /api/settings/auth`
- `GET /api/settings/auth/proxy-keys`
- `POST /api/settings/auth/proxy-keys`
- `PATCH /api/settings/auth/proxy-keys/{key_id}`
- `POST /api/settings/auth/proxy-keys/{key_id}/rotate`
- `DELETE /api/settings/auth/proxy-keys/{key_id}`
- `GET /api/settings/log-retention`
- `PUT /api/settings/log-retention`
- `POST /api/maintenance/log-retention/jobs`

Global log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.

The Settings UI exposes fixed deletion presets of `1`, `7`, `30`, `90` days, or all data, then creates a durable retention job and shows its `job_id`/`status_url` in a toast. The retention API accepts explicit cutoff timestamps, while `GET /api/management/jobs`, `GET /api/management/jobs/{job_id}`, and `POST /api/management/jobs/{job_id}/cancel` are API-only follow-up endpoints; the current UI does not list, inspect, or cancel jobs.

## 8. Runtime Proxy Traffic

Runtime auth follows the latest proxy-key snapshot immediately after auth and proxy-key management writes: rotated, disabled, or expired keys stop authorizing new supported `/v1` and `/v1beta` runtime operations. Create and rotate return a secret shown once in a copyable dialog; the ledger keeps key metadata and rotation lineage, while DELETE removes the row.

**User entrypoints**

- External clients calling one of the operation-registered runtime routes listed below

**Runtime flow**

1. Global CORS runs first. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the exact operation registry in that order. Once inside the registry, unsupported routes and wrong methods reject before body reads, provider transport, telemetry, audit, feedback, or runtime side effects.
2. Provider adapters parse provider-specific payloads, build upstream requests, adapt responses, classify streams, extract usage, and own pure OpenAI Chat/Responses conversion.
3. Planning evaluates all ordered same-family model targets into an eligible Terminal Target aggregate; direct private Terminal Targets are used only when no model-target candidate is eligible.
4. Connection planning applies the attached explicit Ban Policy strategy and per-connection limits.
5. The shared runtime/gateway owns operation registration, admission, routing, SSE lifecycle, accounting, pricing, request-log metadata, and durable handoff. Telemetry/audit rows are materialized by background workers from the runtime outbox; non-accepted side effects use their own in-memory or worker queues.
6. After the first downstream byte or event on a stream, no retry, redirect, or hedge replay can start.
7. Missing pricing stays visibly degraded or unpriced, it never silently looks complete.

**Backend touchpoints**

- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/responses/input_tokens`
- `POST /v1/responses/compact`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1beta/models/{model}:streamGenerateContent`
- `POST /v1beta/models/{model}:countTokens`

These 10 allowlisted runtime routes are defined in `backend/internal/httpapi/runtime/operations.go` and are intentionally separate from `/api/*` management routes. Prism does not treat `/v1` or `/v1beta` as catch-all prefixes.
`GET /v1/models` remains local: without `client_version` it returns the OpenAI-shaped list; with `client_version` it returns the current Codex catalog, seeded from the embedded copy and refreshed asynchronously from OpenAI at startup and every 24 hours, plus a content-derived weak `ETag` and `304 Not Modified` on an exact `If-None-Match`.

## 9. Priority Operations Runbook

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

## Cross-References

- Product scope: `docs/PRD.md`
- API contracts: `docs/API_SPEC.md`
- Request investigation details: `docs/REQUESTS_PAGE.md`
