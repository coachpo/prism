# Operator Workflows

This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in `frontend/src/app/router/appRouter.tsx`, `frontend/src/app/router/rewriteRoutes.ts`, the live Go backend API surface, and the markdown API reference.

The protected frontend route shell mounts observability, model configuration, endpoints, routing strategies, pricing, settings, and proxy-key workflows. See [Operations](operations.md) for deployment procedures and [Project Status](../STATUS.md) for the deployed-instance record.

## Evidence Sources

- Frontend route surface: `frontend/src/app/router/appRouter.tsx` and `frontend/src/app/router/rewriteRoutes.ts`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/useShellNavigation.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Default-profile scoping: `frontend/src/lib/api/request.ts`, `frontend/src/lib/api/profileScope.ts`
- Backend router assembly: `backend/internal/httpapi/management/`, `backend/internal/httpapi/runtime/`, and `backend/internal/platform/http/server.go`
- Backend API reference: [HTTP API references](../ARCHITECTURE.md#api-contracts)
- Request-log details: [Requests workflow contract](requests.md)

## Runtime URLs

The [quick start](../README.md#quick-start) gives local URLs and [Operations](operations.md) explains port selection. [Project Status](../STATUS.md) owns the deployed-instance inventory. Paths below are relative to the chosen Prism origin. The legacy `/models` and `/models/:id` paths redirect to `/route/models` and `/route/models/:id`; legacy Observe `overview`/`analytics` values open Trend, and `events` redirects to `/observe/routing-health`.

## Shared Scope Rules

- Public auth routes are `/auth/login`.
- Protected shell routes cover `/observe`, `/observe/routing-health`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/route/models`, `/route/models/:id`, `/route/models/export`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, and `/system/proxy-keys`; the canonical trend view is `/observe?tab=trend`.
- Profile-scoped management requests are pinned to Default profile id `1`. `X-Profile-Id` is still accepted for compatibility, but the backend ignores its value.
- Global management routes omit `X-Profile-Id` and include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and resolves against frozen Default profile id `1`.

## 1. Sign In And Session Bootstrap

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

## 2. Shell Bootstrap

**User entrypoints**

- Any protected route

**Frontend flow**

1. `AuthProvider` confirms the operator session through the process-local coordinator; every blocking phase (`BOOTSTRAPPING`, `REFRESHING`, `SESSION_EXPIRED`, `LOGGING_OUT`, `AUTH_TRANSITION_FAIL_CLOSED`, `AUTH_UNAVAILABLE`, `AUTH_DISABLED_VERIFYING`) renders the global access layer instead of stale authenticated UI.
2. The shell groups pages under 使用情况, 模型与服务, and 访问与设置; 模型 precedes 服务 in the configuration group. The header contains breadcrumbs, global search, density, theme, and account controls; sign-out is available through the account menu when auth is enabled. The sidebar footer shows authentication state, its settings link, and a copyable build identifier. Interface copy uses the single Simplified Chinese locale.
3. Default-profile pages send the pinned compatibility `X-Profile-Id: 1` header from the shared API client.
4. Auth-disabled shells show the open-access state in the footer and explainer surfaces; issued proxy keys are never labeled as protecting access.

**UI-driven backend touchpoints**

- `GET /api/auth/status`
- Default-profile-scoped `/api/*` routes with accepted-but-ignored `X-Profile-Id`

## 3. Dashboard And Statistics

**User entrypoints**

- `/observe` (canonical dashboard; the root `/` landing gate redirects here for authenticated/disabled sessions)
- `/observe?tab=trend` (main metric trend)
- `/observe/routing-health` (current routing state and events timeline)

**Frontend flow**

1. The Now strip reads the rolling request count, RPM, TPM with token coverage, and enabled-model count. This rolling window is independent of the selected analysis preset.
2. Window KPI cards use one finalized-ingress query context and show request count, final completion rate, TTFT P95, measured output rate, cache-read share, and known cost with coverage and pricing evidence. Final completion includes stream/transport/client-disconnect outcomes; HTTP status success alone does not establish completion.
3. Trend and Errors share a selected-scope context. Their metric and grouping controls expose only the current scope's catalog; changing the cohort withdraws stale data before the next response. Errors links carry the signed cohort into Requests.
4. Activity loads finalized ingress rows in pages of 20 and opens the matching Requests chain. Terminal Targets uses its own endpoint-based final-execution or route-attempt view. Each fragment keeps its own loading, failure, missing-data, and retained-coverage state.
5. Entry, URL changes, manual refresh, and explicit retry trigger reads. Automatic refresh defaults to off, with explicit 30/60-second choices, hidden-tab pause, no overlapping cycles, and cancellation/reset on leaving or changing identity. The freshness bar exposes the returned timestamps, and a failed refresh does not turn retained data into fresh data or synthetic zeros. A block under the bar shows its own timestamp only when it was read at another time than the bar reports or is being shown stale.
6. The setup card groups existing readiness facts into connecting a model, preparing cost reporting, and client access. Its underlying routing progress still counts four configuration facts on one `route_witness_generation`, with only direct-entry models qualifying as roots. A persistent verification action distinguishes saved configuration from an observed model response; fresh completion collapses the card after focus leaves while the disclosure remains available. Once configuration is complete, the page drops the card while the selected window already holds a completed request.
7. Routing Health is a separate `/observe/routing-health` page with current process state and the retained events timeline. Current State is independent of the event time window; the timeline uses its own bounded signed context and shows coverage gaps.

**Backend touchpoints**

- `GET /api/stats/dashboard/now` for the rolling Now strip
- `GET /api/stats/query-context` for signed preset/scope bounds and coverage
- `GET /api/stats/usage-summary` for fixed-ingress Window KPIs
- `GET /api/stats/usage-series` and `/api/stats/usage-errors` for scoped analysis
- `GET /api/stats/observe-activity` for paginated finalized-ingress activity
- `GET /api/stats/endpoints/{endpoint_id}/terminal-targets` for the selected endpoint's scoped target statistics
- `POST /api/loadbalance/events/query-context`, `GET /api/loadbalance/events`, and `GET /api/loadbalance/current-state` for Routing Health
- Setup card existing-owner reads: endpoint, pricing-template, routing-policy, model and model-detail APIs, plus proxy-key setup readiness with the expected route-witness generation

The older dashboard aggregate, recent-activity, incident, usage-snapshot, and endpoint-model statistics endpoints remain API contracts; they are not the current Observe route's data sources.

## 4. Model Management, Model Detail, And Client Export

**User entrypoints**

- `/route/models`
- `/route/models/:id`
- `/route/models/export`

**Frontend flow**

1. The empty Models page offers “接入模型”. The form preselects a service passed by `action=create&endpoint_id=...`, selects the sole existing service when appropriate, or opens inline service creation when none exists. Operators can also save a disabled model and connect a service later. The default entries view can switch to Model Targets only or all configurations; incoming references and unreferenced-target warnings explain non-entry models.
2. Model create/edit separates the exact client model name from the model name provided by the service. The service model name initially follows the client model name until the user edits it; OpenAI format choices state that they must match the service. Direct-entry qualification, text/image dimensions, strategy, and enabled state keep their existing semantics. Successful enabled creation opens the model detail, and existing Terminal Target upstream identities remain independent of model renames.
3. Model detail owns access-target authoring as one mixed list: same-family Model Targets and Terminal Targets share the global `position` order, cross-type adjacent moves use the same controls, and Terminal Target management covers the model's private endpoint bindings. It also hosts the federated 外部目录来源 section: an independent models.dev panel (metadata, overrides, source-linked pricing entry) and an independent pi.dev panel (live candidate evidence, persisted binding, final Pi API, and the full bind/refresh/override/unbind workflows through the shared binding dialogs) — each panel loads, fails, and recovers on its own and neither can mask the other or the target configuration.
4. The Terminal Target dialog keeps request limits and custom headers alongside structured “服务附加设置” fields. Named text, number, switch, empty-value, group, and list values serialize to the unchanged request-parameter overlay contract; incomplete or invalid input stays editable with an inline error. The routing schedule remains a separate section because it governs routing eligibility.
5. The model detail's RouteReadiness card presents the static configuration conclusion, API family, strategy, and enabled/total counts for direct service connections and Model Targets. Missing service model identities remain explicit, and a model without a direct service connection is labeled accordingly. The card does not repeat the ordered connection list or follow Model Target rows recursively.
6. Model detail provides a client connection card with copyable address and model name, a client-key entry, and model-preselected verification. OpenAI clients use the effective Prism origin plus `/v1`; Anthropic and Gemini clients use the origin. Configuration readiness is neutral and does not claim that a real model request succeeded. Request records preserve the requested model separately from the final target.
7. The Models page opens the dedicated client-export route with Pi selected by default. The operator supplies the Prism origin/provider id and selects only structurally eligible direct-entry models with backend `readiness.status=ready`; unready rows keep independent binding-repair actions and become explicitly selectable after repair. Each model's row shows its live pi.dev candidate evidence and persisted binding status, and the operator changes the source (default exact candidate or an explicit bounded directory search, including a cross-directory model id), refreshes, or overrides a binding inline before typing a proxy key only at final confirmation. Directory reads go through Prism's backend; the browser never contacts pi.dev.
8. Choosing OpenCode switches to its independent source and result lifecycle. Operators inspect persisted models.dev source/override/final values and open the existing metadata editor in place from the export row. After saving reliable limits, the source is re-read and still-valid choices are preserved; cancelling or returning leaves the export context intact. No Pi binding or live catalog is needed for OpenCode rendering.
9. Source and render requests use `private, no-store`; a stale digest forces a source refresh, a bind/rebind/refresh/override write forces the same, and generated content is held only long enough to copy, download, or open the exact bytes in a new tab.

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
- `GET /api/models/exports/pi/source`
- `POST /api/models/exports/pi/render`
- `GET /api/models/exports/opencode/source`
- `POST /api/models/exports/opencode/render`
- `GET /api/models/{model_config_id}/pi`
- `POST /api/models/{model_config_id}/pi/bind`
- `POST /api/models/{model_config_id}/pi/search`
- `POST /api/models/{model_config_id}/pi/refresh/preview`
- `POST /api/models/{model_config_id}/pi/refresh/commit`
- `PUT /api/models/{model_config_id}/pi/override`
- `DELETE /api/models/{model_config_id}/pi/override`
- `DELETE /api/models/{model_config_id}/pi`

`POST /api/models/by-endpoints` is used by the Endpoints page to hydrate model references. `GET /api/models/by-endpoint/{endpoint_id}` and `POST /api/models/connections/batch` remain backend/API-client surfaces without a current production frontend caller.

## 5. Endpoints, Loadbalance Strategies, And Pricing Templates

**User entrypoints**

- `/route/endpoints`
- `/route/ban-policies`
- `/route/pricing`

**Frontend flow**

1. Services define reusable upstream credentials and base URLs that Terminal Targets can share. Saving and saving with verification are explicit separate actions; a saved service stays in a completion state with “配置可用模型”, which offers an existing model or a new model with this service preselected. A verification failure does not pretend the preceding save failed, and retrying verification does not recreate the service.
2. The Ban Policies page manages reusable strategies, default selection, attached-model impact, and a strategy preview. Its Routing Health action opens the separate current-state and events page; those views are not tabs inside strategy configuration.
3. Pricing templates define reusable cost models attached to Terminal Targets as typed, mutually-exclusive cards. Standard has one `standard` card; tiered has `tier_base` and `tier_above` plus a positive threshold; peak/valley has `peak` and `offpeak` plus at least one user-authored window and IANA timezone. A request chooses one complete card before FX and arithmetic; Prism never proportionally splits a request across a window boundary. Invalid/missing schedule evaluation is visibly unresolved and fail-closed, never silently priced with the valley card.
4. The price form shows the trusted current reporting currency and the price per million tokens. Input/output prices are required; optional cache/reasoning fields can remain empty and are serialized as explicit JSON `null`. All five component keys remain present in the wire payload, and explicit `"0"` alone means configured free pricing. Missing prices for components actually used by a request leave its cost unknown. Currency-read failure blocks saving and offers retry; failed saves retain the editable draft.
5. Pricing import starts with a selected JSON price file and an add-only or name-based update choice. The operator previews the proposed changes on the page, then explicitly confirms the import. File/read/preview failures preserve a recovery path; a blocked preview states that nothing was written and can be discarded. The versioned import format and preview/hash-commit protocol are unchanged. After creating a template, “为模型设置价格” leads to assigning it to a model service connection; creating a template alone does not price a model or recalculate history. Usage lookup and referenced-template deletion protection remain available.
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

## 6. Request Investigation

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
8. Saved content loads only on the dedicated “请求内容” page or after an explicit read in the two-request comparison panel; the list and detail sheet remain body-free. Both paths show readable conversation text and capture limitations without raw payload or header views. Returning from content inspection restores the prior list filters and selection.

**Backend touchpoints**

- `GET /api/stats/requests` (attempt view and `view=ingress_chains` chain view)
- `GET /api/stats/requests/export` (server-side full filtered CSV)
- `GET /api/stats/requests/{request_id}` (v2 detail: scoped statuses, failure projection, pricing layers)
- `GET /api/stats/cost-segments` and `/api/stats/cost-segments/{segment_key}/symbols`
- `GET /api/audit/logs`
- `GET /api/audit/logs/{log_id}`
- `GET /api/settings/costing` / `PUT /api/settings/costing` (timezone merged into the costing CAS)

All Requests/Audit list, detail, chain, and export responses send `Cache-Control: private, no-store` and carry profile-sensitive `Vary`.

See the [Requests workflow contract](requests.md) for page-specific queries and behavior.

## 7. Settings And Access Control

**User entrypoints**

- `/system/settings`
- `/system/proxy-keys`

**Frontend flow**

1. Settings groups user tasks as “显示与记录” and “访问与数据”, with sections such as “货币与时间”, “详细记录与内容”, and “登录与客户端访问”. Canonical URLs still use `scope=global|instance` and the existing section allowlist; the legacy `tab` value is dropped. The underlying global/instance ownership and API scopes are unchanged.
2. Reporting currency retains its single active epoch contract, while the UI shows the current currency and explains the effect of changing it without exposing the epoch. Once an epoch exists, a currency change still goes through migration preflight; historical snapshots remain read-only and existing request costs are not recalculated.
3. Timezone is part of the costing CAS (no standalone timezone route); preview uses the current instant and current IANA offset, and changing the preference only affects display and Custom input interpretation.
4. Destructive retention changes (enable `null -> N`, shorten `M -> N`, or one-time cleanup) always run through a fresh server preflight dialog with exact/estimated impact, owner coverage after, and keyword confirmation, then create a server-persisted job; queued manual jobs are cancellable from the job center, running manual purges are not.
5. Startup settings remain in the plaintext bootstrap file selected by `PRISM_CONFIG_PATH`; edit `config.json` directly and restart Prism to apply changes.
6. Saving a transition from disabled to enabled authentication broadcasts a localStorage auth-state update, refreshes local auth state, and redirects the current tab to `/auth/login`; other open tabs re-bootstrap from the broadcast. Enabling/disabling requires explicit acknowledgements (proxy-key readiness, permissive attribution when disabling, session invalidation).
7. “客户端密钥” manages global credentials on its own route. Keys can be created while authentication is disabled, but the page explicitly says they are not required for access then. Auth enablement retains the Proxy owner's one-instant readiness count and 30-second safe-active horizon; a zero-safe-key enable requires explicit acknowledgement. One-time secrets have copy/save acknowledgement and an explicit leave-without-saving confirmation; leaving does not delete the created key. Mutation errors stay with the form and preserve a recoverable draft.
8. “验证接入” combines model/key selection, one explicit test send, and its result in one dialog. It offers only enabled direct-entry models, preselects a model from `action=verify&model_id=...`, and defaults to no-key testing only when authentication is known to be disabled. Sending may incur service fees; record-only retry does not send another model request. The UI separates the received outcome, record lookup failure or absence, and cancellation; stopping the wait does not promise to cancel execution.
9. Verification exposes an exact-request link after the authoritative finalized request summary and its detail match the returned request identifier. Execution and cost come from that final summary, including when multiple attempts occurred or HTTP 200 ended incompletely. Receiving a response alone is shown separately from confirmed execution. A response-body interruption preserves any already received request identifier so its record can still be checked. Missing or unavailable records remain explicit and offer the selected model's recent requests with instructions to check the time; retrying a read never sends another model request.

Mail bootstrap fields remain parse-compatible for existing `config.json` files, but Prism no longer sends mail. Fresh bootstrap seeds use backend `8000`, frontend `5173`, and PostgreSQL `15432`, but `./start.sh` follows the existing bootstrap file's configured `server.port` when one already exists. `runtime.sideEffects.attemptTimeout` is seeded as `"10s"`; the `runtime.transport` section was removed outright, and outbound provider requests are no longer subject to any connection or timeout limits (a leftover `runtime.transport` block is rejected with a readable migration error). Direct external `config.json` edits are not watched automatically, and existing valid files are not rewritten by the launcher. To reset startup defaults, stop Prism, remove or relocate the bootstrap file, and restart.

OpenAI image routing is a separate dimension with its own rule. Operators set `openai_image_operations` on a model and `openai_image_capability` on each Terminal Target; an omitted target capability is derived from the owner model. Coverage is containment rather than equality, because generations and edits are additive capabilities on one protocol rather than mutually exclusive wire formats: a target serving both can back a model that only accepts one, and a narrower target is rejected with `422 target_openai_image_uncovered`. Image requests never consult the text dimension and text requests never consult the image dimension. Image usage tokens flow through the ordinary `PER_1M` pricing pipeline; the template has no separate slot for the text/image input split, so an image template's input price must be authored as a weighted rate.

OpenAI text routing is native-only and mode-strict. Operators set runtime support on each Terminal Target through `openai_text_capability`, using `responses_only`, `chat_completions_only`, or `dual_native`; each mode may connect only to the identical mode (3×3 equality matrix, diagonal only). The management UI filters target-model candidates and locks connection capabilities to the owner model's mode, and the backend rejects cross-mode authoring with `422 target_openai_mode_mismatch` and mode changes that break existing relations with `409`. Mode-incompatible Chat Completions/Responses attempts are skipped rather than translated. Startup runs the read-only persisted-relation scan after migration and upgrade work and refuses to serve when violations remain. Diagnostic operation-set coverage (`full|partial|none`) does not relax this authoring or runtime equality gate.

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

The Settings UI exposes fixed deletion presets of `1`, `7`, `30`, `90` days, or all data, runs a fresh manual-cleanup preflight with impact counts and owner actual coverage, then creates a durable sealed job. The retention job center (`scope=instance&section=retention-jobs`) holds a static snapshot that lists/filters durable v2 jobs without background polling — manual refresh and post-mutation calibration re-read the loaded pages serially with fresh cursors and swap atomically — opens progress history and cleanup results through two independently paginated reads, and cancels queued manual jobs (running manual purges return `409 purge_not_cancellable`; running automatic jobs accept `cancel_requested`). The audit & privacy card shows the three-state family policy plus the bounded storage summary (retained rows/bytes with exact/estimated/unavailable states, never zero defaults) and the fixed 4 MiB/body + 12+4 MiB ingress capture limits. Settings explanations preserve UTC retention boundaries, irreversible deletion, delayed physical space reclamation, possible interruption of active queries during manual cleanup, and missing/estimated evidence. Internal table names, revision/generation markers, and protocol states are not product explanations. Currency migration pages remain bounded and preserve pending/null evidence; the archive-only FX action records a ledger without changing the active currency epoch or template prices.

## 8. Runtime Proxy Traffic

Runtime auth follows the latest proxy-key snapshot immediately after auth and proxy-key management writes: rotated, disabled, or expired keys stop authorizing new supported `/v1` and `/v1beta` runtime operations. Create and rotate return a secret shown once in a copyable dialog; rotation replaces the secret on the same ledger row and records when and how many times it has rotated, while DELETE removes the row.

**User entrypoints**

- External clients calling one of the operation-registered runtime routes listed below

**Runtime flow**

1. Global CORS runs first. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the exact operation registry in that order. Once inside the registry, unsupported routes and wrong methods reject before body reads, provider transport, telemetry, audit, feedback, or runtime side effects.
2. Provider adapters parse native payloads, rewrite the selected upstream model identity, build upstream requests, classify responses/streams, and extract usage. OpenAI Chat Completions and Responses retain their native operation shapes; Prism does not translate between them.
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
`GET /v1/models` remains local and always returns the OpenAI-shaped list of enabled direct-entry OpenAI models. Enabled non-entry Model Target rows stay routable through a direct parent but are omitted. Query parameters, including the retired `client_version`, do not select an alternate response shape.

## 9. Priority Operations Runbook

Priority validation commands belong to the [Contributing Guide](../CONTRIBUTING.md). Lane pressure, admission responses, scheduler behavior, durable telemetry, best-effort feedback, and cache-generation failure modes are defined in [Priority enforcement](observability-and-retention.md#25-priority-enforcement-and-operator-facing-failure-modes). Use those owning contracts when diagnosing delayed observability or management overload; current runtime state and retained events are available through Routing Health.

## Cross-References

- [Product scope](product-contract.md#4-core-features)
- [API contracts](../ARCHITECTURE.md#api-contracts)
- [Request investigation details](requests.md)
- [Deployment and retained-data facts](../STATUS.md)

## Client Configuration Export Quick Guide

Open **路由配置 → 模型配置 → 导出客户端配置**. Pi remains the default; choose **OpenCode** for OpenCode **1.18.27**. Enter the Prism gateway origin and a provider key (`prism`, or `prism-` followed by a lowercase letter or digit and then lowercase letters, digits, `_` or `-`). Select the eligible text entries and inspect **查看最终值与来源** before generating `opencode-prism.json`.

OpenCode requires valid positive context and output limits. Missing or invalid limits block that model; open **修复元数据** in that row to bind or correct its saved values, then return to export after the source refreshes. Enter only limits supported by reliable model facts. Export never fetches a catalog or borrows Pi metadata. Prices are included only when every reachable target has the same complete standard USD price per million tokens. Tiered, peak/valley, missing or conflicting prices omit the whole cost group; OpenCode's possible zero estimate does not mean the model is free.

**不嵌入密钥** writes `env: ["PRISM_API_KEY"]` and omits `apiKey`. Set the environment variable in the terminal that launches OpenCode, then load the downloaded file:

```bash
export PRISM_API_KEY='replace-with-your-prism-proxy-key'
OPENCODE_CONFIG=/absolute/path/opencode-prism.json opencode
```

Alternatively, enter a final key in the export dialog; the downloaded file then carries it explicitly. In OpenCode select `prism/<complete Prism model ID>` (for example `prism/codex/gpt-x` when that exact entry exists). A custom provider key replaces only the initial `prism` segment. Chat Completions, Responses (including `dual_native`), Anthropic Messages and Gemini use model-specific SDKs and URLs inside the same provider.

To merge into an existing `opencode.json`, copy **provider 合并片段** into its singular `provider` object, preserving other provider keys. Pi's corresponding fragment belongs under plural `providers`. In Pi, repair an unready row's binding first, then explicitly select it once backend readiness is confirmed. `OPENCODE_CONFIG` participates in OpenCode's normal configuration merge; it does not isolate existing user/project settings. Use a distinct provider key and check that existing settings do not override its models, credentials or URLs. Prism never edits client files or sets a default model automatically.

The [OpenCode format example](../backend/internal/domain/modelexport/testdata/opencode_prism.golden.json) contains synthetic model metadata and no real key. Generate from your own Prism models for use. Gemini IDs that cannot survive its URL path grammar are explicitly unavailable for export; complete IDs containing `/` are retained for the body-based OpenAI and Anthropic protocols. Client inference defaults are not authored reasoning adaptations: custom variants, SDK options and interleaved reasoning are outside this export.

## Everyday Investigation and Maintenance

In model detail, open **采样时路由解释 → 采样 / 刷新** to inspect current published paths and exclusion reasons. The sample does not send a request and does not promise the next target. In **请求日志**, choose ingress duration or cost ordering, open the complete chain, and select two requests for metadata and explicitly loaded capture comparison. Cost groups remain separate; absent usage or price is never treated as free.

The Models page offers bounded batch limit overrides, strategy assignment and target price-template assignment. Select objects, review every difference, explicitly confirm manual replacements, and apply. A conflict requires a new preview and leaves the whole batch unchanged.

Save client export combinations or request views by name and use their JSON file import/export to move to another browser. Reconcile stale or instance-specific references before restoring. These files exclude credentials and request bodies; if browser storage fails, download the file to retain the preference.

On a phone, start with the request summary and enter the full investigation. Observe's **最近活动** lists retained finalized ingress requests. Automatic refresh is off by default; choose 30 or 60 seconds when needed. Background tabs pause, outstanding reads prevent overlapping refreshes, and each fragment shows its own sampling time and stale state.

## Personal Preferences and Read Lifecycles

The Observe feature owns a page-local read-cycle counter shared by its query contexts, summary, Now, chart, setup, activity, errors, current-state summary and previously loaded Terminal Target details. A whole-page refresh starts only after those reads settle; signed-context consumers reload from the newly issued context. The mounted interval is `0`, `30` or `60` seconds, defaults to `0`, pauses while the document is hidden and resets on route exit or auth epoch change. Resource AbortControllers combine with the existing management epoch signal. Same-basis refresh failures retain last-good responses with their original timestamps; scope/preset changes withdraw incompatible data. Stable error-category selection survives a refresh only when the new server projection still contains it, and its request-filter conjunction and signed context are updated together.

Personal preferences are frontend-owned; no server account, background sync or new database table is introduced. Export preset storage uses `prism.export-presets.v1`, with file envelope `{format:"prism.export-presets",version:1,presets}`. Preset fields are `name`, `client`, semantic `modelIds`, `gatewayOrigin` and `providerId`; clients remain independent and render still validates the current source digest. Request views retain `prism.request-logs.saved-views.v1`, with transfer envelope `{format:"prism.request-views",version:1,views}` and allowlisted canonical non-transient query state. Both collections allow at most 20 names of 1–80 characters; file reads are bounded to 256 KiB. Strict transfer validation rejects unknown fields, unsupported versions and query combinations that would lose active filters during normalization. References are reconciled using the existing current-instance model/endpoint/target/key/client owners and bounded currency-segment paging. Storage write failure retains an explicit tab-only copy; it is not a successful persistent save.

The Requests comparison feature reuses the existing request-detail and audit-list contracts for metadata and an explicit selected-audit-detail read for payloads. Ordinary list browsing does not fetch audit bodies. It retains the audit source/direction, capture byte ledger and completeness, then compares only readable conversation text extracted from supported retained formats, with a bounded positional display. Tool arguments, images, attachments, and raw protocol envelopes stay outside the comparison. It never invokes runtime execution or manufactures missing content.
