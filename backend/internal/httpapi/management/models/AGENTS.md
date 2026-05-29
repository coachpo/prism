# BACKEND MANAGEMENT MODELS KNOWLEDGE BASE

## OVERVIEW
`management/models/` owns selected-profile model configuration routes under `/api/models*`. It manages model CRUD, ordered access-target attachment, standalone connection references, and model lookups by endpoint for endpoint detail surfaces.

## STRUCTURE
```text
models/
├── service.go    # Service construction and model route mounting
├── routes.go     # Model CRUD and by-endpoint lookup handlers
├── store.go      # Model, relation, access-target, vendor, and strategy SQL
└── types.go      # Model request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Model list/get/create/update/delete: `routes.go`.
- `/models/by-endpoint/{endpoint_id}` and `/models/by-endpoints`: `routes.go`.
- Access-target validation, standalone connection references, vendor links, and strategy links: `routes.go`, `store.go`.

## CONVENTIONS
- Keep model `api_family` as runtime compatibility truth.
- Keep selected-profile model IDs unique inside the profile.
- Keep model load-balance strategy checks in this package, but strategy CRUD in `loadbalance/`.
- Keep connection CRUD in `connections/`, even when model detail responses include attached standalone connections.
- Keep access targets ordered, same-profile, same-family, and acyclic.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When model `api_family` or access-target validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS
- Do not treat vendor metadata as runtime compatibility; use model `api_family`.
- Do not move connection CRUD into model handlers.
- Do not let access targets point at incompatible, missing, or cyclic targets.
