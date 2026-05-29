# BACKEND MANAGEMENT AUTH KNOWLEDGE BASE

## OVERVIEW
`management/auth/` owns Prism's management-auth surface: public auth bootstrap/status, session cookies, password reset and recovery email flows, WebAuthn methods, proxy API keys, auth runtime-cache publication, and realtime auth state notifications.

## WHERE TO LOOK
- Route mounting and handlers: `routes.go`
- Auth service orchestration and runtime settings: `service.go`, `runtime_config.go`, `runtime_cache.go`
- Persistence boundary: `store.go`, `types.go`
- Cookie, token, and request-token helpers: `cookies.go`, `tokens.go`, `request_tokens.go`
- Proxy API key capture and usage writer: `proxy_key_usage_writer.go`, `../../proxykeyusage/`
- Email reset and recovery side effects: `email_outbox_phase6_test.go`, `../../../platform/email/`
- Realtime notifications: `realtime.go`, `../../realtime/`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing surface is itself a shipped contract or guardrail.
- Keep auth selected-profile neutral unless a route explicitly manages proxy API keys or profile-scoped settings.
- Keep raw secrets and tokens write-only; response payloads expose metadata or one-time generated values only.
- Publish runtime-cache changes through the auth runtime-cache seam instead of making runtime handlers query management state.
- Keep email delivery on the durable outbox and configured mailer path.
- Keep proxy-key usage persistence shared through `proxykeyusage/`.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not duplicate cookie, token, WebAuthn, or proxy-key helpers in sibling management packages.
- Do not return raw stored secrets, reset codes, passkeys, or proxy-key hashes.
- Do not send recovery email inline on the request path.
- Do not let runtime proxy handlers depend on management selected-profile state.
