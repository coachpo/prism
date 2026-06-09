# Prism Workflows Reference

This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in the current frontend route shell in `frontend/src/App.tsx`, the live Go backend API surface, and the markdown API reference.

Validated again against current repo surfaces on 2026-06-05:
- `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` are all `0.4.0`, which is the current backend/frontend version surface.
- The protected frontend route shell in `frontend/src/App.tsx` mounts `/dashboard`, `/models`, `/models/:id`, `/endpoints`, `/loadbalance-strategies`, `/settings`, `/proxy-api-keys`, `/sidecars`, `/pricing-templates`, and `/request-logs`; analytics lives under `/dashboard?tab=analytics`.

## Evidence Sources

- Frontend route surface: `frontend/src/App.tsx`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/navigationProfileConfig.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Selected-profile scoping: `frontend/src/context/ProfileContext.tsx`, `frontend/src/lib/api/core.ts`, `frontend/src/lib/api/profileScope.ts`
- Sidecar route and API surface: `frontend/src/pages/sidecars/`, `frontend/src/lib/api/sidecars.ts`, `backend/internal/httpapi/management/sidecars/`
- Backend router assembly: `backend/internal/httpapi/management/`, `backend/internal/httpapi/runtime/`, `backend/internal/httpapi/realtime/`, and `backend/internal/platform/http/server.go`
- Backend API reference: `docs/API_SPEC.md`
- Request-log details: `docs/REQUESTS_PAGE.md`

## Runtime URLs

- Frontend: `http://localhost:5173`
- Backend: `http://localhost:18000` with the checked-in `config.json`
- Health: `http://localhost:18000/health` with the checked-in `config.json`

## Shared Scope Rules

- Public auth routes are `/login`, `/forgot-password`, and `/reset-password`.
- Protected shell routes are `/dashboard`, `/models`, `/models/:id`, `/endpoints`, `/loadbalance-strategies`, `/settings`, `/proxy-api-keys`, `/sidecars`, `/pricing-templates`, and `/request-logs`; analytics is a dashboard tab at `/dashboard?tab=analytics`.
- `selectedProfile` controls profile-scoped management requests through `X-Profile-Id`.
- Global management routes omit `X-Profile-Id` and include `/api/auth/*`, `/api/profiles/*`, `/api/vendors/*`, `/api/settings/auth*`, `/api/config/vendors/*`, `/api/sidecars/*`, and `/api/realtime/ws`.
- `POST /api/config/profile/import/preview` is profile-scoped and requires `X-Profile-Id`.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` always uses the active profile, not the selected profile.

## 1. Sign In And Session Bootstrap

**User entrypoints**

- `/login`
- `/forgot-password`
- `/reset-password`

**Frontend flow**

1. `AuthProvider` chooses public bootstrap mode for auth-only routes.
2. The login page loads auth state before showing the form.
3. Successful login redirects into the protected shell, usually `/dashboard`.
4. Passive and proactive refresh keep the operator session alive while the tab stays active.

**Backend touchpoints**

- `GET /api/auth/public-bootstrap`
- `GET /api/auth/status`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `POST /api/auth/refresh`
- `GET /api/auth/session`
- `POST /api/auth/password-reset/request`
- `POST /api/auth/password-reset/confirm`

## 2. Shell Bootstrap And Profile Selection

**User entrypoints**

- Any protected route
- Header profile switcher in the shell

**Frontend flow**

1. `AuthProvider` confirms the operator session.
2. `ProfileProvider` loads the profile list, active profile, and profile cap from one bootstrap call.
3. The shell renders sidebar groups, breadcrumbs, language/theme controls, and the version label.
4. Changing the selected profile updates `setApiProfileId()` and refreshes profile-scoped management data.
5. Activating a profile is an explicit runtime action, distinct from selecting one in the shell.

**Backend touchpoints**

- `GET /api/auth/status`
- `GET /api/profiles/bootstrap`
- `GET /api/profiles/active`
- `POST /api/profiles/{profile_id}/activate`
- Profile-scoped `/api/*` routes with `X-Profile-Id`

## 3. Dashboard And Statistics

**User entrypoints**

- `/dashboard`
- `/dashboard?tab=analytics`

**Frontend flow**

1. Dashboard overview bootstrap loads KPI cards, spending summaries, throughput, recent activity, and routing data from the canonical aggregate snapshot.
2. The dashboard subscribes to realtime `dashboard.update` messages for live reconciliation against that same overview snapshot shape.
3. Quick actions send operators into the analytics tab or `/request-logs` for deeper analysis.
4. The analytics tab stays aggregate-focused and uses its own snapshot presets rather than request-level drill-down.

**Backend touchpoints**

- `GET /api/stats/dashboard` for the overview aggregate snapshot, including backend-computed Routing Health Map data
- `GET /api/stats/usage-snapshot` for the analytics tab snapshot presets
- `WS /api/realtime/ws` for overview `dashboard.update` reconciliation

## 4. Model Management And Model Detail

**User entrypoints**

- `/models`
- `/models/:id`

**Frontend flow**

1. Operators list, search, create, edit, and delete model configs.
2. Public model create and edit flows author ordered targets that point only to same-family models.
3. Model detail is the Terminal Target management surface for the model's owned endpoint bindings.
4. Model detail loads owned Terminal Target KPIs, current Ban Policy retry-window state, loadbalance event history, and manual health-check actions.
5. Request-log handoff preserves the requested model while final-target fields show the terminal model reached through the access graph.

**Backend touchpoints**

- `GET /api/models`
- `POST /api/models`
- `GET /api/models/{model_config_id}`
- `PUT /api/models/{model_config_id}`
- `DELETE /api/models/{model_config_id}`
- `GET /api/models/{model_config_id}/targets`
- `POST /api/models/{model_config_id}/targets`
- `PUT /api/models/{model_config_id}/targets/{target_id}`
- `PATCH /api/models/{model_config_id}/targets/{target_id}`
- `PATCH /api/models/{model_config_id}/targets/{target_id}/position`
- `DELETE /api/models/{model_config_id}/targets/{target_id}`
- `GET /api/loadbalance/current-state`
- `GET /api/loadbalance/events`
- `GET /api/models/{model_config_id}/connections`
- `POST /api/models/{model_config_id}/connections`
- `PATCH /api/models/{model_config_id}/connections/{connection_id}`
- `DELETE /api/models/{model_config_id}/connections/{connection_id}`
- `POST /api/models/{model_config_id}/connections/{connection_id}/health`

## 5. Endpoints, Loadbalance Strategies, And Pricing Templates

**User entrypoints**

- `/endpoints`
- `/loadbalance-strategies`
- `/pricing-templates`

**Frontend flow**

1. Endpoints define reusable upstream credentials and base URLs that Terminal Targets can share.
2. Loadbalance strategies define reusable routing plus explicit Ban Policy retry-window settings for model access.
3. Pricing templates define reusable cost models attached to Terminal Targets with five concrete pricing strings: `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, and `reasoning_price`.
4. Pricing-template management saves explicit strings for every component. Missing/null/blank inputs normalize to `"0"`; explicit `"0"` is configured free pricing, not missing pricing data.
5. Request logs and cost math consume canonical disjoint token components: base input, cache-read input, cache-creation input, base output, and reasoning output. Aggregate `cached_tokens` is derived-only for presentation.
6. These resources are profile-scoped and are usually managed before or alongside model-detail work.
7. The defaults action creates the canonical loadbalance strategy rows for the currently selected profile only.

**Backend touchpoints**

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

The loadbalance strategy routes continue to use selected-profile scope through `X-Profile-Id`, and the defaults action is a no-body POST that returns the created/current canonical rows plus creation metadata.
- `GET /api/pricing-templates`
- `POST /api/pricing-templates`
- `GET /api/pricing-templates/{template_id}`
- `PUT /api/pricing-templates/{template_id}`
- `DELETE /api/pricing-templates/{template_id}`

## 5A. CLIProxyAPI Sidecars

**User entrypoints**

- `/sidecars`

**Frontend flow**

1. The sidecars page is global instance control-plane UI, not selected-profile configuration.
2. Operators register CLIProxyAPI sidecar base URLs, management passwords, sync intervals, request timeout, and network policy flags.
3. The page can test management auth, trigger manual provider sync, inspect live auth-files and provider inventory, open read-only auth-file model discovery, patch auth-file status or priority, and delete one confirmed auth file through Prism's backend.
4. Auth-file reads, model discovery, status/priority mutations, and single-authfile delete flow through Prism backend routes; the browser never calls CLIProxyAPI directly.
5. Auth mutation/delete responses can report `succeeded` or `succeeded_sync_failed`; the frontend should refetch live `/auth-files` after mutation attempts and surface returned `sync_status` / `sync_error` when refresh fails.
6. Sidecar provider sync runs as a low-priority bounded scheduler job.

**Backend touchpoints**

- `GET /api/sidecars`
- `POST /api/sidecars`
- `GET /api/sidecars/{sidecar_id}`
- `PATCH /api/sidecars/{sidecar_id}`
- `DELETE /api/sidecars/{sidecar_id}`
- `POST /api/sidecars/{sidecar_id}/test-connection`
- `POST /api/sidecars/{sidecar_id}/sync`
- `GET /api/sidecars/{sidecar_id}/auth-files`
- `GET /api/sidecars/{sidecar_id}/auth-files/models?name=...`
- `DELETE /api/sidecars/{sidecar_id}/auth-files/{auth_id}`
- `PATCH /api/sidecars/{sidecar_id}/auth-files/{auth_id}/status`
- `PATCH /api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields`
- `GET /api/sidecars/{sidecar_id}/provider-snapshots`
- `GET /api/sidecars/{sidecar_id}/providers`
- `GET /api/sidecars/{sidecar_id}/sync-status`

## 6. Request Investigation

**User entrypoints**

- `/request-logs`
- Dashboard and model-detail deep links into `/request-logs`

**Frontend flow**

1. Operators browse request history with server-backed filters.
2. Exact request investigation opens the detail drawer through `request_id`.
3. `ingress_request_id` groups all upstream attempts for one incoming proxy request.
4. The request-log UI keeps the requested `model_id` separate from the final `resolved_target_model_id` so operators can see authoring intent and execution target at the same time.
5. Audit payloads load lazily only when the audit tab is opened.

**Backend touchpoints**

- `GET /api/stats/requests`
- `GET /api/stats/requests/{request_id}`
- `GET /api/audit/logs`
- `GET /api/audit/logs/{log_id}`
- `GET /api/models`
- `GET /api/settings/timezone`

For the page-specific query contract and UI behavior, see `docs/REQUESTS_PAGE.md`.

## 7. Settings And Access Control

**User entrypoints**

- `/settings`
- `/proxy-api-keys`

**Frontend flow**

1. Settings splits into Profile, Global, and Startup tabs.
2. Profile-scoped settings cover backup, reporting currency and FX mappings, timezone, audit/privacy defaults, and retention/deletion actions. Rows with missing FX data remain pricing failures; explicit `"0"` component prices are configured free pricing and do not become `MISSING_PRICE_DATA`.
3. Global settings cover operator authentication and shared vendor management.
4. The Startup tab edits the plaintext bootstrap file under `/settings#startup`, but backend-provided values and backend-owned canonical defaults remain the source of truth.
5. Proxy API keys are managed on their own route and stay global rather than profile-scoped.

Auth email delivery for password reset and recovery-email verification is transport-backed only when startup config has `mail.enabled=true`. Missing `mail` and `mail.enabled=false` mean disabled no-op delivery, so Prism starts without SMTP and does not dial SMTP. Enabled SMTP is strict: invalid host, port, mode, timeout, credential, or plaintext rules fail validation or startup instead of silently using no-op delivery.

The Startup tab treats `mail.smtp.password` as a secret field. Safe bootstrap payloads show metadata only, and operators should either preserve or replace that secret through the bootstrap update flow or point `mail.smtp.passwordFile` at a local secret file. SMTP transport changes apply immediately when saved through the Startup tab or API PUT and hot publish succeeds. Fresh bootstrap seeds use backend `8000`, frontend `5173`, and PostgreSQL `15432`, but `./start.sh` follows the existing bootstrap file's configured `server.port` when one already exists. `runtime.transport.requestTimeout` is seeded as `"300s"`, and `runtime.sideEffects.attemptTimeout` is seeded as `"10s"`. Request timeout is hot-applicable for future provider requests, while side-effects attempt timeout is restart-required. Direct external `config.json` edits are not watched automatically, and existing valid files are not rewritten by the launcher. To reset startup defaults, stop Prism, remove or relocate the bootstrap file, and restart. To roll back delivery, remove `mail` or set `mail.enabled=false` through the Startup tab or API PUT.

The remaining routing startup control stays on that same startup-config lane. `runtime.routing.openaiTerminalTranslationMode` is restart-required and supports `off` and `safe_only`. Profile bundle export/import remains on `version: 3` and does not carry that bootstrap-owned control.

The configuration-operations flow is explicit in both lanes:
- profile export defaults to the safe redacted bundle at `GET /api/config/profile/export`
- secret-bearing profile export uses `POST /api/config/profile/export/with-secrets` with `X-Prism-Dangerous-Confirm: profile-export`
- profile import uses upload, preview, then apply with `X-Prism-Preview-Token`
- profile import replaces profile-scoped rows only, while global vendor rows, other profiles, and request logs remain untouched
- profile import rejects `connection_ref` values used by multiple models because imported connections are model-private endpoint bindings
- vendor catalog import mutates only the shared vendor catalog and leaves profile-scoped rows untouched
- apply stays header-bound, and the raw bundle JSON is not rewritten in transit

**Backend touchpoints**

- `GET /api/settings/costing`
- `PUT /api/settings/costing`
- `GET /api/settings/timezone`
- `PUT /api/settings/timezone`
- `GET /api/config/profile/export`
- `POST /api/config/profile/export/with-secrets`
- `POST /api/config/profile/import/preview`
- `POST /api/config/profile/import`
- `GET /api/config/vendors/export`
- `POST /api/config/vendors/import/preview`
- `POST /api/config/vendors/import`
- `GET /api/config/header-blocklist-rules`
- `PATCH /api/config/header-blocklist-rules/{rule_id}`
- `DELETE /api/config/header-blocklist-rules/{rule_id}`
- `POST /api/config/header-blocklist-rules`
- `GET /api/config/user-agent-client-rules`
- `POST /api/config/user-agent-client-rules`
- `PATCH /api/config/user-agent-client-rules/{rule_id}`
- `DELETE /api/config/user-agent-client-rules/{rule_id}`
- `GET /api/settings/auth`
- `PUT /api/settings/auth`
- `POST /api/settings/auth/email-verification/request`
- `POST /api/settings/auth/email-verification/confirm`
- `GET /api/settings/auth/proxy-keys`
- `POST /api/settings/auth/proxy-keys`
- `PATCH /api/settings/auth/proxy-keys/{key_id}`
- `POST /api/settings/auth/proxy-keys/{key_id}/rotate`
- `DELETE /api/settings/auth/proxy-keys/{key_id}`
- `GET /api/vendors`
- `POST /api/vendors`
- `PATCH /api/vendors/{vendor_id}`
- `DELETE /api/vendors/{vendor_id}`
- `GET /api/settings/log-retention`
- `PUT /api/settings/log-retention`
- `POST /api/maintenance/log-retention/jobs`

Global log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.

Profile export and import stay selected-profile scoped. `POST /api/config/profile/import/preview` is a profile-scoped config readiness route and requires `X-Profile-Id`.

## 8. Runtime Proxy Traffic

Runtime auth follows the latest proxy-key snapshot immediately after auth and proxy-key management writes: rotated, retired, or expired keys stop authorizing new supported `/v1` and `/v1beta` runtime operations, while the management UI keeps their historical rows visible.

**User entrypoints**

- External clients calling one of the operation-registered runtime routes listed below

**Runtime flow**

1. The operation registry resolves an exact `POST` route before provider transport, telemetry, audit, feedback, or side effects.
2. Provider adapters parse provider-specific payloads, build upstream requests, adapt responses, classify streams, and extract usage.
3. Models resolve ordered access targets through same-family model links until a terminal private connection is reached.
4. Connection planning applies the attached explicit Ban Policy strategy and per-connection limits.
5. The shared runtime/gateway owns admission, routing, accounting, telemetry, audit persistence, pricing, feedback, and side-effect handoff.
6. After the first downstream byte or event on a stream, no retry, redirect, context-overflow fallback, or hedge replay can start.
7. Missing pricing stays visibly degraded or unpriced, it never silently looks complete.

**Backend touchpoints**

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

These 11 `POST` routes are allowlisted in `backend/internal/httpapi/runtime/operations.go` and are intentionally separate from `/api/*` management routes. Prism does not treat `/v1` or `/v1beta` as catch-all prefixes.

## 9. Priority Operations Runbook

Before shipping priority-sensitive backend changes, run the standard priority regression tests from the backend tree:

```bash
cd backend && go test ./tests/priority/...
```

The expected pass signal is exit code `0`. Failures should be treated as regressions in the priority classification, admission, scheduler, or lane-isolation behavior covered by the checked-in backend suite.

Operational triage by symptom:

- Lane budget pressure: identify the labeled DB lane first. `runtime_execution` protects proxy work; `management`, `realtime`, `cache_refresh`, `runtime_telemetry`, `runtime_feedback`, and `background_jobs` have separate budgets and should be remediated at their owning workload instead of increasing unrelated pools.
- Overload or `Retry-After`: honor the retry delay and reduce client concurrency. M3 reporting and maintenance routes are expected to shed before M2/M1 management work, and management/background pressure should not affect proxy execution capacity.
- Scheduler lag: expect delayed, coalesced, retried, or dropped background work according to worker policy. Do not add ad hoc goroutines or timers; register new recurring, retrying, or delayed work with the scheduler.
- Outbox failures: inspect the relevant durable store state. Email outbox retries or dead-letters delivery without leaking OTPs or SMTP credentials; management side-effect outbox rows retry or become permanent failures without rolling back committed primary state.
- Runtime telemetry loss: accepted runtime activity intents should drain to the telemetry outbox unless terminal validation or forced shutdown prevents completion. Treat lost accepted telemetry as a durability incident.
- Runtime feedback loss: feedback is best effort and may drop on queue full, invalid event, closed pipeline, or store failure. Drops should be accounted for, but they must not delay or fail proxy responses.
- Audit or stat lag: raw audit reads remain bounded by time window and keyset cursor. Dashboard overview reads come from the canonical `/api/stats/dashboard` aggregate snapshot, including backend-computed Routing Health Map data, and broad deletes run as durable management jobs.
- Cache generation lag: management mutations advance durable runtime-cache generations before commit. Cache warming may lag, but runtime reads compare generation vectors and refresh or fail closed for stale, missing, or unverifiable auth-sensitive snapshots.

## Cross-References

- Product scope: `docs/PRD.md`
- API contracts: `docs/API_SPEC.md`
- Sidecar implementation boundary: `backend/internal/httpapi/management/sidecars/AGENTS.md`, `frontend/src/pages/sidecars/AGENTS.md`
- Request investigation details: `docs/REQUESTS_PAGE.md`
