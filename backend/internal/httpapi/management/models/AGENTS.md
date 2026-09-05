# Model Guidance

Keep model CRUD and graph authoring here; private Terminal Target writes stay in [connections](../connections/AGENTS.md), and strategy CRUD stays in `../loadbalance/`.

- `direct_request_enabled` is the sole direct-entry flag: omitted create defaults true, omitted update preserves, explicit null rejects. Non-entry models remain recursive targets; preserve incoming-reference counts and the unreferenced warning in `direct_request.go`.
- Validate ordered targets as same-profile, same-family, and acyclic with the existing depth limit. Model Targets require exact ids and the flat `position/is_enabled` shape; reject retired weight/priority payloads.
- Preserve strict OpenAI text-mode equality across direct, inbound, and disabled relations. `graph_integrity.go` blocks mode changes that break existing relations; image-operation containment is an independent gate.
- Composite create includes the initial Terminal Target in the same transaction and uses the connections package's presence-aware validation. Warnings describe the final proposed graph and remain structured, non-persisted codes.
- Routing diagnostics and readiness consume `../../../domain/modelrouting/` and the runtime registry's read-only route-witness projection. Do not create a second operation catalog or make a diagnostic read invalidate runtime planning.

Catalog and export boundaries:

- Keep models.dev `catalog_*.go` / `model_catalog_bindings` independent from pi.dev `export_pi_binding_*.go` / `model_pi_catalog_bindings`. Both are management metadata; their writes remain planning-neutral and their responses private/no-store.
- Fetch remote catalogs outside transactions. models.dev writes lock model then binding and recheck confirmed identity, coordinate, catalog revision, and binding token as applicable; refresh changes source fields without overwriting concurrent overrides.
- Pi bind verifies trusted catalog revision plus current Prism id/final Pi API under lock. Default discovery matches full id case-sensitively and requires a unique candidate; explicit coordinates may cross directory model ids only when the final Pi API matches. Search is bounded discovery and never selects or binds.
- Pi refresh replaces source fields only after the full preview CAS. Sparse overrides survive refresh, null restores a source field, coordinate changes clear overrides, and same-coordinate identity reconfirmation does not refresh frozen source data.
- `export_pi_render_route.go` reads only the database snapshot and persisted Pi bindings. Request selections assert existing bindings; they cannot choose them. Keep network/LKG state out of render and `source_digest`; compatible frozen bindings remain usable through catalog outage.
- Renderability compares the frozen Prism identity and final Pi API, not directory id equality. The single-model Pi read keeps live discovery and persisted binding evidence separate and must not load the full export graph.
- Export uses the operator's Prism origin and optional non-empty typed client key, never upstream endpoint URLs or stored keys. Pricing comes from current reachable-target revisions; a failed price gate omits the whole cost group without dropping the model or fabricating zero.

Use `routes_test.go` and `export_pi_binding_boundaries_test.go` for local seams. Database-backed graph, catalog CAS, and export behavior belong in [model](../../../../tests/contract/model_contract_test.go), [catalog concurrency](../../../../tests/contract/catalog_concurrency_contract_test.go), and [export](../../../../tests/contract/model_export_contract_test.go) contract tests.
