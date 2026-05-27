# BACKEND MANAGEMENT CONFIG RULES KNOWLEDGE BASE

## OVERVIEW
`management/configrules/` owns selected-profile configuration rules under `/api/config/*`. It manages header-blocklist rules and user-agent/client mapping rules used by management and runtime-adjacent helpers.

## STRUCTURE
```text
configrules/
├── service.go    # Service construction and `/config` route mounting
├── routes.go     # Header-blocklist and user-agent/client rule handlers
├── store.go      # Rule persistence and duplicate checks
└── types.go      # Rule request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Header-blocklist list/get/create/update/delete: `routes.go`.
- User-agent/client rule list/get/create/update/delete: `routes.go`.
- System-rule mutability and duplicate checks: `routes.go`, `store.go`.

## CONVENTIONS
- Keep these routes under `/api/config/*`; startup bootstrap config belongs to `bootstrapconfig/`.
- Keep rules selected-profile scoped, while allowing system rules to appear where the store includes them.
- Don't let system header-blocklist rules be deleted or reshaped through profile CRUD.

## LLM UPSTREAM MATRIX
- When rule behavior affects upstream request headers or client attribution, check operation behavior across OpenAI, Anthropic, and Gemini shapes.

## ANTI-PATTERNS
- Do not mix `/api/config/bootstrap` startup state into this package.
- Do not make user-agent/client rules global unless the store and routes explicitly support that boundary.
- Do not allow profile CRUD to delete or reshape system header-blocklist rules.
