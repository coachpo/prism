# BACKEND MANAGEMENT ENDPOINTS KNOWLEDGE BASE

## OVERVIEW
`management/endpoints/` owns endpoint CRUD under `/api/endpoints*` pinned to Default profile id `1`. Endpoints are ordered provider base URLs with encrypted API keys and connection dropdown support. `X-Profile-Id` may be accepted but is ignored; storage `profile_id` columns remain.

## STRUCTURE
```text
endpoints/
├── service.go    # Service construction and endpoint route mounting
├── routes.go     # CRUD, position moves, duplication, connection dropdowns
├── store.go      # Endpoint persistence and usage checks
└── types.go      # Endpoint request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Ordered endpoint listing, creation, update, delete, duplicate, and position move flows: `routes.go`.
- Connection dropdown data for endpoint-aware forms: `routes.go`, `store.go`.
- API-key encryption and base URL validation: `routes.go`, `endpointdomain` helpers.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep endpoint writes pinned to Default profile id `1` and position-normalized.
- Keep API keys encrypted at rest and masked from responses.
- Don't delete endpoints still referenced by connections.
- Inline endpoint creation for connection forms belongs to `connections/`, not this route package.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When endpoint base URL, auth, or provider-facing behavior changes, evaluate OpenAI, Anthropic, and Gemini endpoint expectations.

## ANTI-PATTERNS
- Do not return plaintext API keys from endpoint responses.
- Do not bypass endpoint position normalization after move, duplicate, or delete flows.
- Do not delete endpoints while connection usage rows still reference them.
- Do not move inline endpoint creation for connection forms out of `connections/`.
