# BACKEND MANAGEMENT SETTINGS KNOWLEDGE BASE

## OVERVIEW
`management/settings/` owns Prism's management settings routes: instance-scope global log-retention (policy CAS, destructive preflight, sealed manual jobs, owner-drift archive), Default-profile costing + timezone (one CAS, reporting-currency epoch contract), API-family audit three-state policy + storage summary, and the flat settings problem envelope.

## STRUCTURE
```text
settings/
├── routes.go           # Mounted routes and route registration
├── service.go          # Service wiring, CORS snapshot, jobs store
├── problems.go         # Flat problem envelope + code registry (Settings SPEC §4.1)
├── types_v2.go         # Target DTOs (retention settings/preflight/jobs/audit)
├── retention_service.go# log-retention GET/PUT CAS, destructive classifier, preflight,
│                       #   manual job acceptance, owner-drift lineage/archive
├── audit_service.go    # Three-state audit policy CAS + storage summary
├── currency_inventory.go# Pricing-owner migration inventory pages
├── currency_migration_drafts.go # Chunked draft, preview, and commit workflow
├── currency_migration_archive.go# Unused-FX archive-only workflow
├── store.go            # Legacy settings persistence (retention pre-v2 reads)
└── routes_test.go      # Route-level contract coverage
```

## WHERE TO LOOK
- Retention policy/preflight/jobs: `retention_service.go`, `types_v2.go`, `../../../platform/managementjobs/jobs_v2.go`
- Retention source projections: `../../../domain/stats/retention_source.go` (single owner)
- Auth settings v2: `../auth/settings_v2.go` (immutable config versions, readiness, acknowledgements)
- Costing/timezone: `routes.go`, `store.go`, `types_v2.go`
- Currency migration ownership handoff: `currency_inventory.go`, `currency_migration_drafts.go`, `currency_migration_archive.go`, and the bounded Pricing page in `../connections/pricing_list_page.go`
- Frontend settings consumers: `../../../../../frontend/src/pages/settings/`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep costing, timezone, and API-family audit settings pinned to Default profile id `1`; `X-Profile-Id` may be accepted but is ignored. Keep storage `profile_id` columns untouched.
- Keep `/api/settings/audit` as exactly three rows (`openai`, `anthropic`, `gemini`) using the three-state `disabled|metadata_only|body_capture` mode union with full replacement PUT semantics; body capture requires audit enabled.
- Timezone is part of the costing CAS (`GET/PUT /api/settings/costing`); there is no standalone timezone route. Reporting currency has a single active epoch; currency-code change with an existing epoch requires the migration preview/commit flow.
- Currency migrations use bounded owner snapshots, chunked drafts, signed cursors, immutable template/evidence identities, and one atomic active-epoch cutover. `archive_unused_fx` is the only archive path: it records an archive ledger and clears unused FX evidence without changing the active epoch, templates, or source FX authoring.
- Fresh installs keep all four retention fields `NULL`; existing values and legacy rows are classified without silent clamping or cleanup. `actual_coverage` is consumed from the Observe owner materialization cut, including source revision, coverage revision/hash, generation/fence, freshness, and gaps.
- Auth enablement consumes the Proxy owner's counted readiness snapshot and 30-second safe-active horizon; key writes and affected Requests/Audit writers share the DB admission/fence lane before changing or reading the transition proof.
- Log retention: every destructive change (enable `null -> N`, shorten, one-time cleanup) requires a fresh server preflight token plus keyword confirmation; manual job acceptance only seals a durable queued intent (202 / replay), never revokes coverage; final publish advances the domain revocation epoch/floor.
- Keep log-retention settings global and trigger cleanup through the v2 management-jobs executor instead of request-path deletes.
- Keep startup bootstrap config ownership separate; this package does not edit startup files.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not treat global log-retention settings as profile-scoped state.
- Do not run partition cleanup or retention deletes inline in these handlers, and do not accept a manual job without a sealed preflight token.
- Do not recompute a retention floor from policy days or `MIN(created_at)`; consume `LoadRetentionSourceProjection` from the stats domain.
- Do not treat frontend-side settings validation as the source of truth when this package already owns the backend validation and persistence contract.
- Do not mix startup bootstrap, auth-session, or sidecar control-plane behavior into this package.
