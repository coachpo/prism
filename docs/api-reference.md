# Shared HTTP Reference

This reference covers profile resolution, the management request boundary, health, and error envelopes. See [Runtime API](runtime-api.md), [Management API](management-api.md), and [Observability API](observability-api.md) for resource and operation contracts.

Local `./start.sh` backend base URL follows the selected bootstrap file's `server.port`; fresh repo-local bootstrap seeds use `http://localhost:8000`.

Container and custom deployments use the listener configured in the plaintext bootstrap file. The root single-image Docker bundle publishes Nginx on `http://localhost:8080` by default and proxies to the private backend upstream on port `8000`.

Prism does not expose a backend-local `/metrics` operations endpoint or start telemetry exporters. The startup `telemetry` block is parsed for existing `config.json` compatibility only. The retained `/api/stats/*` routes remain product-facing request-history and aggregate APIs.

## 0. Profile Context Semantics

- Prism has three route classes:
  - Global management routes, which omit `X-Profile-Id`.
  - Profile-scoped management routes, which accept `X-Profile-Id` but ignore its value and resolve against Default profile id `1`.
  - Runtime proxy routes, which resolve against frozen Default profile id `1` and ignore management scope overrides.
- Proxy endpoints (`/v1/*`, `/v1beta/*`) always resolve against frozen Default profile id `1` and ignore management scope overrides.
- Global management routes include `/api/auth/*`, `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Management job routes `/api/management/jobs*` are low-priority management routes: list resolves Default-profile jobs, while read and cancel resolve Default-profile jobs and can fall back to global log-retention jobs by ID. The frontend does not treat them as `X-Profile-Id`-scoped routes.
- Profile-scoped management routes include `/api/config/header-blocklist-rules*`, `/api/config/user-agent-client-rules*`, `/api/settings/costing` (including the timezone preference CAS), `/api/settings/audit`, `/api/stats/*`, `/api/audit/*`, `/api/loadbalance/*`, `/api/models/*`, `/api/endpoints/*`, `/api/pricing-templates*`, and `/api/connections/*`.
- Detail endpoints return `404` when a resource exists outside Default profile id `1`.
- Scope-control failures return structured JSON with `code` and `detail`, where `code` is stable for machine handling and `detail` is safe to show to operators.

## 1.0 Request Boundary

Global CORS handling runs before both management and runtime branches and answers qualifying preflight requests with `204` before route middleware runs.

The mounted request path applies the management browser-write guard, runtime-cache invalidation handling, management admission, request-body limits, management-session authentication, and then the mounted management router. For non-GET/HEAD/OPTIONS `/api/*` requests, a cross-origin `Origin` (neither same-origin with the request host nor in the CORS allowlist) returns `403 management_cross_origin_write_blocked`; a body-bearing write whose `Content-Type` is not `application/json` returns `415 management_unsupported_media_type`. The guard is independent of operator auth and applies when auth is off.

`POST`, `PUT`, and `PATCH` requests under `/api/auth/*` are limited to `64 KiB`; other mutating management requests are limited to `1 MiB`. The limit wrapper observes reads from the request body. If a downstream handler reads past the limit, Prism replaces its response with `413`:

```json
{
  "error": "request_body_too_large",
  "message": "Request body exceeds the maximum allowed size.",
  "limit_bytes": 65536
}
```

The wrapper is installed before management authentication, but authentication or admission can reject a request before any body read. For example, an oversized unauthenticated request to a protected management route can return its normal `401` authentication response rather than `413`.

When operator auth is enabled, the exact public management paths are `GET /api/auth/status`, `GET /api/auth/public-bootstrap`, `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/refresh`, and `GET /api/auth/operations/{operation_id}/status`. Other `/api/*` paths, including `GET /api/auth/session`, require a valid access-token cookie. Auth is bypassed for all management routes while operator auth is disabled. A persisted fail-closed auth transition (`enabling_fail_closed` or `rollback_required`) blocks ordinary management before any domain handler with the registered typed `503`, while the auth-control settings surface and the public auth paths above stay reachable as the repair path.

## 3. Health Check

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

The handler reports process readiness and completed startup. It runs no per-request PostgreSQL or provider probe. See [deployment inspection](operations.md#deployment-inspection) for separate database health checks.

---

## 8. Error Responses

Prism does not have one universal error envelope. Management handlers normally return:

```json
{
  "detail": "human-readable detail"
}
```

Field-scoped validation failures flatten their locator onto the same top level rather than nesting it, so `field`, `path`, `reason`, and where applicable `index` and `limit` appear as siblings of `detail`. Clients discriminate on `field` rather than on the detail text, which is not a stable contract.

The auth control plane (and any surface converging on it) uses the canonical flat management problem envelope owned by `internal/httpapi/management/responseutil`:

```json
{
  "code": "auth_login_locked",
  "detail": "登录尝试过多，请稍后重试",
  "params": {},
  "details": {
    "retry_at": "2026-08-11T02:15:00Z",
    "retry_after_seconds": 900
  },
  "request_id": "a1b2c3d4e5f60718"
}
```

Registered auth problem codes (registry in `internal/httpapi/management/auth/problems.go`):

| Code | Status | Route matcher | Retry/recovery semantics |
| --- | --- | --- | --- |
| `auth_not_authenticated` | 401 | protected management pre-handler | coordinator refresh then replay once; recovery `session_refresh` |
| `auth_not_enabled` | 400 | `POST /api/auth/login \| POST /api/auth/logout` | never retry; recovery `public_auth_bootstrap` |
| `auth_invalid_credentials` | 401 | `POST /api/auth/login` | correct credentials; recovery `correct_credentials` |
| `auth_login_locked` | 429 | `POST /api/auth/login` | operator after `retry_at`; same-source `Retry-After` required; recovery `wait_then_resubmit` |
| `auth_transition_in_progress` | 503 | ordinary management or auth transition clients | coordinator public-status only; optional same-source `Retry-After`; recovery `confirm_public_status` |
| `auth_transition_recovery_required` | 503 | ordinary management or auth transition clients | coordinator public-status only; recovery `confirm_public_status` |

`params` is the exact registered empty object for every auth code; `details` is the registered typed payload (empty object, `AuthLoginLockedDetails`, or `AuthTransitionProblemDetails`); no password, cookie, Authorization, subject existence, failure count, throttle key, username or session identity is ever exposed.

Profile-resolution failures add a stable `code`:

```json
{
  "code": "profile_scope_profile_not_found",
  "detail": "Profile 1 not found"
}
```

Audit and statistics domain-validation errors use the flat management problem envelope (`code`, `detail`, `params`, `details`, `request_id`). `details` is present only when the domain error includes structured detail:

```json
{
  "code": "audit_window_too_large",
  "detail": "Audit event windows may not exceed 7 days.",
  "params": {},
  "details": {
    "max_window_seconds": 604800
  },
  "request_id": "..."
}
```

Unregistered management paths and wrong-method management writes return the same flat problem envelope (`management_route_not_found` / `management_method_not_allowed`). `request_id` comes from the chi `RequestID` middleware when present, so it can be aligned with server-side logs.

Runtime handlers return `detail`, and include `error` only when the runtime error has a machine-readable code:

```json
{
  "error": "admission_exhausted",
  "detail": "No eligible Terminal Target passed admission."
}
```

Transport exhaustion returns the fixed `transport_error` shape with a normalized `last_failure` classification label:

```json
{
  "error": "transport_error",
  "detail": "All connections failed for model 'gpt-4o'. Last failure: upstream_connect_failed.",
  "route_reason": "no_healthy_upstream",
  "last_failure": "upstream_connect_failed"
}
```

`last_failure` takes one of `upstream_connect_failed` / `upstream_tls_failed` / `upstream_dns_failed` / `upstream_timeout` / `client_disconnected` / `upstream_http_<status>` / `unknown_upstream_failure` and never contains an upstream address.

The request-size guard is a separate envelope with `error`, `message`, and `limit_bytes`, as documented for [management requests](#10-request-boundary) and [runtime operations](runtime-api.md#22-unsupported-routes-and-methods).

| Status Code | Meaning |
| --- | --- |
| 400 | Bad request (invalid input) |
| 401 | Management session or runtime proxy key is missing or invalid |
| 405 | Wrong method for an allowlisted runtime path |
| 404 | Resource not found |
| 409 | Conflict (duplicate scoped identifier) |
| 413 | Request body exceeds the mounted route limit |
| 429 | Login locked (typed `auth_login_locked` with `Retry-After`) |
| 502 | Upstream service error |
| 503 | Runtime routing/admission unavailable, another service snapshot is unavailable, or a persisted auth transition is in progress / needs recovery |
