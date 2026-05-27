# BACKEND MANAGEMENT MODELS KNOWLEDGE BASE

## OVERVIEW
`management/models/` owns selected-profile model configuration routes under `/api/models*`. It manages native and proxy model CRUD plus model lookups by endpoint for endpoint detail surfaces.

## STRUCTURE
```text
models/
├── service.go    # Service construction and model route mounting
├── routes.go     # Model CRUD and by-endpoint lookup handlers
├── store.go      # Model, relation, proxy-target, vendor, and strategy SQL
└── types.go      # Model request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Model list/get/create/update/delete: `routes.go`.
- `/models/by-endpoint/{endpoint_id}` and `/models/by-endpoints`: `routes.go`.
- Native/proxy validation, proxy targets, vendor links, and strategy links: `routes.go`, `store.go`.

## CONVENTIONS
- Keep model `api_family` as runtime compatibility truth.
- Keep selected-profile model IDs unique inside the profile.
- Keep native model load-balance strategy checks in this package, but strategy CRUD in `loadbalance/`.
- Keep connection CRUD in `connections/`, even when model detail responses include connections.

## LLM UPSTREAM MATRIX
- When model `api_family`, proxy targets, or native/proxy validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS
- Do not treat vendor metadata as runtime compatibility; use model `api_family`.
- Do not convert native models with connections to proxy models without deleting connections first.
- Do not let proxy models point at incompatible or missing native targets.
- Do not move connection CRUD into model handlers.
