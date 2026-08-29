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
├── catalog_handlers.go           # Catalog route guards and error envelope boundary
├── catalog_read_projection.go    # Catalog binding projection and auto-match hint
├── catalog_read_routes.go        # Catalog read, candidate, and match-preview routes
├── catalog_remote.go              # Restricted catalog fetch and stale/unavailable errors
├── catalog_binding.go             # Catalog bind matching and atomic bind transaction
├── catalog_refresh.go             # Refresh preview and atomic refresh commit
├── catalog_override.go            # Override field decoding and validation
├── catalog_override_write.go      # Override write transactions
├── catalog_unbind.go               # Catalog unbind transaction
├── export_pi_types.go            # Pi-only source/render wire contracts
├── export_pi_facts.go            # Pi live-candidate matching + persisted-binding fact building (full-ID + Pi API)
├── export_pi_projection.go       # Pi source response projection
├── export_pi_source_route.go     # M3 no-store source HTTP route (outside-TX pi.dev fetch, loads persisted bindings)
├── export_pi_render_route.go     # M3 no-store render HTTP route (no network, digest + binding-matching selections assertion)
├── export_pi_binding_types.go    # model_pi_catalog_bindings storage/wire shapes (7 safe leaves, source/override/effective)
├── export_pi_binding_store.go    # model_pi_catalog_bindings read/upsert/delete SQL
├── export_pi_binding_diff.go     # Refresh-preview field-by-field diff over the 7 safe leaves
├── export_pi_binding_handlers.go # Shared bind/refresh/override helpers, pidev.Model <-> binding-shape conversion
├── export_pi_binding_bind.go     # M2 bind route: candidate resolution + atomic bind transaction
├── export_pi_binding_refresh.go  # M2 refresh preview/commit routes
├── export_pi_binding_override.go # M2 override write/clear + unbind routes
└── *_test.go                     # Store and route regression coverage
```

## WHERE TO LOOK

- Route list and mount contract: `service.go` mounts `GET /api/models/export/source` and `POST /api/models/export/render` as static Pi-only routes, plus `POST .../pi/bind`, `POST .../pi/refresh/preview`, `POST .../pi/refresh/commit`, `PUT .../pi/override`, `DELETE .../pi/override`, `DELETE .../pi`; the old `/api/models/exports/{platform}/*` and the interim `resolve` step are no longer mounted and have no compatibility shell.
- Pi export surface: `export_pi_source_route.go` does one best-effort pi.dev fetch outside any DB transaction and one consistent snapshot, returning live catalog evidence (`pi_candidates`/`candidate_status`) separately from persisted binding evidence (`pi_selected`/`pi_binding_status`); `export_pi_render_route.go` performs no network I/O, reads no live catalog state, and requires the request's `selections` coordinate assertion to exactly match each selected model's `model_pi_catalog_bindings` row, failing `422` on a mismatch or missing binding and `409 export_source_stale` on any digest drift. Both are `M3`, `planning-neutral`, `private, no-store`. `export_pi_facts.go` owns full-ID exact plus final Pi API live-candidate matching (`openai-responses`/`openai-completions`/`anthropic-messages`/`google-generative-ai`) and independently reads the persisted binding per model; a bound coordinate stays render-authoritative even when this request's live fetch fails or no longer lists it. `export_pi_projection.go` owns the Pi source response projection. The pure pricing/digest/origin-based Pi renderer domain lives in `internal/domain/modelexport`; the pi.dev typed catalog transport lives in `internal/domain/pidev` (restricted HTTPS, `4 s` timeout, same-origin redirects, `16 MiB` budget, SHA-256-verified revision, `singleflight`, `LKG`, minimum-version validation, HTML rejection).
- Pi persisted binding surface (`export_pi_binding_*.go`): `export_pi_binding_bind.go` fetches+revision-checks the catalog outside the write transaction, resolves an explicit-or-unique candidate, and upserts inside one; `export_pi_binding_refresh.go` diffs then replaces only the bound row's source fields after a revision guard, re-resolving the coordinate inside the commit transaction so a concurrent rebind cannot smuggle a foreign candidate's values in; `export_pi_binding_override.go` validates and writes/clears per-field overrides through `modelexport.ValidatePiSourceField` and unbinds. `export_pi_binding_store.go` owns the `model_pi_catalog_bindings` SQL (independent of `catalog_store.go`/`model_catalog_bindings` below — the two tables and their Go layers never share code). All six binding routes are `M2`, planning-neutral.
- models.dev catalog surface: `catalog_read_routes.go`, `catalog_binding.go`, `catalog_refresh.go`, `catalog_override.go`, `catalog_override_write.go`, and `catalog_unbind.go` mount `/models/{model_config_id}/catalog*`; `catalog_handlers.go` owns route guards/error envelopes, `catalog_remote.go` owns the restricted fetch, and the client lives in `internal/domain/modelsdev`. Metadata writes are planning-neutral (`none:true` admission specs); remote catalog I/O always happens outside transactions and commits verify the previewed ETag.
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

- Keep models.dev catalog metadata (`catalog_*.go`, `model_catalog_bindings`) and pi.dev catalog binding (`export_pi_binding_*.go`, `model_pi_catalog_bindings`) fully independent: neither table nor Go layer reads, rewrites, or reuses the other, even though both are management-only metadata that never enters the runtime snapshot or participates in api_family/capability/routing decisions. All eight write routes across both surfaces stay declared `runtimeCacheEffect{none: true}`.
- Keep `render` free of every live pi.dev dependency: no network I/O, no `LKG` read, no availability gate. Its only inputs beyond the request body are the DB snapshot and each selected model's persisted `model_pi_catalog_bindings` row; a request's `selections` coordinate can only assert what it believes is bound, never choose or change it. `source` is the only route that touches the live catalog, and only for discovery evidence (`pi_candidates`/`candidate_status`) — never as render authority.
- Keep `source_digest` covering exactly what changes rendered bytes: target version, models/targets/pricing/prism metadata, each selected-eligible model's bound coordinate plus its `catalog_revision`, and its effective (source-with-override) candidate metadata (`SourceFacts.Enrichment`, populated from `buildPiSourceFacts`). Keep the current live pi.dev fetch's own status/revision/candidate evidence tagged `json:"-"` in `backend/internal/domain/modelexport/digest.go` so it never enters the digest: `source`'s live fetch and `render`'s cached-only read are two different paths that need not agree on that transient state, and folding it in would make `render` spuriously `409` after every successful bind.
- Keep pi.dev candidate matching to complete `model_id` case-sensitive exact plus final Pi API compat; a single candidate may auto-apply on bind, multiple always require an explicit `provider_id`/`catalog_model_id` bind with no auto-merge/first/lex/provider-preference. Bind's explicit-coordinate path still requires the coordinate to be a current candidate (`pi_candidate_unknown` otherwise) — it is disambiguation among exact-id matches, never free-form manual binding to an unrelated pi.dev entry.
- Render requires an operator-supplied Prism HTTP(S) origin, supports a trimmed slash-free provider id (default `prism`), and distinguishes an omitted client key slot from one explicitly included, trimmed final-dialog string (including empty). Never derive output URLs from upstream endpoint base URLs or read/decrypt stored endpoint keys. Credential, source, render, and every binding route's error data must stay out of caches through `private, no-store`.
- Source and render expose stable `enrichment_unavailable`, `metadata_incomplete`, and Pi `candidate_*` warning codes. Render failures for `candidate_unselected`/`candidate_invalid` map to `422`; digest staleness maps to `409`; bind/refresh coordinate conflicts map to `422`/`409` per `export_pi_binding_bind.go`/`export_pi_binding_refresh.go`. Never accept unknown client fields merely because they are valid JSON.
- Price truth comes only from current pricing-template revisions across every actually reachable target. Emit `cost` only for one consistent USD/PER_1M five-component shape with reasoning equal to output and lossless Pi tier representation; a null component emits `pricing_component_missing`, explicit `"0"` alone means free, and every failed gate keeps the model while omitting the whole group. Pi supports a single strict positive-threshold tier with any threshold.
- `pi/refresh/commit` replaces only a binding's `source_*` columns; `pi/override` writes survive refreshes, per-field restore writes a null override, and rebinding a different coordinate clears overrides while rebinding the same one keeps them (mirrors the models.dev `catalog_binding.go` convention exactly, but against the independent Pi table).
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX

- When model `api_family` or access-target validation changes, evaluate OpenAI, Anthropic, and Gemini operation compatibility.

## ANTI-PATTERNS

- Do not move owner-scoped private connection route handling into model handlers.
