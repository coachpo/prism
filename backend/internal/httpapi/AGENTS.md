# BACKEND HTTP API KNOWLEDGE BASE

## OVERVIEW
`backend/internal/httpapi/` owns mounted backend HTTP handlers and shared handler seams. It splits the management API, runtime proxy surface, realtime websocket delivery, proxy-key usage capture, log-retention job endpoints, and request-context helpers while platform code owns server assembly.

## STRUCTURE
```text
httpapi/
├── management/      # `/api/*` management handlers and services
├── proxykeyusage/   # Runtime proxy-key usage persistence helper
├── realtime/        # WebSocket service and async dashboard publisher
├── requestcontext/  # Request metadata helpers shared by handlers
└── runtime/         # `/v1` and `/v1beta` runtime proxy handlers
```

## WHERE TO LOOK
- Management subpackages: `management/auth/`, `management/bootstrapconfig/`, `management/configbundle/`, `management/configrules/`, `management/connections/`, `management/endpoints/`, `management/loadbalance/`, `management/models/`, `management/profiles/`, `management/settings/`, `management/sidecars/`, `management/stats/`, `management/vendors/`, `management/audit/`
- Management auth status/session/bootstrap, proxy-key, WebAuthn, reset-email, realtime, and runtime-cache seams: `management/auth/AGENTS.md`
- Global sidecar registration, CLIProxyAPI sync, auth/provider inventory, direct auth-file mutation, and worker seams: `management/sidecars/AGENTS.md`
- Runtime proxy leaf and package entry points: `runtime/AGENTS.md`, `runtime/runtime.go`, `runtime/service.go`, `runtime/cache.go`, `runtime/log_partitions.go`, `runtime/telemetry_outbox.go`
- Management settings leaf and package routes: `management/settings/AGENTS.md`, `management/settings/`
- Realtime websocket service and dashboard publisher: `realtime/service.go`, `realtime/dashboard_publisher.go`
- Proxy-key usage capture: `proxykeyusage/`
- Request-context helpers shared across mounted handlers: `requestcontext/`
- Router mounting and `/metrics`: `../platform/http/server.go`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep management selected-profile scope separate from runtime active-profile routing. `X-Profile-Id` affects profile-scoped `/api/*` management calls, not proxy traffic or global sidecar management.
- Keep runtime proxy routes under `runtime/`; they are served from `/v1` and `/v1beta` and remain separate from `/api/*` management handlers.
- Keep `api_family` as runtime compatibility truth. Vendor rows and `icon_key` are presentation metadata.
- Do not invent backend-local generated docs artifacts; keep durable API reference updates in the markdown docs.
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
