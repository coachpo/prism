# BACKEND MANAGEMENT CONNECTIONS KNOWLEDGE BASE

## OVERVIEW
`management/connections/` owns selected-profile read-only connection list/get routes under `/api/connections`, connection reference reads under `/api/connections/{id}/references`, owner-scoped private connection mutation routes under `/api/models/{model_config_id}/connections`, model target attachment routes under `/api/models/{model_config_id}/targets`, and `/api/pricing-templates/*`. Private connections bind reusable endpoints to an owner model's `api_family`, and pricing templates stay in this package.

## STRUCTURE
```text
connections/
├── service.go              # Service construction and route mounting
├── routes.go               # Read-only connection routes, references, owner-scoped mutations, and model target routes
├── health.go               # Owner-scoped persisted health checks
├── pricing_templates.go    # Pricing-template CRUD and validation
├── pricing_lookup.go       # Pricing-template connection usage lookup
├── store.go                # Profile-scoped connection, endpoint, model, rule, and pricing SQL
└── types.go                # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Read-only connection list/get, reference reads, owner-scoped create/update/delete, and target attachment flows: `routes.go`.
- Inline endpoint creation and endpoint/model lookup helpers: `routes.go`, `store.go`.
- Owner-scoped connection health checks with header-blocklist filtering: `health.go`.
- Pricing-template CRUD, connection assignment, and usage lookup: `pricing_templates.go`, `pricing_lookup.go`.

## CONVENTIONS
- Keep pricing templates here, not in a separate management package.
- Keep all reads and writes selected-profile scoped through `ResolveEffectiveProfile`.
- Keep connection ordering on model access-target rows through `/api/models/{model_config_id}/targets` reorder flows.
- Keep public `/api/connections` mutation routes rejected; private connection deletes go through owner-scoped model routes while `/api/connections/{id}/references` remains the read-only owner lookup.
- Keep endpoint secrets encrypted at rest through the shared endpoint-domain helpers.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When health checks or probe variants change, evaluate OpenAI, Anthropic, and Gemini connection behavior instead of assuming one provider shape covers all families.

## ANTI-PATTERNS
- Do not move pricing-template CRUD or lookup into a separate management package.
- Do not accept both `endpoint_id` and `endpoint_create` on one connection write.
- Do not reintroduce the retired owner lookup route, legacy auxiliary mutation routes, or connection-level ordering moves.
- Do not run health probes without the profile's enabled header-blocklist rules.
