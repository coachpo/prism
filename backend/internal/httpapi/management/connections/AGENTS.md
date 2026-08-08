# BACKEND MANAGEMENT CONNECTIONS KNOWLEDGE BASE

## OVERVIEW
`management/connections/` owns Default-profile connection list or get routes under `/api/connections`, connection reference reads, public mutation rejection surfaces for `/api/connections/*`, owner-scoped private connection routes under `/api/models/{model_config_id}/connections`, and `/api/pricing-templates/*` including JSON import. Model target authoring stays in `management/models/`, while this package keeps reusable endpoint-to-connection binding and pricing-template ownership.

## STRUCTURE
```text
connections/
├── service.go              # Service construction and route mounting
├── routes.go               # Public connection reads, rejection surfaces, and owner-scoped mutations
├── pricing_templates.go    # Pricing-template CRUD and validation
├── pricing_lookup.go       # Pricing-template connection usage lookup
├── store.go                # Profile-scoped connection, endpoint, model, rule, and pricing SQL
└── types.go                # Request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`
- Public connection list/get/reference flows plus rejection surfaces for direct mutations: `routes.go`
- Owner-scoped create/update/delete, legacy priority read compatibility, pricing-template assignment, and inline endpoint creation helpers: `routes.go`, `store.go`
- Pricing-template CRUD, JSON import, connection assignment, and usage lookup: `pricing_templates.go`, `pricing_lookup.go`
- Model target CRUD and ordering live in the separate model leaf: `../models/AGENTS.md`, `../models/service.go`

## PRICING IMPORT CONTRACT
- `POST /api/pricing-templates/import` imports Default-profile pricing templates only. `X-Profile-Id` may be accepted for compatibility, but the effective scope remains profile id `1`.
- Request shape: `{ "mode": "upsert_by_name" | "create_only", "templates": [pricing-template-create-fields...] }`. Each template uses the normal pricing-template create fields, including `name`, optional `description`, `pricing_unit`, `pricing_currency_code`, and the five price strings.
- Response shape: `{ "created": number, "updated": number, "skipped": string[], "errors": [{ "index": number, "name"?: string, "detail": string }] }`.
- `upsert_by_name` updates existing normalized names and creates missing names. `create_only` creates missing names and returns existing names in `skipped`.
- Invalid mode, unknown JSON fields, duplicate names, or row validation failures return `400`. Row validation is all-or-nothing: any invalid row prevents all creates and updates.
- The management route contract row must keep `invalidates_planning: true` so pricing-template imports refresh runtime planning snapshots.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep pricing templates here, not in a separate management package.
- Keep all reads and writes pinned to Default profile id `1`. `X-Profile-Id` compatibility headers may be accepted, but they are ignored and the store still keeps `profile_id` columns for persistence and lookup.
- Keep public `/api/connections` mutation routes mounted only as owner-scoped rejection surfaces; real connection writes go through `/api/models/{model_config_id}/connections`.
- Keep model target CRUD and ordering on `/api/models/{model_config_id}/targets` in `management/models/`, not here.
- Keep `connections.priority` detached from access-target positions: owner-scoped writes must not copy or persist mixed-list ordering into the legacy column.
- Keep endpoint secrets encrypted at rest through the shared endpoint-domain helpers.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## ANTI-PATTERNS
- Do not move pricing-template CRUD or lookup into a separate management package.
- Do not accept both `endpoint_id` and `endpoint_create` on one connection write.
- Do not reintroduce retired owner-lookup helpers, legacy auxiliary mutation routes, or connection-level ordering moves.
