# BACKEND MANAGEMENT ENDPOINTS KNOWLEDGE BASE

## OVERVIEW
`management/endpoints/` owns selected-profile endpoint CRUD under `/api/endpoints*`. Endpoints are ordered provider base URLs with encrypted API keys and connection dropdown support.

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
- Keep endpoint writes selected-profile scoped and position-normalized.
- Keep API keys encrypted at rest and masked from responses.
- Don't delete endpoints still referenced by connections.
- Inline endpoint creation for connection forms belongs to `connections/`, not this route package.

## LLM UPSTREAM MATRIX
- When endpoint base URL, auth, or provider-facing behavior changes, evaluate OpenAI, Anthropic, and Gemini endpoint expectations.

## ANTI-PATTERNS
- Do not return plaintext API keys from endpoint responses.
- Do not bypass endpoint position normalization after move, duplicate, or delete flows.
- Do not delete endpoints while connection usage rows still reference them.
- Do not move inline endpoint creation for connection forms out of `connections/`.
