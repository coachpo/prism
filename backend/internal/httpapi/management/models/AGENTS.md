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
├── catalog_types.go              # models.dev binding request/response shapes
├── catalog_store.go              # model_catalog_bindings read/upsert and source diff helpers
├── catalog_handlers.go           # Catalog bind/match-preview/refresh/override/candidates routes
├── export_types.go               # Pi/OpenCode source/render wire contracts
├── export_store.go               # consistent model/routing/current-price export snapshot
├── export_facts.go               # DB/catalog rows projected into pure export facts
├── export_handlers.go            # M3 no-store source/render handlers and server-owned replay
└── *_test.go                     # Store and route regression coverage
```

## WHERE TO LOOK

- Route list and mount contract: `service.go`.
- Client model-config export surface: `export_handlers.go` mounts `/models/exports/{platform}/source` (consistent snapshot plus clock-free `source_digest`) and `/models/exports/{platform}/render` (fresh database facts matched against current-catalog and no-enrichment digest candidates, no network I/O and no use of request-carried catalog data). `export_store.go` reads models, reachable Terminal Targets, current pricing revisions, and catalog bindings in one transaction; `export_facts.go` projects rows into domain facts; `export_types.go` owns the wire shapes. Both M3 routes, including errors, are `private, no-store`, planning-neutral, and non-persistent. The pure merge/pricing/digest/origin-based renderer domain lives in `internal/domain/modelexport`.
- models.dev catalog surface: `catalog_handlers.go` mounts `/models/{model_config_id}/catalog*`; the restricted client lives in `internal/domain/modelsdev` and is injected through `Options.Catalog`. Metadata writes are planning-neutral (`none:true` admission specs); remote catalog I/O always happens outside transactions and commits verify the previewed ETag.
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

- Keep models.dev catalog metadata management-only: it never enters the runtime snapshot, never participates in api_family/capability/routing decisions, never changes `display_name`, and its write routes must stay declared `runtimeCacheEffect{none: true}`.
- Keep the export surface read-only against runtime state: no migrations, persisted export state, digest cache, or planning invalidation. Source may best-effort refresh models.dev outside its database transaction, but render never performs network I/O or accepts a request-carried candidate as truth; it recomputes only the current catalog-backed and independently derived no-enrichment candidates, with no exact digest match returning `export_source_stale`.
- Render requires an operator-supplied Prism HTTP(S) origin, supports a trimmed slash-free provider id (default `prism`), and distinguishes an omitted client key slot from one explicitly included, trimmed final-dialog string (including empty). Never derive output URLs from upstream endpoint base URLs or read/decrypt stored endpoint keys. Credential, source, and error data must stay out of caches through `private, no-store`.
- Source and render expose stable `enrichment_unavailable`, `metadata_incomplete`, and platform-specific metadata warning codes. Manual and completed-document target-schema failures map to `422 target_schema_invalid`; never accept unknown client fields merely because they are valid JSON.
- Price truth comes only from current pricing-template revisions across every actually reachable target. Emit `cost` only for one consistent USD/PER_1M five-component shape with reasoning equal to output and lossless target representation; a null component emits `pricing_component_missing`, explicit `"0"` alone means free, and every failed gate keeps the model while omitting the whole group. Pi supports a strict positive-threshold tier; OpenCode supports flat or exactly 200,000 tokens as `context_over_200k`.
- Refreshes replace only `source_*` columns; manual overrides survive refreshes, per-field restore writes a null override, and rebinding to a different offering clears overrides while same-offering rebinds keep them.
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
