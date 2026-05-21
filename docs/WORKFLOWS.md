# Prism Workflows Reference

This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in the current frontend route shell in `frontend/src/App.tsx`, the live Go backend API surface, and the checked-in Go-served contract in `docs/openapi.json`.

Validated again against current repo surfaces on 2026-05-10:
- `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` are all `0.3.21`, which is the current backend/frontend version surface.
- `docs/openapi.json` is the management and health OpenAPI artifact served by the Go backend at `/openapi.json`.
- The protected frontend route shell in `frontend/src/App.tsx` mounts `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/pricing-templates`, `/request-logs`, `/settings`, `/proxy-api-keys`, and `/sidecars`; analytics lives under `/dashboard?tab=analytics`.

## Evidence Sources

- Frontend route surface: `frontend/src/App.tsx`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/navigationProfileConfig.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Selected-profile scoping: `frontend/src/context/ProfileContext.tsx`, `frontend/src/lib/api/core.ts`, `frontend/src/lib/api/profileScope.ts`
- Sidecar route and API surface: `frontend/src/pages/sidecars/`, `frontend/src/lib/api/sidecars.ts`, `backend/internal/httpapi/management/sidecars/`
- Backend router assembly: `backend/internal/httpapi/management/`, `backend/internal/httpapi/runtime/`, `backend/internal/httpapi/realtime/`, and `backend/internal/platform/http/server.go`
- Checked-in backend contract: `docs/openapi.json` (served at `/openapi.json` by the Go backend)
- Request-log details: `docs/REQUESTS_PAGE.md`

## Runtime URLs

- Frontend: `http://localhost:15173`
- Backend: `http://localhost:18000`
- Swagger UI: `http://localhost:18000/docs`
- Health: `http://localhost:18000/health`

## Shared Scope Rules

- Public auth routes are `/login`, `/forgot-password`, and `/reset-password`.
- Protected shell routes are `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/settings`, `/proxy-api-keys`, `/sidecars`, `/pricing-templates`, and `/request-logs`; analytics is a dashboard tab at `/dashboard?tab=analytics`.
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

1. Dashboard bootstrap loads KPI cards, spending summaries, throughput, recent activity, and routing data.
2. The dashboard subscribes to realtime `dashboard.update` messages for live reconciliation.
3. Quick actions send operators into the analytics tab or `/request-logs` for deeper analysis.
4. The analytics tab stays aggregate-focused and uses snapshot presets rather than request-level drill-down.

**Backend touchpoints**

- `GET /api/stats/summary`
- `GET /api/stats/spending`
- `GET /api/stats/throughput`
- `GET /api/stats/requests?limit=12`
- `GET /api/stats/usage-snapshot`
- `WS /api/realtime/ws`

## 4. Model Management And Model Detail

**User entrypoints**

- `/models`
- `/models/:id`
- `/models/:id/proxy`

**Frontend flow**

1. Operators list, search, create, edit, and delete model configs.
2. Native models manage connections, pricing-template assignment, and attached loadbalance strategy.
3. Proxy models manage `proxy_selection_strategy` and explicit proxy target metadata instead of direct connections, and both `/models` and `/models/:id/proxy` use the same non-empty same-family native-target contract.
4. The `/models/:id/proxy` route keeps the page card summary-only; the shared Model Settings dialog is the one authoritative proxy-target editor and save path.
5. Model detail loads connection KPIs, current loadbalance state, loadbalance event history, and manual health-check actions.

**Backend touchpoints**

- `GET /api/models`
- `POST /api/models`
- `GET /api/models/{model_config_id}`
- `PUT /api/models/{model_config_id}`
- `DELETE /api/models/{model_config_id}`
- `GET /api/models/{model_config_id}/connections`
- `POST /api/models/{model_config_id}/connections`
- `POST /api/models/connections/batch`
- `GET /api/loadbalance/current-state`
- `GET /api/loadbalance/events`
- `POST /api/connections/{connection_id}/health-check`
- `POST /api/models/{model_config_id}/connections/health-check-preview`

## 5. Endpoints, Loadbalance Strategies, And Pricing Templates

**User entrypoints**

- `/endpoints`
- `/loadbalance-strategies`
- `/pricing-templates`

**Frontend flow**

1. Endpoints define reusable upstream credentials and base URLs.
2. Loadbalance strategies define reusable `legacy` or `adaptive` routing policies for native models.
3. Pricing templates define reusable cost models attached to connections. Required input and output prices must be explicit, while nullable optional cache-read, cache-creation, and reasoning prices mean default zero/effective zero in phase 1.
4. Pricing-template management displays nullable optional prices as `0 (default)`. Request logs and cost math surfaces display the same effective value as plain `0`.
5. These resources are profile-scoped and are usually managed before or alongside model-detail work.
6. The defaults action creates the canonical loadbalance strategy rows for the currently selected profile only.

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
3. The page can test management auth, trigger manual sync, inspect auth-file and provider snapshots, open read-only auth-file model discovery, patch auth-file status or priority, and delete one confirmed auth file through Prism's backend.
4. Auth-file model discovery, status/priority mutations, and single-authfile delete flow through Prism backend routes; the browser never calls CLIProxyAPI directly.
5. Auth mutation/delete responses can report `succeeded` or `succeeded_sync_failed`; the frontend should preserve the last known detail view and surface the returned `sync_status` / `sync_error` instead of treating the refresh as fresh truth.
6. Sidecar snapshot sync runs as a low-priority bounded scheduler job.

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
- `GET /api/sidecars/{sidecar_id}/auth-snapshots`
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
4. For proxy traffic, the request-log UI keeps the requested proxy `model_id` separate from the resolved native `resolved_target_model_id` so operators can see authoring intent and execution target at the same time.
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
2. Profile-scoped settings cover backup, reporting currency and FX mappings, timezone, audit/privacy defaults, and retention/deletion actions. Rows with missing FX data remain pricing failures and are separate from optional pricing-template components that default to zero.
3. Global settings cover operator authentication and shared vendor management.
4. The Startup tab owns plaintext bootstrap config management under `/settings#startup`.
5. Proxy API keys are managed on their own route and stay global rather than profile-scoped.

Auth email delivery for password reset and recovery-email verification is transport-backed only when startup config has `mail.enabled=true`. Missing `mail` and `mail.enabled=false` keep the backward-compatible no-op delivery path, so existing deployments keep starting without SMTP and do not dial SMTP. Enabled SMTP is strict: invalid host, port, mode, timeout, credential, or plaintext rules fail validation or startup instead of silently using no-op delivery.

The Startup tab treats `mail.smtp.password` as a secret field. Safe bootstrap payloads show metadata only, and operators should either preserve or replace that secret through the bootstrap update flow or point `mail.smtp.passwordFile` at a local secret file. SMTP transport changes apply immediately when saved through the Startup tab or API PUT and hot publish succeeds. Raw `runtime.sideEffects.attemptTimeout` sets the per-attempt background side-effect enqueue budget, defaults to `"10s"` in newly seeded configs, and is restart-required rather than hot-applied. Direct external `config.json` edits are not watched automatically. To roll back delivery, remove `mail` or set `mail.enabled=false` through the Startup tab or API PUT.

The configuration-operations flow is explicit in both lanes:
- profile export defaults to the safe redacted bundle at `GET /api/config/profile/export`
- secret-bearing profile export uses `POST /api/config/profile/export/with-secrets` with `X-Prism-Dangerous-Confirm: profile-export`
- profile import uses upload, preview, then apply with `X-Prism-Preview-Token`
- profile import replaces profile-scoped rows only, while global vendor rows, other profiles, and request logs remain untouched
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

Runtime auth follows the latest proxy-key snapshot immediately after auth and proxy-key management writes: rotated, retired, or expired keys stop authorizing new `/v1/*` and `/v1beta/*` requests, while the management UI keeps their historical rows visible.


**User entrypoints**

- External clients calling Prism on `/v1/*` or `/v1beta/*`

**Runtime flow**

1. The incoming request resolves a model from the request body or Gemini path.
2. Proxy models choose a native target through `ordered_fallback`, `weighted_static`, or `priority_static` before connection planning starts.
3. Native connection planning applies the attached loadbalance strategy and per-connection limits.
4. The upstream request is rewritten as needed for the target API family, then proxied through.
5. Request logs, audit data, and loadbalance events are recorded for later operator investigation.
6. Missing pricing stays visibly degraded or unpriced, it never silently looks complete.

**Backend touchpoints**

- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/messages`
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1beta/models/{model}:streamGenerateContent`

These routes are implemented in `backend/internal/httpapi/runtime/runtime.go` plus the related helpers under `backend/internal/httpapi/runtime/`, and they are intentionally excluded from the management-only OpenAPI document.

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
- Audit or stat lag: raw audit reads remain bounded by time window and keyset cursor, dashboard stats come from materialized rollups, and broad deletes run as durable management jobs. Freshness lag is visible; Prism does not hide it with unbounded live aggregation.
- Cache generation lag: management mutations advance durable runtime-cache generations before commit. Cache warming may lag, but runtime reads compare generation vectors and refresh or fail closed for stale, missing, or unverifiable auth-sensitive snapshots.

## Cross-References

- Product scope: `docs/PRD.md`
- API contracts: `docs/API_SPEC.md`
- Sidecar implementation boundary: `backend/internal/httpapi/management/sidecars/AGENTS.md`, `frontend/src/pages/sidecars/AGENTS.md`
- Request investigation details: `docs/REQUESTS_PAGE.md`
