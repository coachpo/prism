# BACKEND MANAGEMENT BOOTSTRAP CONFIG KNOWLEDGE BASE

## OVERVIEW
`management/bootstrapconfig/` owns the file-backed startup bootstrap surface mounted at `/api/config/bootstrap`. It handles snapshot reads, validate/update planning, hot-apply classification, writable-path checks, and failed hot apply reporting for the bootstrap document itself.

## STRUCTURE
```text
bootstrapconfig/
├── service.go      # GET, validate, and PUT route ownership, live snapshot/classification, hot-apply publication
└── service_test.go # Route, hot-apply, validation, and file-durability contract coverage
```

## WHERE TO LOOK
- Route mounting and request handlers: `service.go` (`MountManagementRoutes`, `handleGetBootstrapConfig`, `handleValidateBootstrapConfig`, `handlePutBootstrapConfig`)
- Bootstrap response and diff/apply classification: `service.go`, `../../../platform/config/`
- Hot-apply publication and failure reporting: `service.go`, `../../../platform/http/hot_bootstrap_runtime.go`
- Frontend consumer: `../../../../../frontend/src/pages/settings/startup/`

## CONVENTIONS
- Keep bootstrap config file-backed and separate from `management/settings/`, profile/vendor bundle import/export, and sidecar control-plane state.
- Keep GET/validate/PUT behavior centered on loaded snapshot plus live settings comparison, planned changes, and apply results; do not reframe this package as a generic settings CRUD layer.
- Keep hot-eligible changes and restart-required changes classified explicitly, including `failed_hot_apply_fields`.
- Keep the writable-path guardrails and revision/etag semantics intact.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not mix `/api/config/bootstrap` with `management/settings/` or `management/configbundle/`.
- Do not treat bootstrap writes as DB-backed state.
- Do not swallow hot-apply failures or hide `failed_hot_apply_fields`.
- Do not move bootstrap document ownership into the frontend or the broader settings package.
