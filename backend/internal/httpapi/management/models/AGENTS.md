# BACKEND MANAGEMENT MODELS KNOWLEDGE BASE

## OVERVIEW
`management/models/` owns model configuration routes under `/api/models*` pinned to Default profile id `1`. It manages model CRUD, public same-family model-target authoring with exact `target_model_id`, `position`, and `is_enabled`, private connection target preservation/mutation, obsolete nested create/update field rejection, and model lookups by endpoint for endpoint detail surfaces. `X-Profile-Id` may be accepted but is ignored; storage `profile_id` columns remain.

## STRUCTURE
```text
models/
├── service.go                    # Model service lifecycle
├── routes.go                     # Model CRUD handlers
├── access_target_handlers.go     # Access-target HTTP handlers
├── access_target_ordering.go     # Ordered access-target editing
├── access_target_tx_steps.go     # Access-target transaction steps
├── composite_create.go           # Composite model creation
├── endpoint_model_lookup.go      # Endpoint model lookup
├── model_request_validation.go   # Model request validation
├── model_request_decoding.go     # Model request decoding
├── model_routing_validation.go   # Model routing validation
├── store.go                      # pgx type boundary
├── model_rows.go                 # Model SQL rows
├── model_queries.go              # Model query composition
├── strategy_queries.go           # Strategy read queries
├── reachability_queries.go       # Reachability queries
├── access_target_queries.go      # Access-target read queries
├── connection_target_rows.go    # Connection target rows
├── graph_integrity.go            # Access-target graph integrity
├── access_target_rows.go         # Ordered access-target rows
├── access_target_write.go        # Access-target writes
├── model_response_projection.go  # Model response projection
├── model_request_projection.go   # Model request projection
├── types.go                      # Model request and response shapes
├── routing_diagnostics.go        # Static routing-diagnostics endpoint and list routing_summary
├── route_readiness.go            # Route-witness readiness projection for the model surfaces
└── *_test.go                     # Store and route regression coverage
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Model list/get/create/update/delete handlers: `routes.go`.
- Access-target HTTP handlers: `access_target_handlers.go`.
- Ordered access-target editing: `access_target_ordering.go`.
- Access-target transaction steps and private-target preservation: `access_target_tx_steps.go`, `access_target_write.go`.
- Composite model creation: `composite_create.go`.
- `/models/by-endpoint/{endpoint_id}` and `/models/by-endpoints`: `endpoint_model_lookup.go`.
- Model request validation and decoding: `model_request_validation.go`, `model_request_decoding.go`.
- Model routing validation and graph rules: `model_routing_validation.go`, `graph_integrity.go`.
- Model rows, query composition, reachability, and strategy reads: `model_rows.go`, `model_queries.go`, `reachability_queries.go`, `strategy_queries.go`.
- Access-target rows, connection rows, and read queries: `access_target_rows.go`, `connection_target_rows.go`, `access_target_queries.go`.
- Model request/response projections: `model_request_projection.go`, `model_response_projection.go`.
- pgx query boundary conversions: `store.go`.
- Model request/response fields and model-target metadata: `types.go`.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep model `api_family` as runtime compatibility truth.
- Keep model IDs unique inside Default profile id `1`.
- Keep model load-balance strategy checks in this package, but strategy CRUD in `loadbalance/`.
- Keep owner-scoped private connection routes in `connections/`, even when model detail responses include owned private connections.
- Keep access targets ordered, same-profile, same-family, and acyclic.
- Keep public model targets requiring exact `target_model_id`, `position`, and `is_enabled`; obsolete `weight` and `target_priority` payload keys must reject, while internal connection-owner targets keep the same flat ordered shape.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When model `api_family` or access-target validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS
- Do not move owner-scoped private connection route handling into model handlers.
- Do not let access targets point at incompatible, missing, or cyclic target models.
- Do not reintroduce exact-facade, regex matching, capability-metadata expansion, or frontend-only target authoring from this package.

## UX-UPGRADE SURFACES

- `POST /api/models` supports composite create via `initial_terminal_target` (endpoint_id XOR inline endpoint_create; capability derives from the owner accepted format; enabled defaults; `model_initial_target_inactive` / `model_no_enabled_targets` hard errors; single transaction with full rollback). Responses are `{model, configuration_warnings}` envelopes.
- `GET /api/models/{model_config_id}/routing-diagnostics` serves the `modelrouting` analyzer over the committed graph; it is read-only and never invalidates planning. `GET /api/models` embeds a compact `routing_summary` per model computed in one bounded batch.
- Routing-relevant mutations attach `configuration_warnings` from `modelMutationWarnings`; the frontend keys presentation off warning codes, never message text.
