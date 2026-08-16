# BACKEND MANAGEMENT CONNECTIONS KNOWLEDGE BASE

## OVERVIEW
`management/connections/` owns Default-profile connection list or get routes under `/api/connections`, connection reference reads, public mutation rejection surfaces for `/api/connections/*`, owner-scoped private connection routes under `/api/models/{model_config_id}/connections`, and `/api/pricing-templates/*` including JSON import and bounded migration pages. Model target authoring stays in `management/models/`, while this package keeps reusable endpoint-to-connection binding and pricing-template ownership.

## STRUCTURE
```text
connections/
├── service.go              # Service construction and route mounting
├── routes.go               # Public connection reads, rejection surfaces, and owner-scoped mutations
├── custom_request_parameters.go  # Presence-aware field parsing and 422 field-error envelope
├── pricing_templates.go    # Pricing-template CRUD and validation
├── pricing_list_page.go    # Bounded keyset pricing-template owner page
├── pricing_lookup.go       # Pricing-template connection usage lookup
├── store.go                # Profile-scoped connection, endpoint, model, rule, and pricing SQL
└── types.go                # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`
- Public connection list/get/reference flows plus rejection surfaces for direct mutations: `routes.go`
- Owner-scoped create/update/delete, legacy priority read compatibility, pricing-template assignment, and inline endpoint creation helpers: `routes.go`, `store.go`
- `custom_request_parameters` create/update semantics (missing/`null`/`{}` normalize, whole-value PATCH replace, 422 `field`/`path`/`reason`/`limit` envelope): `custom_request_parameters.go`, `routes.go`
- Pricing-template CRUD, JSON import, connection assignment, and usage lookup: `pricing_templates.go`, `pricing_lookup.go`
- Bounded keyset pricing-template owner pages used by Settings migration previews: `pricing_list_page.go`
- Model target CRUD and ordering live in the separate model leaf: `../models/AGENTS.md`, `../models/service.go`

## PRICING IMPORT CONTRACT
- Import is two-phase: `POST /api/pricing-templates/import` is a read-only preview (returns `preview_hash`, `committable`, per-row `action`); `POST /api/pricing-templates/import/commit` applies the preview atomically and fails closed with `409 currency_import_stale` when the hash no longer matches the current template set.
- Default-profile scope only; `X-Profile-Id` may be accepted for compatibility, but the effective scope remains profile id `1`.
- Request shape: `{ "mode": "upsert_by_name" | "create_only", "templates": [pricing-template-create-fields...] }`. Each template uses the canonical create fields: `name`, optional `description`, and the five price strings (`input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, `reasoning_price`). `pricing_unit` and `pricing_currency_code` are derived server-side (`PER_1M` and the active reporting currency); supplying them returns `422 unknown_field`. The five price keys must be present explicitly (strict key presence); missing keys return `422`.
- Preview response shape: `{ "mode", "items": [{ "name", "action": "create" | "update" | "no_op" }], "created", "updated", "skipped", "errors", "preview_hash", "committable" }`. Commit response mirrors the counts and drops the hash.
- `upsert_by_name` matches on the canonical `name_identity` (Unicode-trimmed, whitespace-collapsed) and updates existing names; `create_only` returns existing names in `skipped`.
- Invalid mode, unknown JSON fields, duplicate names, or row validation failures return `400`; stale previews return `409`. Preview/commit are both all-or-nothing per phase.
- The management route contract row must keep `invalidates_planning: true` so pricing-template imports refresh runtime planning snapshots.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep pricing templates here, not in a separate management package.
- Settings currency migration must consume `pricing_list_page.go` (bounded `limit` + signed cursor + owner snapshot hash) or the explicitly bounded inventory evidence routes; do not add a new unbounded active-template scan to a Settings handler.
- A no-query array response remains only for existing compatibility callers. New migration, import, and UI flows must use the bounded owner page and preserve pending/null pricing evidence instead of fabricating zero or current values.
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

- Owner-scoped Connection create/update/delete and Access Target mutations return fixed envelopes: `{connection, access_targets, configuration_warnings}` / `{access_targets, configuration_warnings}` / `{deleted, access_targets, configuration_warnings}`. Warnings come from the `modelrouting` analyzer, are computed on the proposed final state in-transaction, are non-persisted and never echo secrets.
- `POST /api/models/{model_config_id}/connections/{connection_id}/copies` is the transactional batch copy: all-or-nothing, sorted locks, `enable_copies` default false, redacted `connection_summary` (counts only), per-destination warnings. Runtime state is never copied.
- The shared writer in `writer.go` (`CreateOwnerConnection`) is the single HTTP-neutral owner-scoped connection create used by both this package and the models composite create; keep validators/inserts there, not in handlers.
