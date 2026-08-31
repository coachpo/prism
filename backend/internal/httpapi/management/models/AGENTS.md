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
├── catalog_types.go              # models.dev binding request/response shapes (incl. CAS fields)
├── catalog_binding_record.go     # models.dev metadata projections, binding record, response projection
├── catalog_store.go              # model_catalog_bindings SQL: plain + FOR UPDATE reads, bind/source/override writes, delete, CAS token
├── catalog_binding_diff.go       # refresh-preview field-by-field diff over the 18 metadata fields
├── pi_catalog_types.go           # common Pi catalog/candidate/search wires + single-model read shapes
├── pi_catalog_remote.go          # shared fresh/stale/unavailable read resolver (outside DB transactions)
├── pi_catalog_projection.go      # shared live-candidate/binding-health projections + single-model read projection
├── pi_catalog_read_route.go      # M2 no-store GET /models/{id}/pi (read-only, planning-neutral)
├── catalog_handlers.go           # Catalog route guards and error envelope boundary
├── catalog_read_projection.go    # Catalog binding projection and auto-match hint
├── catalog_read_routes.go        # Catalog read, candidate, and match-preview routes
├── catalog_remote.go              # Restricted catalog fetch and stale/unavailable errors
├── catalog_binding.go             # Catalog bind matching and atomic bind transaction
├── catalog_refresh.go             # Refresh preview and atomic refresh commit
├── catalog_override.go            # Override field decoding and validation
├── catalog_override_write.go      # Override write transactions
├── catalog_unbind.go               # Catalog unbind transaction
├── export_pi_types.go            # Pi-only aggregate source/render wire contracts
├── export_pi_facts.go            # Pi live-candidate matching + persisted-binding fact building (full-ID + Pi API)
├── export_pi_projection.go       # Pi source response projection
├── export_pi_source_route.go     # M3 no-store source HTTP route (outside-TX pi.dev fetch, loads persisted bindings)
├── export_pi_render_route.go     # M3 no-store render HTTP route (no network, digest + binding-matching selections assertion)
├── export_pi_binding_types.go    # model_pi_catalog_bindings storage/wire shapes (7 safe leaves, source/override/effective)
├── export_pi_binding_store.go    # model_pi_catalog_bindings read/upsert/delete SQL
├── export_pi_binding_diff.go     # Refresh-preview field-by-field diff over the 7 safe leaves
├── export_pi_binding_handlers.go # Shared bind/refresh/override helpers, pidev.Model <-> binding-shape conversion
├── export_pi_catalog_search.go   # M2 no-store bounded pi.dev model-id directory search (read-only, zero writes)
├── export_pi_binding_bind.go     # M2 bind route: candidate/coordinate resolution + atomic bind transaction
├── export_pi_binding_refresh.go  # M2 refresh preview/commit routes
├── export_pi_binding_override.go # M2 override write/clear + unbind routes
└── *_test.go                     # Store and route regression coverage
```

## WHERE TO LOOK

- Route list and mount contract: `service.go` mounts the literal Pi-only routes `GET /api/models/exports/pi/source` and `POST /api/models/exports/pi/render`, plus `GET /api/models/{model_config_id}/pi`, `POST .../pi/bind`, `POST .../pi/search`, `POST .../pi/refresh/preview`, `POST .../pi/refresh/commit`, `PUT .../pi/override`, `DELETE .../pi/override`, `DELETE .../pi`; no variable platform segment, singular `/models/export/*` alias, or interim `resolve` step is mounted.
- Pi export surface: `export_pi_source_route.go` does one best-effort pi.dev fetch outside any DB transaction and one consistent snapshot, returning live catalog evidence (`pi_candidates`/`candidate_status`) separately from persisted binding evidence (`pi_selected`/`pi_binding_status`/`pi_binding_renderable`, source/override/effective metadata, and dropped paths); `export_pi_render_route.go` performs no network I/O, reads no live/LKG catalog state, and requires exactly one request coordinate assertion matching each selected model's compatible binding, failing `422` on missing/extra/mismatched/incompatible assertions and `409 export_source_stale` on digest drift. Both are `M3`, planning-neutral, `private, no-store`. `export_pi_facts.go` owns full-ID exact plus final Pi API live-candidate matching (`openai-responses`/`openai-completions`/`anthropic-messages`/`google-generative-ai`) and independently reads the persisted binding per model; a compatible frozen coordinate stays render-authoritative through live catalog failure or drift. Renderability compares the binding's frozen **Prism identity snapshot** (`prism_model_id_at_bind`) against the current model id plus final Pi API — never the directory model id — so a deliberate cross-directory bind renders while a later Prism rename fails closed. `pi_api` publishes the model's final Pi API so the UI never re-derives it. `export_pi_projection.go` owns the source response. The pure pricing/digest/origin-based renderer lives in `internal/domain/modelexport`; `internal/domain/pidev` owns restricted HTTPS, strict JSON, SHA-256 revision verification, ETag/304, `singleflight`, LKG, version checks, and API-specific compat sanitization.
- Pi persisted binding surface (`export_pi_binding_*.go`): `export_pi_binding_bind.go` fetches+revision-checks the catalog outside the write transaction, resolves an explicit-or-unique candidate, and freezes it inside one; rebinding the same coordinate with the same confirmed Prism identity is a true no-op, rebinding the same coordinate after a Prism rename preserves frozen source, overrides, revision, `fetched_at`, dropped evidence and `bind_source` while advancing the identity snapshot and `updated_at` CAS token, and changing coordinates clears overrides. Every bind requires `expected_catalog_revision` plus `expected_prism_model_id`/`expected_pi_api`, all re-verified under row lock; every rejection writes nothing.
- Pi directory discovery (`export_pi_catalog_search.go`): `POST .../pi/search` is the single bounded, model-id-only, case-insensitive literal directory search for every model whose final Pi API is determinable, including `not_in_catalog`/`api_mismatch` ones. It reads the trusted catalog through `pidev` (fresh, or last-known-good labelled `stale`), restricts hits to exactly the model's current final Pi API, ranks exact/prefix/substring then provider/id over the whole same-API hit set before cutting the `offset` window, defaults to 20 and clamps to 100, echoes `offset` plus both `fetched_at`/`checked_at` stamps, never selects, and writes nothing — the browser never contacts pi.dev. `export_pi_binding_refresh.go` previews the seven source fields plus sorted dropped-field paths, then replaces them only when catalog revision, coordinate, API, and `binding_updated_at` all still match under row lock. `export_pi_binding_override.go` validates all seven sparse per-field writes through the bound API's exact Pi schema and unbinds. `export_pi_binding_store.go` owns the `model_pi_catalog_bindings` SQL (independent of `catalog_store.go`/`model_catalog_bindings` below — the two tables and their Go layers never share code). The search route plus six binding mutation routes are `M2`, planning-neutral.
- Single-model Pi read (`pi_catalog_read_route.go` + `pi_catalog_types.go` + `pi_catalog_remote.go` + `pi_catalog_projection.go`): `GET /api/models/{model_config_id}/pi` is `M2`, Default-profile, planning-neutral, `private, no-store`. It runs one best-effort shared `piCatalogForRead` fetch outside any transaction, then one profile-scoped read-only `REPEATABLE READ` transaction over the model row and its `model_pi_catalog_bindings` row, then the pure projection. It reports the model identity (`pi_api` is Prism's own mapping), one catalog evidence block (fresh/stale/unavailable with `fetched_at`/`checked_at`), live exact-candidate evidence, and the persisted binding truth (status, renderability, coordinate, frozen `prism_model_id_at_bind`, revision, stamps, source/override/effective, dropped paths). A catalog outage degrades only the live evidence block and never manufactures an unbound binding. It never loads export targets, pricing plans, source digests, credentials, render results, or any runtime graph. The status/match helpers in `pi_catalog_projection.go` are shared with `export_pi_facts.go` so export and the single-model read compute identical axes without sharing the full export assembly.
- models.dev catalog surface: `catalog_read_routes.go`, `catalog_binding.go`, `catalog_refresh.go`, `catalog_override.go`, `catalog_override_write.go`, and `catalog_unbind.go` mount `/models/{model_config_id}/catalog*`; `catalog_handlers.go` owns route guards/error envelopes, `catalog_remote.go` owns the restricted fetch, and the client lives in `internal/domain/modelsdev`. Every write takes the model row lock first, then the binding row lock (`SELECT ... FOR UPDATE OF bindings` in `catalog_store.go`). Bind re-verifies the caller-confirmed Prism identity and catalog ETag; refresh re-verifies its catalog ETag plus the previewed binding coordinate/token; sparse override verifies the confirmed coordinate before merging over the locked row; bulk override clear and unbind verify the confirmed coordinate/token snapshot. Conflicts return the stable `409 models_dev_model_changed` / `409 models_dev_binding_stale` / `409 models_dev_catalog_stale` with zero writes. Refresh commits are source-only UPDATEs (overrides never rewritten from a stale snapshot), unbind stays idempotent once the row is gone, and the monotonic `nextCatalogBindingUpdatedAt` helper advances the CAS token even under a fixed clock. Candidates pages publish `catalog_revision`/`fetched_at` snapshot evidence without fabricating a freshness enum. Metadata writes are planning-neutral (`none:true` admission specs); remote catalog I/O always happens outside transactions. All `/catalog*` responses and the model-detail GET (which still embeds the legacy models.dev catalog field) are `private, no-store`.
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

- Keep models.dev catalog metadata (`catalog_*.go`, `model_catalog_bindings`) and pi.dev catalog binding (`export_pi_binding_*.go`, `model_pi_catalog_bindings`) fully independent: neither table nor Go layer reads, rewrites, or reuses the other, even though both are management-only metadata that never enters the runtime snapshot or participates in api_family/capability/routing decisions. Every write route across both surfaces stays declared `runtimeCacheEffect{none: true}`.
- Keep `render` free of every live pi.dev dependency: no network I/O, no `LKG` read, no availability gate. Its only inputs beyond the request body are the DB snapshot and each selected model's persisted `model_pi_catalog_bindings` row; a request's `selections` coordinate can only assert what it believes is bound, never choose or change it. `source` and `pi/search` are the read-only live-catalog consumers: source publishes default exact-candidate evidence, while search publishes bounded explicit-discovery evidence; neither is render authority.
- Keep `source_digest` covering the target version, models/targets/pricing/Prism metadata, and every persisted binding's full directory coordinate, frozen Prism identity snapshot, `catalog_revision`, effective Pi template, and dropped-field evidence (`ModelFact.PiSelected` plus `ModelFact.PiTemplate`). Keep live source/search catalog outcomes excluded; `render` reads no live or LKG catalog at all, so transport health must not create a spurious `409`.
- Keep default pi.dev candidate matching to complete `model_id` case-sensitive exact plus final Pi API compat; a single candidate may auto-apply, multiple always require an explicit choice with no auto-merge/first/lex/provider-preference. The explicit-coordinate bind path may name any real directory model id, but must re-resolve it in the expected trusted revision and require its API to equal the current final Pi API exactly.
- Render requires an operator-supplied Prism HTTP(S) origin, supports a trimmed slash-free provider id (default `prism`), and distinguishes an omitted client key slot from one explicitly included, trimmed, non-empty final-dialog string. Pi 0.84.3 cannot load an empty `apiKey`, which returns `422 credential_api_key_required`. Never derive output URLs from upstream endpoint base URLs or read/decrypt stored endpoint keys. Credential, source, render, and every binding route's error data must stay out of caches through `private, no-store`.
- Source/render warnings include stable `metadata_incomplete` and `pi_source_fields_dropped`; live matching uses the explicit `candidate_status` enum instead of warning codes. Render returns `422` with exact `candidate_unselected` or `candidate_invalid` codes and `model_config_id`; digest staleness returns `409 export_source_stale`. Bind/refresh coordinate conflicts map to `422`/`409` per their owning files. Never accept unknown client fields merely because they are valid JSON.
- Price truth comes only from current pricing-template revisions across every actually reachable target. Emit `cost` only for one consistent USD/PER_1M five-component shape with reasoning equal to output and lossless Pi tier representation; a null component emits `pricing_component_missing`, explicit `"0"` alone means free, and every failed gate keeps the model while omitting the whole group. Pi supports a single strict positive-threshold tier with any threshold.
- `pi/refresh/commit` replaces only a binding's `source_*` columns and dropped-field evidence after its full preview CAS; `pi/override` writes survive refreshes, explicit null restores one field to source, and rebinding a different coordinate clears overrides. Same-coordinate/same-identity bind is a no-op; same-coordinate identity reconfirm advances only identity/`updated_at`, so refresh remains the only path that advances its frozen template.
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
- Route-witness readiness receives the model-bound operation projection from `httpapi/runtime/operations.go`, covers OpenAI/Anthropic/Gemini without a second catalog, and preserves actual Endpoint identity, lowercase coverage, and routing-schedule qualifiers.
