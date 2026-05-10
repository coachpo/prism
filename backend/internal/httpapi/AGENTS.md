# BACKEND HTTP API KNOWLEDGE BASE

## OVERVIEW
`backend/internal/httpapi/` owns mounted backend HTTP handlers and shared handler seams. It splits the management API, runtime proxy surface, realtime websocket delivery, checked-in OpenAPI serving, proxy-key usage capture, log-retention job endpoints, and request-context helpers while platform code owns server assembly.

## STRUCTURE
```text
httpapi/
├── management/      # `/api/*` management handlers and services
├── openapi/         # OpenAPI artifact loading and docs handlers
├── proxykeyusage/   # Runtime proxy-key usage persistence helper
├── realtime/        # WebSocket service and async dashboard publisher
├── requestcontext/  # Request metadata helpers shared by handlers
└── runtime/         # `/v1` and `/v1beta` runtime proxy handlers
```

## WHERE TO LOOK
- Management subpackages: `management/auth/`, `management/bootstrapconfig/`, `management/configbundle/`, `management/configrules/`, `management/connections/`, `management/endpoints/`, `management/loadbalance/`, `management/models/`, `management/profiles/`, `management/settings/`, `management/sidecars/`, `management/stats/`, `management/vendors/`, `management/audit/`
- Management auth status/session/bootstrap, proxy-key, WebAuthn, reset-email, realtime, and runtime-cache seams: `management/auth/AGENTS.md`
- Global sidecar registration, CLIProxyAPI sync, watchdog, action history, and worker seams: `management/sidecars/AGENTS.md`
- Runtime proxy entry, log partition cache, and helpers: `runtime/runtime.go`, `runtime/service.go`, `runtime/cache.go`, `runtime/log_partitions.go`, `runtime/telemetry_outbox.go`
- Management settings costing, timezone, retention settings, and retention-job endpoints: `management/settings/`
- Realtime websocket service and dashboard publisher: `realtime/service.go`, `realtime/dashboard_publisher.go`
- OpenAPI loading and docs handlers: `openapi/`
- Proxy-key usage capture: `proxykeyusage/`
- Request-context helpers shared across mounted handlers: `requestcontext/`
- Router mounting and `/metrics`: `../platform/http/server.go`

## CONVENTIONS
- Keep management selected-profile scope separate from runtime active-profile routing. `X-Profile-Id` affects profile-scoped `/api/*` management calls, not proxy traffic or global sidecar management.
- Keep runtime proxy routes under `runtime/`; they are served from `/v1` and `/v1beta` and intentionally stay outside the management OpenAPI artifact.
- Keep `api_family` as runtime compatibility truth. Vendor rows and `icon_key` are presentation metadata.
- Keep OpenAPI serving aligned with root `docs/openapi.json`; do not invent a backend-local docs artifact.
- Keep request-log and dashboard materialization off the hot request path by using runtime telemetry outboxes and realtime publishers.
- Keep runtime partition creation on `runtime/log_partitions.go` and `platform/logretention.Store`; handlers should not create or drop partitions directly.
- Keep log-retention settings global in `management/settings/`, with cleanup triggered through low-priority management jobs instead of request-path cleanup.
- Keep `/metrics` DB-backed and mounted by platform server assembly, even though stats handlers live under management.

## ANTI-PATTERNS
- Do not add unsupported providers, proxy routes, or realtime message types without updating docs and contracts.
- Do not inject profile scope inside runtime proxy handlers.
- Do not run retention deletes, partition drops, or horizon creation inside HTTP handlers.
- Do not duplicate request-context or proxy-key usage helpers inside individual endpoint packages.
- Do not duplicate auth cookie, token, WebAuthn, or proxy-key helpers outside `management/auth/`.
