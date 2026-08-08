# BACKEND MANAGEMENT MODELS KNOWLEDGE BASE

## OVERVIEW
`management/models/` owns model configuration routes under `/api/models*` pinned to Default profile id `1`. It manages model CRUD, public same-family model-target authoring with exact `target_model_id`, `position`, and `is_enabled`, private connection target preservation/mutation, obsolete nested create/update field rejection, and model lookups by endpoint for endpoint detail surfaces. `X-Profile-Id` may be accepted but is ignored; storage `profile_id` columns remain.

## STRUCTURE
```text
models/
├── service.go    # Service construction and model route mounting
├── routes.go     # Model CRUD, access targets, and by-endpoint lookups
├── store.go      # Model, relation, access-target, vendor, and strategy SQL
├── types.go      # Model request and response shapes
└── *_test.go     # Store and route regression coverage
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Model list/get/create/update/delete and obsolete create/update payload rejection: `routes.go`.
- `/models/by-endpoint/{endpoint_id}` and `/models/by-endpoints`: `routes.go`.
- Access-target validation, exact `target_model_id` model-target metadata (`position`, `is_enabled`), obsolete `weight` / `target_priority` rejection, private connection target preservation, vendor links, and strategy links: `routes.go`, `store.go`.
- Model request/response fields and model-target metadata: `types.go`.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep model `api_family` as runtime compatibility truth.
- Keep model IDs unique inside Default profile id `1`.
- Keep model load-balance strategy checks in this package, but strategy CRUD in `loadbalance/`.
- Keep owner-scoped private connection routes in `connections/`, even when model detail responses include owned private connections.
- Keep access targets ordered, same-profile, same-family, and acyclic. `position` is one global mixed sequence shared by model targets and connection-owner targets: creates append to the global tail (or insert at a global index), `PATCH .../position` moves within the complete mixed list, delete compacts across both types, and enable/disable never moves a row.
- Keep public model targets requiring exact `target_model_id`, `position`, and `is_enabled`; obsolete `weight` and `target_priority` payload keys must reject, while internal connection-owner targets keep the same flat ordered shape. `connections.priority` is never written back from access-target order.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When model `api_family` or access-target validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS
- Do not move owner-scoped private connection route handling into model handlers.
- Do not let access targets point at incompatible, missing, or cyclic target models.
- Do not reintroduce exact-facade, regex matching, capability-metadata expansion, frontend-only target authoring, per-type position sequences, or a second ordering API from this package.
