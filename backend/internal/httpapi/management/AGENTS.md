# BACKEND MANAGEMENT HTTPAPI KNOWLEDGE BASE

## OVERVIEW
`backend/internal/httpapi/management/` owns Prism's `/api/*` management fanout. It routes Default-profile-scoped CRUD, auth/session/proxy-key flows, observability reads, retention jobs, and shared management response helpers while platform HTTP owns mounting and middleware.

## STRUCTURE
```text
management/
├── auth/            # auth bootstrap/status, sessions, proxy API keys, runtime auth cache
├── audit/           # audit-log reads and management job list/get/cancel
├── configrules/     # User-Agent Client Rules CRUD
├── connections/     # private connections and pricing templates
├── endpoints/       # endpoint CRUD, encrypted keys, ordering, duplication
├── loadbalance/     # strategy CRUD, current-state reset, event reads
├── models/          # model CRUD and access targets
├── responseutil/    # shared profile/error response helpers
├── settings/        # costing, timezone, audit settings, global log-retention jobs
└── stats/           # dashboard, usage, spending, request-log read APIs
```

## WHERE TO LOOK
- Router assembly and management middleware order: `../../../platform/http/management_branch.go`
- Auth/session/proxy-key/runtime-auth cache seams: `auth/AGENTS.md`, `auth/`
- Model graph authoring and validation: `models/AGENTS.md`, `models/routes.go`, `models/store.go`
- Endpoint, connection, load-balance, and config-rule CRUD leaves: `endpoints/AGENTS.md`, `connections/AGENTS.md`, `loadbalance/AGENTS.md`, `configrules/AGENTS.md`
- Product observability and retention-job APIs: `stats/AGENTS.md`, `audit/AGENTS.md`, `settings/AGENTS.md`
- Shared profile/error response shaping used across leaves: `responseutil/profile_errors.go`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep `/api/*` management handlers here; server mounting, admission, runtime-cache invalidation middleware, and CORS snapshots stay in `../../../platform/http/`.
- Keep profile-scoped CRUD pinned through effective-profile resolution to Default id=1. `X-Profile-Id` is accepted for old clients but ignored.
- Keep raw secrets, tokens, and endpoint keys write-only or metadata-only in responses; startup mail config is parse-only compatibility data and has no management delivery behavior.
- Keep startup bootstrap config outside management CRUD; PostgreSQL-backed settings stay in their own leaves.
- Keep request-path side effects on durable outboxes, scheduler workers, or platform mutation middleware. Handlers should not publish dashboards, invalidate runtime caches, or run retention cleanup inline.
- Keep shared profile/error response helpers in `responseutil/` instead of cloning profile error shaping across leaves.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new management env knobs.

## LLM UPSTREAM MATRIX
- When management work changes model, endpoint, runtime-cache, audit, or request-log semantics that affect proxy behavior, evaluate OpenAI Chat/Responses, Anthropic, and Gemini operation shapes instead of assuming one provider family covers all effects.
- Terminal Target `custom_headers` is covered by the same rule: read APIs mask sensitive-named values with the `__prism_redacted__` sentinel and expose `custom_headers_redacted`; writes substitute the sentinel back from stored state, and never persist the sentinel itself.

## ANTI-PATTERNS
- Do not turn `settings/` into startup bootstrap ownership.
- Do not let frontend-side validation become the source of truth for model graph, endpoint, or settings contracts.
- Do not duplicate cookie, token, proxy-key, profile-error, or request-context helpers inside individual leaves.
- Do not expose stored plaintext secrets or hashes in management responses.
- Do not run partition cleanup, dashboard materialization, cache invalidation, or provider sends inline from management handlers.
