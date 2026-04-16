# Prism Workflows Reference

This document maps Prism's current operator workflows from mounted frontend routes to the backend APIs they drive. It is grounded in the live local stack started with `./start.sh full`, the running backend contract at `http://localhost:18000/openapi.json`, and the current route shell in `frontend/src/App.tsx`.

Validated again against a live local stack on 2026-04-09:
- `GET /health` returned `{"status":"ok","version":"0.2.19"}` and `/openapi.json` exposed 79 mounted paths.
- The rendered frontend confirmed `/dashboard`, `/models`, `/endpoints`, `/loadbalance-strategies`, `/pricing-templates`, `/request-logs`, `/settings`, `/proxy-api-keys`, and `/statistics` as live protected routes.
- A supplied OpenAI-compatible upstream passed Prism connection health checks and two non-streaming proxy requests (`/v1/chat/completions` and `/v1/responses`), and both requests appeared in Request Logs plus Dashboard activity.

## Evidence Sources

- Frontend route surface: `frontend/src/App.tsx`
- Shell navigation and route scoping: `frontend/src/components/layout/app-layout/navigationProfileConfig.ts`
- Auth bootstrap and session flow: `frontend/src/context/AuthContext.tsx`
- Selected-profile scoping: `frontend/src/context/ProfileContext.tsx`, `frontend/src/lib/api/core.ts`
- Backend router assembly: `backend/app/main.py`
- Live backend contract: `http://localhost:18000/openapi.json`
- Request-log details: `docs/REQUESTS_PAGE.md`

## Runtime URLs

- Frontend: `http://localhost:15173`
- Backend: `http://localhost:18000`
- Swagger UI: `http://localhost:18000/docs`
- Health: `http://localhost:18000/health`

## Shared Scope Rules

- Public auth routes are `/login`, `/forgot-password`, and `/reset-password`.
- Protected shell routes are `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/statistics`, `/settings`, `/proxy-api-keys`, `/pricing-templates`, and `/request-logs`.
- `selectedProfile` controls management scope through `X-Profile-Id` on profile-scoped `/api/*` requests.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` always uses the active profile, not the selected profile.
- Global management routes include `/api/auth/*`, `/api/profiles/*`, `/api/vendors/*`, `/api/settings/auth*`, and `/api/realtime/ws`.

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
- `POST /api/auth/webauthn/register/options`
- `POST /api/auth/webauthn/register/verify`
- `POST /api/auth/webauthn/authenticate/options`
- `POST /api/auth/webauthn/authenticate/verify`

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
- `/statistics`

**Frontend flow**

1. Dashboard bootstrap loads KPI cards, spending summaries, throughput, recent activity, and routing data.
2. The dashboard subscribes to realtime `dashboard.update` messages for live reconciliation.
3. Quick actions send operators into `/statistics` or `/request-logs` for deeper analysis.
4. The statistics route stays aggregate-focused and uses snapshot presets rather than request-level drill-down.

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
3. Proxy models manage ordered proxy targets instead of direct connections.
4. Model detail loads connection KPIs, current loadbalance state, loadbalance event history, and manual health-check actions.

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
3. Pricing templates define reusable cost models attached to connections.
4. These resources are profile-scoped and are usually managed before or alongside model-detail work.
5. The defaults action creates the canonical loadbalance strategy rows for the currently selected profile only.

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

## 6. Request Investigation

**User entrypoints**

- `/request-logs`
- Dashboard and model-detail deep links into `/request-logs`

**Frontend flow**

1. Operators browse request history with server-backed filters.
2. Exact request investigation opens the detail drawer through `request_id`.
3. `ingress_request_id` groups all upstream attempts for one incoming proxy request.
4. Audit payloads load lazily only when the audit tab is opened.

**Backend touchpoints**

- `GET /api/stats/requests`
- `GET /api/stats/requests/{request_id}`
- `GET /api/audit/logs`
- `GET /api/audit/logs/{log_id}`
- `GET /api/endpoints`
- `GET /api/models`
- `GET /api/settings/timezone`

For the page-specific query contract and UI behavior, see `docs/REQUESTS_PAGE.md`.

## 7. Settings And Access Control

**User entrypoints**

- `/settings`
- `/proxy-api-keys`

**Frontend flow**

1. Settings splits into a Profile tab and a Global tab.
2. Profile-scoped settings cover backup, reporting currency and FX mappings, timezone, audit/privacy defaults, and retention/deletion actions.
3. Global settings cover operator authentication and shared vendor management.
4. Proxy API keys are managed on their own route and stay global rather than profile-scoped.

**Backend touchpoints**

- `GET /api/settings/costing`
- `PUT /api/settings/costing`
- `GET /api/settings/timezone`
- `PUT /api/settings/timezone`
- `GET /api/config/profile/export`
- `POST /api/config/profile/import/preview`
- `POST /api/config/profile/import`
- `GET /api/config/header-blocklist-rules`
- `POST /api/config/header-blocklist-rules`
- `GET /api/config/user-agent-client-rules`
- `POST /api/config/user-agent-client-rules`
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
- `DELETE /api/stats/requests`
- `DELETE /api/stats/statistics`
- `DELETE /api/audit/logs`
- `DELETE /api/loadbalance/events`

## 8. Runtime Proxy Traffic

**User entrypoints**

- External clients calling Prism on `/v1/*` or `/v1beta/*`

**Runtime flow**

1. The incoming request resolves a model from the request body or Gemini path.
2. Proxy models choose an ordered native target before connection planning starts.
3. Native connection planning applies the attached loadbalance strategy and per-connection limits.
4. The upstream request is rewritten as needed for the target API family, then proxied through.
5. Request logs, audit data, and loadbalance events are recorded for later operator investigation.

**Backend touchpoints**

- `POST /v1/chat/completions`
- `POST /v1/messages`
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1beta/models/{model}:streamGenerateContent`

These routes are implemented in `backend/app/routers/proxy.py` and the `backend/app/routers/proxy_domains/` package, but they are intentionally excluded from the live OpenAPI document.

## Cross-References

- Product scope: `docs/PRD.md`
- API contracts: `docs/API_SPEC.md`
- Request investigation details: `docs/REQUESTS_PAGE.md`
