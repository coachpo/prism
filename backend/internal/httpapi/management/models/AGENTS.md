# BACKEND MANAGEMENT MODELS KNOWLEDGE BASE

## OVERVIEW
`management/models/` owns selected-profile model configuration routes under `/api/models*`. It manages model CRUD, Release 1 exact-facade model fields (`facade_enabled`, `facade_selection_policy`, `facade_fallback_policy`), explicit context overflow promotion targets (`context_overflow_promotion_target_id`), public same-family model-target authoring with exact `target_model_id`, `position`, and `is_enabled`, private connection target preservation/mutation, obsolete access-target field rejection, and model lookups by endpoint for endpoint detail surfaces.

## STRUCTURE
```text
models/
├── service.go    # Service construction, model route mounting, promotion-target validation seam
├── routes.go     # Model CRUD, access targets, promotion target validation, by-endpoint lookups
├── store.go      # Model, relation, access-target, vendor, strategy, and promotion-target SQL
├── types.go      # Model request and response shapes
└── *_test.go     # Store, route, and promotion-target regression coverage
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Model list/get/create/update/delete and Release 1 exact-facade validation: `routes.go`.
- Context overflow promotion target validation and persistence: `service.go`, `routes.go`, `store.go`, `promotion_target_test.go`, `store_test.go`.
- `/models/by-endpoint/{endpoint_id}` and `/models/by-endpoints`: `routes.go`.
- Access-target validation, exact `target_model_id` model-target metadata (`position`, `is_enabled`), obsolete `weight` / `target_priority` rejection, nested-facade rejection, private connection target preservation, vendor links, and strategy links: `routes.go`, `store.go`.
- Model request/response fields for exact-facade authoring, context overflow promotion target IDs, and model-target metadata: `types.go`.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep model `api_family` as runtime compatibility truth.
- Keep selected-profile model IDs unique inside the profile.
- Keep Release 1 facade authoring exact-ID only. This package owns backend CRUD for `facade_enabled`, `facade_selection_policy`, and `facade_fallback_policy`; do not add regex matcher fields, capability-metadata expansion, or frontend-only authoring assumptions here.
- Keep context overflow promotion targets exact-ID, same-profile, same `api_family`, enabled, non-facade, non-self, acyclic explicit-chain, max-depth-3, and no-same-terminal-loop. Runtime planning owns terminal-fit decisions.
- Keep model load-balance strategy checks in this package, but strategy CRUD in `loadbalance/`.
- Keep owner-scoped private connection routes in `connections/`, even when model detail responses include owned private connections.
- Keep access targets ordered, same-profile, same-family, acyclic, and nested-facade-safe.
- Keep public model targets requiring exact `target_model_id`, `position`, and `is_enabled`; obsolete `weight` and `target_priority` payload keys must reject, while internal connection-owner targets keep the same flat ordered shape.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When model `api_family` or access-target validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS
- Do not move owner-scoped private connection route handling into model handlers.
- Do not let access targets point at incompatible, missing, cyclic, or facade-enabled target models.
- Do not accept context overflow promotion targets that bypass exact model IDs, same-profile ownership, same `api_family`, enabled non-facade non-self targets, acyclic explicit chains, max depth 3, or no-same-terminal-loop validation.
- Do not claim Release 1 regex matching, capability-metadata facade expansion, or frontend facade authoring from this package.
