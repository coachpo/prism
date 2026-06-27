# BACKEND MANAGEMENT HTTPAPI KNOWLEDGE BASE

## OVERVIEW
`backend/internal/httpapi/management/` owns Prism's `/api/*` management fanout. It routes selected-profile CRUD, startup bootstrap reads/writes, profile bundle import/export, auth/session/proxy-key flows, observability reads, retention jobs, and shared management response helpers while platform HTTP owns mounting and middleware.

## STRUCTURE
```text
management/
├── auth/            # auth bootstrap/status, sessions, proxy API keys, runtime auth cache, realtime auth state
├── audit/           # audit-log reads and management job list/get/cancel
├── bootstrapconfig/ # file-backed `/api/config/bootstrap` snapshot, validate, update, hot-apply reporting
├── configbundle/    # profile bundle export/import, preview tokens, secret crypto, graph validation
├── configrules/     # User-Agent Client Rules CRUD
├── connections/     # private connections, health probes, pricing templates
├── endpoints/       # endpoint CRUD, encrypted keys, ordering, duplication
├── loadbalance/     # strategy CRUD, current-state reset, event reads
├── models/          # model CRUD and access targets
├── profiles/        # selected-profile lifecycle
├── responseutil/    # shared profile/error response helpers
├── settings/        # costing, timezone, audit settings, global log-retention jobs
└── stats/           # dashboard, usage, spending, request-log read APIs
```

## WHERE TO LOOK
- Router assembly and management middleware order: `../../../platform/http/management_branch.go`
- Auth/session/proxy-key/runtime-auth cache/realtime auth-state seams: `auth/AGENTS.md`, `auth/`
- File-backed startup config, hot/restart classification, safe secret responses: `bootstrapconfig/AGENTS.md`, `bootstrapconfig/service.go`
- Profile bundle import/export and preview-token path: `configbundle/AGENTS.md`, `configbundle/import.go`, `configbundle/routes.go`
- Model graph authoring and validation: `models/AGENTS.md`, `models/routes.go`, `models/store.go`
- Endpoint, connection, load-balance, and config-rule CRUD leaves: `endpoints/AGENTS.md`, `connections/AGENTS.md`, `loadbalance/AGENTS.md`, `configrules/AGENTS.md`
- Product observability and retention-job APIs: `stats/AGENTS.md`, `audit/AGENTS.md`, `settings/AGENTS.md`
- Shared profile/error response shaping used across leaves: `responseutil/profile_errors.go`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep `/api/*` management handlers here; server mounting, admission, runtime-cache invalidation middleware, telemetry middleware, and CORS snapshots stay in `../../../platform/http/`.
- Keep selected-profile CRUD scoped through effective-profile resolution and `X-Profile-Id` only where the leaf contract says so; runtime proxy traffic never depends on selected-profile state.
- Keep raw secrets, tokens, endpoint keys, SMTP passwords, and bundle secrets write-only or metadata-only in responses.
- Keep startup bootstrap config file-backed in `bootstrapconfig/`; PostgreSQL-backed profile bundles and settings stay in their own leaves.
- Keep request-path side effects on durable outboxes, scheduler workers, or platform mutation middleware. Handlers should not publish dashboards, send email, invalidate runtime caches, or run retention cleanup inline.
- Keep shared profile/error response helpers in `responseutil/` instead of cloning profile error shaping across leaves.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new management env knobs.

## LLM UPSTREAM MATRIX
- When management work changes model, endpoint, runtime-cache, audit, or request-log semantics that affect proxy behavior, evaluate OpenAI Chat/Responses, Anthropic, and Gemini operation shapes instead of assuming one provider family covers all effects.

## ANTI-PATTERNS
- Do not turn `settings/` into startup bootstrap ownership or profile bundle transport.
- Do not let frontend-side validation become the source of truth for model graph, bundle, endpoint, or settings contracts.
- Do not duplicate cookie, token, proxy-key, profile-error, or request-context helpers inside individual leaves.
- Do not expose stored plaintext secrets or hashes in management responses.
- Do not run partition cleanup, dashboard materialization, cache invalidation, email delivery, or provider sends inline from management handlers.
