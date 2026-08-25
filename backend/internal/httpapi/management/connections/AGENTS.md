# BACKEND MANAGEMENT CONNECTIONS KNOWLEDGE BASE

## OVERVIEW

`management/connections/` owns Default-profile connection list or get routes under `/api/connections`, connection reference reads, public mutation rejection surfaces for `/api/connections/*`, owner-scoped private connection routes under `/api/models/{model_config_id}/connections`, and `/api/pricing-templates/*` including JSON import and bounded migration pages. Model target authoring stays in `management/models/`, while this package keeps reusable endpoint-to-connection binding and pricing-template ownership.

## STRUCTURE

```text
connections/
├── service.go              # Service construction and route mounting
├── routes.go               # Public connection reads, rejection surfaces, and owner-scoped mutations
├── writer.go               # Shared HTTP-neutral owner-scoped create (`CreateOwnerConnection`)
├── composite_create.go     # Redacted, secret-free composite create input/result shapes
├── copies.go               # Transactional batch copy route and redacted copy summaries
├── access_targets.go       # Flat access-target summary carried in mutation envelopes
├── custom_request_parameters.go  # Presence-aware field parsing and 422 field-error envelope
├── custom_header_redaction.go    # Custom-header masking against the safediag bottom line and write resolution
├── openai_image_dimension.go     # OpenAI target-dimension authoring rules (text equality and image containment)
├── routing_schedule.go     # Routing-schedule field parsing, validation errors, and payload conversion
├── routing_schedule_state.go     # Routing-schedule open/closed/not_evaluated state projection
├── pricing_templates.go    # Pricing-template CRUD and validation
├── pricing_template_store.go     # Pricing-template row shape, queries, revision/mutation ledger, scanners
├── pricing_template_import.go    # Two-phase pricing-template JSON import (preview_hash / commit)
├── pricing_template_catalog.go   # models.dev source-linked price import: preview, atomic commit, double-CAS assignment
├── pricing_template_cards.go     # Card/window hydration and read-shape validation
├── pricing_template_shape.go     # Typed kind/card/window normalization and shape validation
├── pricing_template_write.go     # Immutable revision writes and change detection
├── pricing_template_prices.go    # Pricing-template price value object (canonical decimals, tier, equality)
├── pricing_list_page.go    # Bounded keyset pricing-template owner page
├── pricing_lookup.go       # Pricing-template connection usage lookup
├── pricing_setup_readiness.go    # Pricing/Proxy setup-readiness projection over the route witness
├── store.go                # Profile-scoped connection, endpoint, model, and rule SQL, plus the connection→pricing-template reference guard
├── types.go                # Request and response shapes
└── *_test.go               # Route-level regression coverage
```

## WHERE TO LOOK

- Route list and mount contract: `service.go`
- Public connection list/get/reference flows plus rejection surfaces for direct mutations: `routes.go`
- Owner-scoped create/update/delete, legacy priority read compatibility, pricing-template assignment, and inline endpoint creation helpers: `routes.go`, `store.go`
- `custom_request_parameters` create/update semantics (missing/`null`/`{}` normalize, whole-value PATCH replace, 422 `field`/`path`/`reason`/`limit` envelope): `custom_request_parameters.go`, `routes.go`
- Pricing-template CRUD and validation: `pricing_templates.go`
- Pricing-template persistence (row shape, select query, revision/mutation ledger, row scanners): `pricing_template_store.go`
- Pricing-template card/window read validation: `pricing_template_cards.go`
- Typed authoring and immutable revision change detection: `pricing_template_shape.go`, `pricing_template_write.go`
- Shape, readiness, and digest regression cases: `pricing_template_shape_test.go`, `pricing_readiness_test.go`
- Two-phase pricing-template JSON import (`preview_hash` / commit): `pricing_template_import.go`
- Pricing-template price value object (canonical decimals, tier normalization/equality): `pricing_template_prices.go`
- Pricing-template connection assignment and usage lookup: `pricing_lookup.go`
- Bounded keyset pricing-template owner pages used by Settings migration previews: `pricing_list_page.go`
- Model target CRUD and ordering live in the separate model leaf: `../models/AGENTS.md`, `../models/service.go`

## PRICING IMPORT CONTRACT

- Import is two-phase: `POST /api/pricing-templates/import` is a read-only preview (returns `preview_hash`, `committable`, per-row `action` and `template_kind`); `POST /api/pricing-templates/import/commit` applies the preview atomically and fails closed when the hash no longer matches.
- Default-profile scope only; `X-Profile-Id` may be accepted for compatibility, but the effective scope remains profile id `1`.
- Request shape is schema-versioned and uses the strict typed create shape. `standard` carries one card; `tiered` carries `base_card` plus `tier.card`; `peak_valley` carries `peak_card`, `offpeak_card`, and schedule. Legacy flat prices, provider presets, and kind-specific fields in the wrong branch are rejected. Unit/currency remain server-derived.
- Preview/commit are all-or-nothing per phase, and any kind/card/threshold/window change invalidates the planning snapshot. Card roles and window digest are part of the replay identity.
- Catalog import (`POST /api/pricing-templates/catalog/preview` + `/commit`) is the models.dev source-linked path. Preview builds the plan outside transactions from `internal/domain/modelsdev`, hashes offering + revision + plan + linked-template identity + target CAS columns, and reports stable fail-closed reasons (`reporting_currency_not_usd`, `cost_missing`, `audio_cost_present`, `multiple_tiers`, `tier_not_supported`, `legacy_tier_shape`, `tier_evidence_conflict`, `specialty_shape_mismatch`). Commit runs one transaction against the cached snapshot (never remote I/O), rejects stale revisions/state with 409, requires explicit `confirm_drift` after manual edits, appends an append-only `revision_source='catalog'` revision carrying the ETag, and assigns Terminal Targets through the existing double CAS under sorted row locks with whole-transaction rollback on any conflict.

## CONVENTIONS

- Routing schedules are stored as a `connections` column plus `connection_routing_windows` child rows. Reads must attach the child rows in a second batch pass and project the evaluated state through `RoutingScheduleStateFor`, the single state projection every surface shares; never let a read return configuration without it.
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep pricing templates here, not in a separate management package. `pricing_template_revisions` owns typed selector metadata; `pricing_template_cards` is the sole current price-card source and `pricing_template_windows` is the sole peak/valley window source. `pricing_templates` stores only logical identity and the current revision pointer.
- The list page reports specialty NULLs as `configuration_status=incomplete` with explicit missing component names, while setup readiness and currency archive readiness require only canonical input/output plus valid peak/valley schedule shape. Keep those three predicates aligned with the write contract: NULL specialty prices are valid unconfigured values, and explicit `"0"` is configured free pricing.
- Read hydration fails with a typed shape-unavailable error for an unknown kind, missing required role, zero peak/valley windows, invalid timezone, or digest mismatch; it must not project a broken revision as an empty or positive state.
- The pricing import envelope is schema version 3. The existing impact endpoint is the edit preflight source for current/next revision and Terminal Target references.
- Settings currency migration must consume `pricing_list_page.go` (bounded `limit` + signed cursor + owner snapshot hash) or the explicitly bounded inventory evidence routes; do not add a new unbounded active-template scan to a Settings handler.
- A no-query array response remains only for existing compatibility callers. New migration, import, and UI flows must use the bounded owner page and preserve pending/null pricing evidence instead of fabricating zero or current values. Currency migration carries every required card role; it must never project only a standard/base/offpeak card.
- Keep all reads and writes pinned to Default profile id `1`. `X-Profile-Id` compatibility headers may be accepted, but they are ignored and the store still keeps `profile_id` columns for persistence and lookup.
- Keep public `/api/connections` mutation routes mounted only as owner-scoped rejection surfaces; real connection writes go through `/api/models/{model_config_id}/connections`.
- Keep `custom_request_parameters` validation delegated to the shared `domain/terminaltarget` value; the 422 envelope must carry `field`, `path`, `reason`, and `limit` (when applicable) and must never echo configuration values. Validation failures must not update `updated_at` or trigger successful planning invalidation.
- Keep model target CRUD and ordering on `/api/models/{model_config_id}/targets` in `management/models/`, not here.
- Keep `connections.priority` detached from access-target positions: owner-scoped writes must not copy or persist mixed-list ordering into the legacy column.
- Keep endpoint secrets encrypted at rest through the shared endpoint-domain helpers.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## ANTI-PATTERNS

- Do not move pricing-template CRUD or lookup into a separate management package.
- Do not accept both `endpoint_id` and `endpoint_create` on one connection write.
- Do not reintroduce retired owner-lookup helpers, legacy auxiliary mutation routes, or connection-level ordering moves.

## UX-UPGRADE SURFACES

- Owner-scoped Connection create/update/delete and Access Target mutations return fixed envelopes: `{connection, access_targets, configuration_warnings}` / `{access_targets, configuration_warnings}` / `{deleted, access_targets, configuration_warnings}`. Warnings are computed in-transaction on the proposed final state, are non-persisted, and never echo secrets. Note that `ownerScopedConnectionWarnings` does not run the `modelrouting` analyzer: it only produces OpenAI target-dimension warnings and returns nothing for other families. The analyzer-backed warnings come from the models package.
- `POST /api/models/{model_config_id}/connections/{connection_id}/copies` is the transactional batch copy: all-or-nothing, sorted locks, `enable_copies` default false, redacted `connection_summary` (counts only), per-destination warnings. Runtime state is never copied.
- There are **two** HTTP-neutral owner-scoped create chains: `CreateOwnerConnection` in `writer.go` and `CreateOwnerScopedConnectionTx` in `composite_create.go`. The second does not call the first — it repeats the validation — so every new field must be validated in both, or composite create will silently accept what the plain create path rejects. Shared validators may live in their own file in this package (for example `routing_schedule.go`) rather than physically inside `writer.go`, but never in handlers.
