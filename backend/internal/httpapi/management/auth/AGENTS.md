# BACKEND MANAGEMENT AUTH KNOWLEDGE BASE

## OVERVIEW
`management/auth/` owns Prism's management-auth surface: auth bootstrap or status, session cookies and refresh flows, proxy API keys, runtime-auth cache publication, and realtime auth-state resolution.

## WHERE TO LOOK
- Route mounting and handlers: `routes.go`, `service.go`
- Auth settings, runtime cache, auth-decision telemetry, and route construction: `service.go`, `runtime_config.go`, `runtime_cache.go`, `telemetry.go`
- Session persistence and refresh-token lifecycle: `store.go`, `types.go`, `tokens.go`, `routes_test.go`, `store_test.go`, `runtime_cache_test.go`
- Cookie and request-token helpers: `cookies.go`, `request_tokens.go`
- Proxy API key capture and usage writer: `proxy_key_usage_writer.go`, `../../proxykeyusage/`
- - Realtime auth-state resolution used by `/api/realtime/ws`: `realtime.go`, `../../realtime/`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing surface is itself a shipped contract or guardrail.
- Keep auth selected-profile neutral unless a route explicitly manages proxy API keys or profile-scoped settings.
- Keep raw secrets and tokens write-only; response payloads expose metadata or one-time generated values only.
- Publish runtime-auth changes through the auth runtime-cache seam instead of making runtime handlers query management state.
- Keep proxy-key usage persistence shared through `proxykeyusage/`.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not duplicate cookie, token, request-token, or proxy-key helpers in sibling management packages.
- Do not return raw stored secrets, reset codes, verification tokens, or proxy-key hashes.
- Do not let runtime proxy handlers depend on management selected-profile state.
