# Dashboard Contract

This reference covers mounted dashboard routes, session and read lifecycles, and the UI's management API dependencies. The [route tree](../frontend/src/app/router/appRouter.tsx) and [route normalization](../frontend/src/app/router/rewriteRoutes.ts) define paths and search parameters. The [shell navigation](../frontend/src/components/layout/app-layout/useShellNavigation.ts), [auth provider](../frontend/src/context/AuthContext.tsx), and [management client](../frontend/src/lib/api/request.ts) own the corresponding browser behavior. [Requests](requests.md) covers the request browser and investigation contract. See [operator procedures](operations.md) for client setup, maintenance, and recovery.

## Dashboard URLs

Dashboard routes are relative to the Prism origin. See [Operations](operations.md) for client setup and deployment URLs. The legacy `/models` and `/models/:id` paths redirect to `/route/models` and `/route/models/:id`; legacy Observe `overview`/`analytics` values open Trend, and `events` redirects to `/observe/routing-health`.

## Shared Scope Rules

- Public auth routes are `/auth/login`.
- Protected shell routes cover `/observe`, `/observe/routing-health`, `/observe/requests`, `/observe/requests/:requestId/audit`, `/route/models`, `/route/models/:id`, `/route/models/export`, `/route/endpoints`, `/route/ban-policies`, `/route/pricing`, `/system/settings`, and `/system/proxy-keys`; the canonical trend view is `/observe?tab=trend`.
- Profile-scoped management requests are pinned to Default profile id `1`. `X-Profile-Id` is still accepted for compatibility, but the backend ignores its value.
- Global management routes omit `X-Profile-Id` and include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Runtime proxy traffic has its own [operation and profile boundary](runtime-api.md).

## Sign In And Session Bootstrap

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

## Shell Bootstrap

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

## Dashboard And Statistics

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

## Model Management, Model Detail, And Client Export

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

## Endpoints, Loadbalance Strategies, And Pricing Templates

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

## Settings And Access Control

**User entrypoints**

- `/system/settings`
- `/system/proxy-keys`

**Frontend flow**

1. Settings groups user tasks as “显示与记录” and “访问与数据”, with sections such as “货币与时间”, “详细记录与内容”, and “登录与客户端访问”. Canonical URLs still use `scope=global|instance` and the existing section allowlist; the legacy `tab` value is dropped. The underlying global/instance ownership and API scopes are unchanged.
2. Reporting currency retains its single active epoch contract, while the UI shows the current currency and explains the effect of changing it without exposing the epoch. Once an epoch exists, a currency change still goes through migration preflight; historical snapshots remain read-only and existing request costs are not recalculated.
3. Timezone is part of the costing CAS (no standalone timezone route); preview uses the current instant and current IANA offset, and changing the preference only affects display and Custom input interpretation.
4. Destructive retention changes (enable `null -> N`, shorten `M -> N`, or one-time cleanup) always run through a fresh server preflight dialog with exact/estimated impact, owner coverage after, and keyword confirmation, then create a server-persisted job; queued manual jobs are cancellable from the job center, running manual purges are not.
5. Startup settings are maintained through the [bootstrap file](operations.md#startup-config-file).
6. Saving a transition from disabled to enabled authentication broadcasts a localStorage auth-state update, refreshes local auth state, and redirects the current tab to `/auth/login`; other open tabs re-bootstrap from the broadcast. Enabling/disabling requires explicit acknowledgements (proxy-key readiness, permissive attribution when disabling, session invalidation).
7. “客户端密钥” manages global credentials on its own route. Keys can be created while authentication is disabled, but the page explicitly says they are not required for access then. Auth enablement retains the Proxy owner's one-instant readiness count and 30-second safe-active horizon; a zero-safe-key enable requires explicit acknowledgement. One-time secrets have copy/save acknowledgement and an explicit leave-without-saving confirmation; leaving does not delete the created key. Mutation errors stay with the form and preserve a recoverable draft.
8. “验证接入” combines model/key selection, one explicit test send, and its result in one dialog. It offers only enabled direct-entry models, preselects a model from `action=verify&model_id=...`, and defaults to no-key testing only when authentication is known to be disabled. Sending may incur service fees; record-only retry does not send another model request. The UI separates the received outcome, record lookup failure or absence, and cancellation; stopping the wait does not promise to cancel execution.
9. Verification exposes an exact-request link after the authoritative finalized request summary and its detail match the returned request identifier. Execution and cost come from that final summary, including when multiple attempts occurred or HTTP 200 ended incompletely. Receiving a response alone is shown separately from confirmed execution. A response-body interruption preserves any already received request identifier so its record can still be checked. Missing or unavailable records remain explicit and offer the selected model's recent requests with instructions to check the time; retrying a read never sends another model request.

The authoring controls follow [native OpenAI text mode equality](runtime-api.md#openai-native-mode-equality-strict) and [independent image capability containment](runtime-api.md#openai-image-capability-independent-dimension-containment). Text-mode selectors filter Model Target candidates and lock Terminal Target capability to the owner's mode. An omitted image capability is derived from the owner model.

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

## Portable Personal Preferences

Personal preferences are frontend-owned; no server account, background sync or new database table is introduced. Export preset storage uses `prism.export-presets.v1`, with file envelope `{format:"prism.export-presets",version:1,presets}`. Preset fields are `name`, `client`, semantic `modelIds`, `gatewayOrigin` and `providerId`; clients remain independent and render still validates the current source digest. Request views retain `prism.request-logs.saved-views.v1`, with transfer envelope `{format:"prism.request-views",version:1,views}` and allowlisted canonical non-transient query state. Both collections allow at most 20 names of 1–80 characters; file reads are bounded to 256 KiB. Strict transfer validation rejects unknown fields, unsupported versions and query combinations that would lose active filters during normalization. References are reconciled using the existing current-instance model/endpoint/target/key/client owners and bounded currency-segment paging. Storage write failure retains an explicit tab-only copy; it is not a successful persistent save.

## Product Behavior

### Web UI (Management Dashboard)

- View model configurations and their reachable exits (the `/route/models` management surface for `model_configs`). The default view shows only direct entries; a URL-backed segmented selector switches to non-entry configurations (`仅模型目标`) or the full inventory. KPI/counts and search operate on the selected view. The list's 出口映射 column projects the direct `access_targets` rows in `(position, id)` order and shows the first two: Terminal Target rows as endpoint → persisted `upstream_model_id`, Model Target rows as the logical target id; rows beyond the second are a `还有 N 项，见详情` pointer to model detail, and the projection never follows Model Target rows recursively. Missing endpoint or upstream evidence renders a reasoned `—` and is never backfilled from the owning model configuration's `model_id`; disabled rows stay visible with a 未参与 state. The upstream identity is shown as-is, without a label saying whether it matches the client model name. Non-entry rows expose their incoming Model Target count and a warning when that count is zero.
- Add/edit/delete model configurations with ordered access targets. The form exposes the direct-entry switch; omission on create defaults to true and omission on update preserves the stored bit. The page's stats switch is a controlled single-select segmented control over the three named metric scopes (`ingress`/`final_execution`/`route_attempt`, URL-backed through `scope`) with a fixed attribution note per scope, and re-selecting the active scope never clears it. Connection filters (`flag`) cover `has_model_target` (at least one direct Model Target row), plus the retained `needs_target` and `single_truncated` states. Whether an upstream identity matches the client model name is not a filter; a retired `flag=upstream_decoupled` link falls back to the unfiltered list.
- Add/edit/delete profile-scoped endpoints
- Model detail renders one mixed access-target list ordered by the shared `position`; Model and Terminal rows share a continuous "位置 N" numbering, adjacent rows of either type can be moved up/down with the same controls, and reloads never restore type grouping
- Add/edit/delete Terminal Targets from model detail; the Terminal Target dialog includes request limits, custom headers, and structured “服务附加设置” fields for service-required values
- Toggle enabled/disabled access targets per model
- Select an explicit load-balance strategy with Ban Policy settings per model
- Dedicated model-detail route (`/route/models/:id`) for ordered access-target and Terminal Target configuration; global current loadbalance state and event history live at `/observe/routing-health`, while `/route/ban-policies` owns strategy configuration
- Dedicated request-log browsing and investigation at `/observe/requests`, separate from dashboard analytics
- Dedicated routes for pricing templates and proxy API key lifecycle management
- `/observe` is one dashboard with four content-switcher values sharing the page time preset: Trend (`trend`), Errors (`errors`), Activity (`activity`), and Terminal Targets (`terminal_targets`). The Now strip and fixed-ingress Window KPI grid render once above that switcher; routing health is a separate `/observe/routing-health` route. Legacy `overview`/`analytics` links resolve to Trend, while legacy `events` links redirect to routing health. Observe fragments load independently and never synthesize zeros on failure. The fixed-ingress Window KPI grid has requests, final completion rate, TTFT P95, output rate, cache-read share, and known cost; final completion counts completed finalized ingress requests, including stream outcomes, rather than only HTTP status; cache-read share distinguishes genuine zero, no comparable rows, empty window, failed read, and partial coverage. Output rate is backend-authoritative measured evidence: only completed, progressive requests with trusted visible text/tool-output delivery evidence (at least two output events across at least 50ms) enter the per-request equal-weight mean, while GLM-style buffered bursts, non-streaming responses, Images, failures, and evidence-insufficient requests are unmeasured and never pollute the average. The Window KPI exposes the measured/unmeasurable/not_applicable/unknown census, series buckets expose measured sample/request counts with honest null gaps, and request details expose the persisted per-request state/reason; missing rates render as `—` and genuine zeros remain 0. Trend and Errors share a URL-backed scope and expose only that scope's authoritative metric/group catalog: ingress uses `requests|errors|ttft|output_rate|tokens|cache_read_share|cost` (default `requests`), final execution uses `requests|errors|final_attempt_latency|tokens|cache_read_share|cost` (default `requests`), and route attempt uses `attempts|errors|attempt_latency` (default `attempts`). URL and wire metrics use the same names with no implicit aliases. Output rate and cache-read share draw as lines from the same usage-series read, preserve absent buckets as gaps, and carry sample/coverage evidence. Invalid combinations fail with typed `422`; no alias silently substitutes a different metric. Every scoped fragment carries its own caliber, bounds, dataset coverage and sample/missing evidence, and the UI/export keep request counts distinct from attempt counts.
- Model creation supports one-step composite create: an optional `initial_terminal_target` (existing or inline endpoint) is created atomically with the model and enables the model by default. Non-OpenAI wire payloads omit OpenAI-only keys, while OpenAI text/image dimensions are validated independently and cross-mode capability values reject with `422 target_openai_mode_mismatch` and zero writes. Terminal Targets can be batch-copied to same-profile, same-family, same-mode destination models in one transaction only when text capability matches exactly (including `null`) and image capability contains the destination requirement; both enabled and disabled copies validate before any write, and copied targets default to not participating in routing.
- Model detail exposes read-only static routing diagnostics (`GET /api/models/{id}/routing-diagnostics`) and the models list embeds the authoritative compact `routing_summary`; both share one backend analyzer and the runtime-owned model-bound operation catalog across OpenAI, Anthropic, and Gemini. The analyzer is separate from the runtime planner, reads no Ban/retry/admission state, and applies the routing strategy to the same single mixed peer sequence, so `single` truncates that whole list rather than each target type. Route witnesses carry the real Endpoint identity, lowercase coverage, and a routing-schedule qualifier. Routing mutations refresh diagnostics and the authoritative model list, rather than preserving browser-recomputed routing or connection counts.
- models.dev Catalog overrides expose all backend-owned metadata fields and preserve three states per field: missing, explicit `null` restore, and explicit value (including an empty string). Refresh replaces source facts only; manual overrides survive.
- Dashboard trends live under canonical `/observe?tab=trend`; the legacy `tab=analytics` value remains a compatibility alias for that same view
- The protected shell renders sidebar navigation and breadcrumbs from local route metadata.
- Settings uses canonical public URLs with **显示与记录** (`scope=global`) and **访问与数据** (`scope=instance`) groups and a section allowlist; the legacy `tab` query value is dropped during canonicalization. `scope=global` contains billing/reporting currency, timezone, audit/privacy, and config rules; `scope=instance` contains authentication and operator account, automatic retention policy with owner actual coverage, manual cleanup, and the retention job center.

### Personal Investigation and Maintenance

- The model-detail routing explanation is an explicit, read-only sample of the published runtime configuration and independently observed process-local state. It shows candidate paths and planner order, exclusions, schedules, missing observations and capacity evidence. Sampling never sends an upstream request, reserves capacity, advances live round-robin state or changes bans. A candidate is not a promise about the next request. Static diagnostics and manual runtime self-tests retain their separate meanings.
- Models support three bounded batch actions: models.dev/OpenCode context/output limit overrides, model routing-strategy assignment, and Terminal Target pricing-template assignment. Each batch contains 1–20 explicitly selected objects and one action. The operator reviews item-by-item before/after values and invalid items, confirms replacement of existing manual values, and applies one atomic transaction. Stale or invalid previews write nothing; conflicts retain inputs for a fresh preview. Successful results are read back from the database and related views/runtime references are refreshed. Pi metadata remains independent.
- Ingress ordering offers recorded elapsed duration and known cost in addition to time. Ranking, pagination, details and chain CSV retain the same filtered ingress cohort. Missing metrics sort last; the page reports full-cohort ranked/unranked counts and missing reasons. Monetary ordering stays within the same historical currency and currency epoch. Parent and attempt costs are not added, and a missing finalized record, usage/price evidence or known currency never becomes a zero metric.
- Requests can compare two retained request records across pages. Metadata loads independently; a chosen audit record loads only on explicit action, then its request or response direction can be compared. The existing audit-detail API returns both directions for that selected record. Comparison never sends a reproduction request or executes captured tool calls. It compares readable conversation text extracted from supported retained request/response formats; tool parameters, images, attachments, and raw protocol envelopes are excluded. Missing or truncated content and unsupported/binary captures remain explicit limitations. The displayed positional difference is bounded to 200 lines and 2,000 characters per line, with the display limit stated.
- Narrow screens show ingress summaries with result, model, measured duration and known cost, preserving the complete chain, attempts and audit entry points. Filters may be collapsed without changing their active values; the full desktop table remains available.
- Named export presets and request views use browser storage plus explicit versioned JSON file transfer. Restoring an export preset rechecks model identities against the current client source and the normal render gates. Imported numeric request filters and currency segments require explicit reconciliation with current-instance references, even if a numeric id happens to exist. Missing/renamed models, unsupported or damaged files, conflicting names and storage failures are explained; invalid filters never silently become all data. Files contain no keys, rendered configurations, captured bodies, signed query contexts, cursors or exact-request selection. Failed storage writes remain usable only in the current tab and may be downloaded for recovery.

