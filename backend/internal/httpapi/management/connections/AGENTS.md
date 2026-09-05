# Terminal Target and Pricing Guidance

Keep owner-scoped Terminal Target writes under `/api/models/{model_config_id}/connections`; public `/api/connections` mutations remain rejection surfaces. Model Target authoring and the mixed target order belong to [models](../models/AGENTS.md), not the legacy `connections.priority` column.

- Validate new fields in both HTTP-neutral create chains: `CreateOwnerConnection` in `writer.go` and `CreateOwnerScopedConnectionTx` in `composite_create.go`. They do not call each other. Endpoint selection is exactly one of `endpoint_id` or `endpoint_create`.
- `upstream_model_id` uses the shared terminal-target validator: create omission writes the owner's current model id; PATCH omission preserves; null, blank, and over-200-rune values reject. Model rename does not cascade, and copy preserves the source value.
- OpenAI text capability must equal the owner's optional text mode, including nil equality; image capability must cover the owner's image operations. Enforce both dimensions on create, update, composite create, and every destination of a copy, even when disabled. An OpenAI target must declare at least one dimension.
- Custom headers use `custom_header_redaction.go`: reads mask sensitive values and return the redacted-name list; writes restore sentinels only from existing stored values and never persist a sentinel. Custom request parameters use `../../../domain/terminaltarget/` validation, whole-value PATCH replacement, and value-free `field/path/reason/limit` errors.
- Hydrate routing-window child rows in a second batch pass and project their evaluated state through `RoutingScheduleStateFor`. Do not return schedule configuration without that common state projection.
- Owner mutations return authoritative access-target/warning envelopes from the transaction. Batch copies take sorted locks, roll back as a whole, default copies to disabled, and never copy runtime state.

Pricing authoring remains in this package:

- `pricing_templates` holds identity/current revision; typed selector metadata, role-keyed cards, and peak/valley windows belong to their revision tables. Reject malformed or incomplete revision shape on read instead of projecting an empty valid value.
- NULL specialty prices mean unconfigured; explicit `"0"` means configured free. Preserve the distinct list-completeness and setup/archive-readiness predicates in `pricing_setup_readiness.go` and the card validators.
- JSON import uses the schema-3 typed shape and preview-hash/atomic-commit flow in `pricing_template_import.go`. Card roles, thresholds, and window digest participate in identity and invalidation.
- Catalog pricing previews fetch models.dev outside transactions; commits use the confirmed snapshot and one transaction, including target assignment CAS under sorted locks. Preserve drift confirmation, source provenance, live-row offering uniqueness, unchanged-template reuse, and private/no-store headers (`pricing_catalog_*.go`).
- Settings currency migration consumes the bounded owner page in `pricing_list_page.go` and all required card roles. Do not add unbounded active-template scans or substitute legacy evidence for current cards.

Local checks include `routes_test.go`, `pricing_template_shape_test.go`, and `pricing_readiness_test.go`. Composite creation and transactional batch copies are covered in [composite create/copy contract tests](../../../../tests/contract/composite_create_copy_contract_test.go).
