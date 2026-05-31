# BACKEND HTTP API KNOWLEDGE BASE

## OVERVIEW
`backend/internal/httpapi/` owns mounted backend HTTP handlers and shared handler seams. It splits the management API, runtime proxy surface, realtime websocket delivery, proxy-key usage capture, log-retention job endpoints, and request-context helpers while platform code owns server assembly.

## STRUCTURE
```text
httpapi/
├── management/      # `/api/*` management handlers and services
├── proxykeyusage/   # Runtime proxy-key usage persistence helper
├── realtime/        # `/api/realtime/ws`, connection manager, dashboard and analytics publishers
├── requestcontext/  # Request metadata helpers shared by handlers
└── runtime/         # Operation-registered `/v1` and `/v1beta` runtime proxy handlers
```

## WHERE TO LOOK
- Management subpackages: `management/auth/`, `management/bootstrapconfig/`, `management/configbundle/`, `management/configrules/`, `management/connections/`, `management/endpoints/`, `management/loadbalance/`, `management/models/`, `management/profiles/`, `management/settings/`, `management/sidecars/`, `management/stats/`, `management/vendors/`, `management/audit/`
- Management child docs for CRUD and observability leaves: `management/audit/AGENTS.md`, `management/connections/AGENTS.md`, `management/configrules/AGENTS.md`, `management/endpoints/AGENTS.md`, `management/loadbalance/AGENTS.md`, `management/models/AGENTS.md`, `management/profiles/AGENTS.md`, `management/stats/AGENTS.md`, `management/vendors/AGENTS.md`
- Startup bootstrap ownership: `management/bootstrapconfig/AGENTS.md`, `management/bootstrapconfig/service.go`
- Config bundle and vendor catalog ownership: `management/configbundle/AGENTS.md`, `management/configbundle/service.go`, `management/configbundle/routes.go`
- Management auth status/session/bootstrap, password-reset or verification delivery, proxy-key, realtime auth-state, and runtime-cache seams: `management/auth/AGENTS.md`
- Global sidecar registration, CLIProxyAPI sync, auth/provider inventory, direct auth-file mutation, and worker seams: `management/sidecars/AGENTS.md`
- Runtime proxy leaf, operation registry, ingress rejection semantics, planning helpers, and hook collections: `runtime/AGENTS.md`, `runtime/operations.go`, `runtime/service.go`, `runtime/runtime.go`, `runtime/planning_snapshot.go`, `runtime/proxy_selector_helpers.go`, `runtime/operation_request_hooks.go`, `runtime/operation_response_hooks.go`, `runtime/operation_stream_hooks.go`, `runtime/operation_media_hooks.go`
- Realtime websocket contract, channels, auth gate, and async publishers: `realtime/AGENTS.md`, `realtime/service.go`, `realtime/manager.go`, `realtime/async_publisher.go`, `realtime/async_analytics_publisher.go`
- Management settings leaf and package routes: `management/settings/AGENTS.md`, `management/settings/`
- Proxy-key usage capture: `proxykeyusage/`
- Request-context helpers shared across mounted handlers: `requestcontext/`
- Router mounting and OTLP-first operations telemetry: `../platform/http/server.go`, `../platform/telemetry/`, `../platform/db/telemetry.go`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep management selected-profile scope separate from runtime active-profile routing. `X-Profile-Id` affects profile-scoped `/api/*` management calls, not proxy traffic or global sidecar management.
- Keep runtime proxy routes under `runtime/`; they are served from mounted `/v1` and `/v1beta` prefixes, but the exact supported operations live only in `runtime/operations.go`.
- Keep realtime websocket delivery under `realtime/`; do not mix it into runtime or unrelated management packages.
- Keep `api_family` as runtime compatibility truth. Vendor rows and `icon_key` are presentation metadata.
- Keep startup bootstrap and config-bundle behavior in their own management subpackages instead of folding them into `settings/` or other CRUD surfaces.
- Do not invent backend-local generated docs artifacts; keep durable API reference updates in the markdown docs.
- Keep request-log and dashboard materialization off the hot request path by using runtime telemetry outboxes and realtime publishers.
- Keep runtime partition creation on `runtime/log_partitions.go` and `platform/logretention.Store`; handlers should not create or drop partitions directly.
- Keep log-retention settings global in `management/settings/`, with cleanup triggered through low-priority management jobs instead of request-path cleanup.
- Keep operations telemetry on startup-JSON OTLP providers; do not reintroduce a backend-local `/metrics` compatibility endpoint. Stats handlers under management remain product-facing retained-history APIs.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not add unsupported providers, proxy routes, or realtime message types without updating docs and contracts.
- Do not inject profile scope inside runtime proxy handlers.
- Do not describe the mounted `/v1` and `/v1beta` prefixes as a generic passthrough contract; runtime allowlisting and hook selection belong under `runtime/`.
- Do not run retention deletes, partition drops, or horizon creation inside HTTP handlers.
- Do not duplicate request-context or proxy-key usage helpers inside individual endpoint packages.
- Do not duplicate auth cookie, token, request-token, or proxy-key helpers outside `management/auth/`.
