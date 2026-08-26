# BACKEND MANAGEMENT CONFIG RULES KNOWLEDGE BASE

## OVERVIEW
`management/configrules/` owns Default-profile configuration rules under `/api/config/*`. It manages header-blocklist rules and user-agent/client mapping rules used by management and runtime-adjacent helpers. `/api/profiles*` management CRUD is removed; profiles stay frozen on Default id `1`.

## STRUCTURE
```text
configrules/
├── service.go                         # Service construction and `/config` route mounting
├── routes.go                          # Shared route decode, error, query, and path boundary
├── header_blocklist_routes.go         # Header Blocklist HTTP CRUD
├── header_blocklist_validation.go     # Header Blocklist normalization and validation
├── header_blocklist_store.go          # Header Blocklist persistence and duplicate checks
├── user_agent_client_routes.go        # User-Agent Client Rule HTTP CRUD
├── user_agent_client_validation.go    # User-Agent Client Rule normalization and validation
├── user_agent_client_store.go         # User-Agent Client Rule persistence
├── config_rule_query_contract.go      # PostgreSQL executor contract
├── config_rule_db_values.go           # Rule nullable projections and defaults
├── types.go                           # Rule request and response shapes
└── *_test.go                          # Route-level regression coverage
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Header-blocklist list/get/create/update/delete: `header_blocklist_routes.go`; field normalization: `header_blocklist_validation.go`; persistence and duplicate checks: `header_blocklist_store.go`.
- User-agent/client rule list/get/create/update/delete: `user_agent_client_routes.go`; field normalization: `user_agent_client_validation.go`; persistence: `user_agent_client_store.go`.
- Shared route decode/error/query boundary: `routes.go`; PostgreSQL executor and scalar values: `config_rule_query_contract.go`, `config_rule_db_values.go`.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep these routes under `/api/config/*`; startup bootstrap config is file-owned under `platform/config/`.
- Keep rules pinned to Default profile id `1`, while allowing system rules to appear where the store includes them. `X-Profile-Id` compatibility headers are ignored.
- Don't let system header-blocklist rules be deleted or reshaped; `/api/profiles*` CRUD is removed.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When rule behavior affects upstream request headers or client attribution, check operation behavior across OpenAI, Anthropic, and Gemini shapes.

## ANTI-PATTERNS
- Do not mix startup file state into this package.
- Do not make user-agent/client rules global unless the store and routes explicitly support that boundary.
- Do not allow removed `/api/profiles*` CRUD to delete or reshape system header-blocklist rules.
