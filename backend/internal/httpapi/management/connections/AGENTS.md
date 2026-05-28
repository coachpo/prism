# BACKEND MANAGEMENT CONNECTIONS KNOWLEDGE BASE

## OVERVIEW
`management/connections/` owns selected-profile standalone connection CRUD under `/api/connections`, connection reference reads under `/api/connections/{id}/references`, model target attachment routes under `/api/models/{model_config_id}/targets`, and `/api/pricing-templates/*`. Standalone connections bind endpoints to an `api_family`; models attach to them through ordered access targets, and pricing templates stay in this package.

## STRUCTURE
```text
connections/
├── service.go              # Service construction and route mounting
├── routes.go               # Standalone connection CRUD, references, and model target routes
├── health.go               # Connection-ID scoped persisted health checks
├── pricing_templates.go    # Pricing-template CRUD and validation
├── pricing_lookup.go       # Pricing-template connection usage lookup
├── store.go                # Profile-scoped connection, endpoint, model, rule, and pricing SQL
└── types.go                # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Standalone connection list, get, create, update, delete, references, and target attachment flows: `routes.go`.
- Inline endpoint creation and endpoint/model lookup helpers: `routes.go`, `store.go`.
- Persisted connection health checks with header-blocklist filtering: `health.go`.
- Pricing-template CRUD, connection assignment, and usage lookup: `pricing_templates.go`, `pricing_lookup.go`.

## CONVENTIONS
- Keep pricing templates here, not in a separate management package.
- Keep all reads and writes selected-profile scoped through `ResolveEffectiveProfile`.
- Keep connection ordering on model access-target rows through `/api/models/{model_config_id}/targets` reorder flows.
- Keep standalone connection deletes blocked while `/api/connections/{id}/references` reports model target usage.
- Keep endpoint secrets encrypted at rest through the shared endpoint-domain helpers.

## LLM UPSTREAM MATRIX
- When health checks or probe variants change, evaluate OpenAI, Anthropic, and Gemini connection behavior instead of assuming one provider shape covers all families.

## ANTI-PATTERNS
- Do not move pricing-template CRUD or lookup into a separate management package.
- Do not accept both `endpoint_id` and `endpoint_create` on one connection write.
- Do not reintroduce the retired route family, connection ownership lookup routes, or connection-level ordering moves.
- Do not run health probes without the profile's enabled header-blocklist rules.
