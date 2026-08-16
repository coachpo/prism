# BACKEND MANAGEMENT AUTH KNOWLEDGE BASE

## OVERVIEW
`management/auth/` owns Prism's management-auth surface: tagged public auth status, session cookies and refresh flows, the auth problem registry with the flat management error envelope, auth transition operations (fail-closed enable, rollback, disable), proxy API keys, runtime-auth cache publication, and proxy-key setup readiness. It also owns the runtime middleware that separates management session enforcement from `/v1` and `/v1beta` proxy-key authentication.

## STRUCTURE
```
auth/
├── routes.go                  # Route mounting, management/runtime middleware, handlers
├── service.go                 # Auth settings load/update, publish proof, rollback transition
├── store.go                   # Session, refresh-token, settings, proxy-key persistence
├── tokens.go                  # Access/refresh token minting, rotation, revocation
├── cookies.go                 # Access/refresh cookie helpers
├── request_tokens.go          # Request-token helpers
├── operations.go              # Auth transition operations (operation id, status route)
├── settings_v2.go             # Immutable desired/effective auth handoff and Proxy readiness fence
├── problems.go                # Auth problem registry (flat envelope codes)
├── types.go                   # Tagged PublicAuthStatus, session/proxy-key payloads
├── runtime_config.go          # Runtime auth config snapshot (cookie names, TTLs)
├── runtime_cache.go           # Published runtime-auth snapshot + invalidation
├── proxy_key_usage_writer.go  # Proxy-key usage persistence
├── proxy_setup_readiness.go   # Proxy-key setup readiness projection (sixth setup fact)
└── *_test.go                  # Registry, middleware matrix, store, runtime-cache tests
```

## WHERE TO LOOK
- Route mounting and handlers: `routes.go`, `service.go`
- Tagged PublicAuthStatus union and session payloads: `types.go`, `routes.go` (`buildPublicAuthStatus`, `buildAuthenticatedSession`)
- Auth problem registry and flat envelope writer: `problems.go`, `../responseutil/problem_envelope.go`
- Auth settings, publish proof (`validateAuthSettingsPublished`), rollback transition (`enterAuthRollbackRequired`), and route construction: `service.go`, `routes.go`, `runtime_config.go`, `runtime_cache.go`
- Session persistence and refresh-token lifecycle: `store.go`, `types.go`, `tokens.go`, `routes_test.go`, `store_test.go`, `runtime_cache_test.go`
- Cookie and request-token helpers: `cookies.go`, `request_tokens.go`
- Transition operations and `GET /api/auth/operations/{operation_id}/status`: `operations.go`
- Proxy API key capture and usage writer: `proxy_key_usage_writer.go`, `../../proxykeyusage/`
- Proxy-key setup readiness projection: `proxy_setup_readiness.go`
- v2 auth settings and Proxy readiness capture: `settings_v2.go`; the readiness count is one server-clock snapshot with a 30-second safe-active horizon and is the only source used by auth enablement and setup handoff.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing surface is itself a shipped contract or guardrail.
- Keep the auth problem registry as the single source of truth for auth error codes. Every entry binds an exact route matcher, wire params (exact empty objects), details schema, retry policy, recovery kind, and Retry-After rule; the Go registry, the TypeScript known-code decoder, the coordinator classifier, and the zh-CN catalog stay exhaustive against it.
- Keep `GET /api/auth/status` a tagged union (`state` = `enabled|disabled|transition_fail_closed`, plus `transition_state`, `login_available`, `effective_generation`). Never regress to an untagged `auth_enabled`-only payload; the E2E fixture contract is the tagged shape.
- Keep authenticated session responses strict `{authenticated, auth_enabled, username, subject_key}`; `subject_key` is server-authored and never appears in public status or anonymous/disabled payloads.
- Keep session enforcement and transition semantics out of runtime proxy traffic. `/v1` and `/v1beta` proxy-key 401s are runtime credential failures, never management session expiry; the runtime middleware stays fail-closed on enforcement and permissive-but-honest on attribution when auth is off.
- Keep persisted transition states real: `enabling_fail_closed` and `rollback_required` block ordinary management (typed 503) before any domain handler; the auth-control settings surface, public auth status, and `operations/{operation_id}/status` remain reachable as the repair path. `disabling_enforced` keeps the old enabled gate.
- Keep auth-control writes proven: a settings write must round-trip through a fresh DB read before it is reported effective; a failed proof reverts the write and persists a durable `rollback_required` transition with the initiating browser operation id (never a second mutation from a lost response).
- Acquire the shared affected Requests/Audit writer admission before auth reads/writes that participate in the transition proof. Proxy-key writes and readiness reads acquire the Proxy readiness row fence; do not use a second live-key predicate for activation.
- Keep secrets and tokens write-only; response payloads expose metadata or one-time generated values only. Never leak failure counts, subject existence, throttle keys, or session identity in auth problem details.
- Publish runtime-auth changes through the auth runtime-cache seam instead of making runtime handlers query management state.
- Keep proxy-key usage persistence shared through `proxykeyusage/`.
- Proxy-key lifecycle owns the typed capacity snapshot (`{limit, used, remaining, counted_at}` from one server clock), atomic capacity serialization on the singleton auth settings row, presence-aware expiry (omitted=preserve, null=clear, RFC3339=future set with `proxy_key_expiry_invalid`), typed errors (`proxy_key_capacity_exhausted`, `proxy_key_not_found`, `proxy_key_not_rotatable`), and `private, no-store` create/rotate responses. Rotation is in-place on the same row: the id is a logical key's stable identity for retained request/usage attribution, so never reintroduce a successor-row rotation.
- Runtime auth middleware owns permissive attribution: auth-off still extracts and optionally verifies credentials, writing `identified`/`none`/`unknown` with `AuthEnforced` frozen per request; optional lookup failure is fail-open for execution and fail-closed for identity.
- Do not return raw stored secrets, reset codes, verification tokens, or proxy-key hashes.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not duplicate cookie, token, request-token, or proxy-key helpers in sibling management packages.
- Do not return raw stored secrets, reset codes, verification tokens, or proxy-key hashes.
- Do not let runtime proxy handlers depend on management Default-profile state.
- Do not treat runtime proxy-key 401s as management session expiry or route them through the coordinator refresh path.
- Do not fabricate `false,false` or `true,false` status answers; the tagged status union and typed transition 503s are the only legal shapes during transitions.
