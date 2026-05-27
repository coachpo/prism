# BACKEND MANAGEMENT CONNECTIONS KNOWLEDGE BASE

## OVERVIEW
`management/connections/` owns profile-scoped model connection management under `/api/models/*/connections`, `/api/connections/*`, and `/api/pricing-templates/*`. It binds configured models to endpoints, manages inline endpoint creation during connection writes, and keeps pricing templates in this package.

## STRUCTURE
```text
connections/
├── service.go              # Service construction and route mounting
├── routes.go               # Connection CRUD, owner lookup, priority moves
├── health.go               # Persisted and preview connection health checks
├── pricing_templates.go    # Pricing-template CRUD and validation
├── pricing_lookup.go       # Pricing-template connection usage lookup
├── store.go                # Profile-scoped connection, endpoint, model, rule, and pricing SQL
└── types.go                # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Connection list, create, update, delete, priority, and owner flows: `routes.go`.
- Inline endpoint creation and endpoint/model lookup helpers: `routes.go`, `store.go`.
- Preview and persisted health checks with header-blocklist filtering: `health.go`.
- Pricing-template CRUD, connection assignment, and usage lookup: `pricing_templates.go`, `pricing_lookup.go`.

## CONVENTIONS
- Keep pricing templates here, not in a separate management package.
- Keep all reads and writes selected-profile scoped through `ResolveEffectiveProfile`.
- Keep connection ordering explicit through priority moves and normalization.
- Keep endpoint secrets encrypted at rest through the shared endpoint-domain helpers.

## LLM UPSTREAM MATRIX
- When health checks or probe variants change, evaluate OpenAI, Anthropic, and Gemini connection behavior instead of assuming one provider shape covers all families.

## ANTI-PATTERNS
- Do not move pricing-template CRUD or lookup into a separate management package.
- Do not accept both `endpoint_id` and `endpoint_create` on one connection write.
- Do not bypass priority normalization when creating, moving, or deleting connections.
- Do not run health probes without the profile's enabled header-blocklist rules.
