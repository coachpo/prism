# BACKEND MANAGEMENT SETTINGS KNOWLEDGE BASE

## OVERVIEW
`management/settings/` owns Prism's management settings routes for costing and timezone preferences pinned to Default profile id `1`, API-family audit settings pinned to Default profile id `1`, global log-retention settings, and creation of low-priority log-retention maintenance jobs.

## STRUCTURE
```text
settings/
├── routes.go       # `/api/settings/*` handlers and retention-job creation
├── service.go      # Service wiring, CORS snapshot, jobs store, route mounting
├── store.go        # Settings persistence for costing, timezone, and retention
├── types.go        # Request/response payloads and validation helpers
└── routes_test.go  # Route-level contract coverage
```

## WHERE TO LOOK
- Mounted routes and ownership split: `service.go` (`MountManagementRoutes`), `routes.go`
- Default-profile costing, timezone, and `/api/settings/audit` reads/writes: `routes.go`, `store.go`, `types.go`
- Global log-retention reads/writes and maintenance-job creation: `routes.go`, `../../../platform/managementjobs/`
- Frozen Default profile resolution and shared transaction helpers: `../../../profiledomain/`, `../../../pgxutil/tx.go`
- Frontend settings consumers: `../../../../../frontend/src/pages/settings/`, `../../../../../frontend/src/pages/settings/costing/`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep costing, timezone, and API-family audit settings pinned to Default profile id `1`; `X-Profile-Id` may be accepted but is ignored. Keep storage `profile_id` columns untouched.
- Keep `/api/settings/audit` as exactly three rows (`openai`, `anthropic`, `gemini`) with full replacement PUT semantics. Body capture requires audit enabled.
- Keep costing model choices aligned with unified model access; don't filter choices by retired model families.
- Keep log-retention settings global and trigger cleanup through low-priority management jobs instead of request-path deletes.
- Keep startup bootstrap config ownership separate; this package does not edit startup files.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not treat global log-retention settings as profile-scoped state.
- Do not run partition cleanup or retention deletes inline in these handlers.
- Do not treat frontend-side settings validation as the source of truth when this package already owns the backend validation and persistence contract.
- Do not mix startup bootstrap, auth-session, or sidecar control-plane behavior into this package.
