# BACKEND MANAGEMENT MODELS KNOWLEDGE BASE

## OVERVIEW
`management/models/` owns selected-profile model configuration routes under `/api/models*`. It manages model CRUD, Release 1 exact-facade model fields (`facade_enabled`, `facade_selection_policy`, `facade_fallback_policy`), public same-family model-target authoring with exact `target_model_id` plus optional `weight` / `target_priority` that default to `1` / `position` when omitted, private connection target preservation/mutation, and model lookups by endpoint for endpoint detail surfaces.

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
- Model list/get/create/update/delete and Release 1 exact-facade validation: `routes.go`.
- `/models/by-endpoint/{endpoint_id}` and `/models/by-endpoints`: `routes.go`.
- Access-target validation, exact `target_model_id` model-target metadata (`weight`, `target_priority`), nested-facade rejection, private connection target preservation, vendor links, and strategy links: `routes.go`, `store.go`.
- Model request/response fields for exact-facade authoring and model-target metadata: `types.go`.

## CONVENTIONS
- Keep model `api_family` as runtime compatibility truth.
- Keep selected-profile model IDs unique inside the profile.
- Keep Release 1 facade authoring exact-ID only. This package owns backend CRUD for `facade_enabled`, `facade_selection_policy`, and `facade_fallback_policy`; do not add regex matcher fields, capability-metadata expansion, or frontend-only authoring assumptions here.
- Keep model load-balance strategy checks in this package, but strategy CRUD in `loadbalance/`.
- Keep owner-scoped private connection routes in `connections/`, even when model detail responses include owned private connections.
- Keep access targets ordered, same-profile, same-family, acyclic, and nested-facade-safe.
- Keep public model targets requiring exact `target_model_id`; authored `weight` and `target_priority` stay optional and default to `1` / `position` when omitted, while internal connection-owner targets continue to omit weight/priority metadata.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When model `api_family` or access-target validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS
- Do not treat vendor metadata as runtime compatibility; use model `api_family`.
- Do not move owner-scoped private connection route handling into model handlers.
- Do not let access targets point at incompatible, missing, cyclic, or facade-enabled target models.
- Do not claim Release 1 regex matching, capability-metadata facade expansion, or frontend facade authoring from this package.
