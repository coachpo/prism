# BACKEND MANAGEMENT SETTINGS KNOWLEDGE BASE

## OVERVIEW

`management/settings/` owns Prism's management settings routes: instance-scope global log-retention (policy CAS, destructive preflight, sealed manual jobs, owner-drift archive), Default-profile costing + timezone (one CAS, reporting-currency epoch contract), API-family audit three-state policy + storage summary, and the flat settings problem envelope.

## STRUCTURE

```text
settings/
├── routes.go                         # Settings route dispatch
├── service.go                        # Settings service lifecycle
├── problems.go                       # Settings problem registry
├── settings_contracts.go                       # Retention, preflight, audit, and settings wire contracts
├── types.go                          # Settings wire types
├── retention_service.go              # Retention service type
├── retention_row.go                  # Retention persistence rows
├── retention_policy_classifier.go   # Retention policy classification
├── retention_cutoff_format.go       # Retention cutoff formatting
├── settings_read_savepoint.go        # Settings read savepoints
├── retention_settings_projection.go  # Retention settings projection
├── retention_owner_snapshot.go       # Retention owner snapshot
├── retention_impact_estimate.go      # Retention impact estimate
├── retention_impact_preview.go       # Retention impact preview
├── retention_preflight.go            # Retention destructive preflight
├── retention_policy_routes.go        # Retention policy routes
├── retention_policy_resource.go      # Retention policy resources
├── retention_owner_drift.go          # Retention owner drift
├── retention_manual_job.go           # Retention manual job
├── settings_operations.go            # Settings mutation operations
├── settings_request_identity.go      # Settings request identity
├── settings_conflict_errors.go      # Settings conflict errors
├── audit_service.go                  # Audit policy service
├── currency_inventory.go             # Currency migration inventory
├── currency_migration_drafts.go     # Currency migration wire types
├── currency_migration_draft_routes.go # Currency draft routes
├── currency_migration_commit_routes.go # Currency commit routes
├── currency_migration_draft_store.go  # Currency draft persistence
├── currency_migration_preview.go     # Currency migration preview
├── currency_migration_cutover.go     # Currency migration cutover
├── currency_migration_cursor.go      # Currency draft cursors
├── currency_migration_values.go      # Currency migration value validation
├── currency_migration_identity.go    # Currency migration identity
├── currency_migration_errors.go      # Currency migration errors
├── currency_migration_pages.go       # Currency migration pages
├── currency_migration_archive.go     # Unused-FX archive workflow
├── settings_values.go                # Settings scalar values
├── store.go                          # Legacy settings persistence
└── routes_test.go                    # Route-level contract coverage
```

## WHERE TO LOOK

- Retention policy classification and persistence: `retention_policy_classifier.go`, `retention_row.go`, `retention_policy_resource.go`
- Retention settings reads and projections: `retention_settings_projection.go`, `retention_owner_snapshot.go`, `settings_read_savepoint.go`
- Retention policy routes and destructive preflight: `retention_policy_routes.go`, `retention_preflight.go`, `retention_cutoff_format.go`
- Retention impact analysis: `retention_impact_estimate.go`, `retention_impact_preview.go`
- Retention owner drift and manual jobs: `retention_owner_drift.go`, `retention_manual_job.go`, `../../../platform/managementjobs/jobs.go`, `../../../platform/managementjobs/retention_legacy.go`, `../../../platform/managementjobs/retention_api.go`
- Retention source projections: `../../../domain/stats/retention_source.go` (single owner)
- Auth settings: `../auth/auth_settings_mutation.go` (immutable config versions, readiness, acknowledgements)
- Costing/timezone: `routes.go`, `store.go`, `types.go`
- Settings operation identity and conflicts: `settings_request_identity.go`, `settings_operations.go`, `settings_conflict_errors.go`, `problems.go`
- Currency migration wire types and routes: `currency_migration_drafts.go`, `currency_migration_draft_routes.go`, `currency_migration_commit_routes.go`
- Currency migration persistence and preview: `currency_migration_draft_store.go`, `currency_migration_preview.go`, `currency_migration_cutover.go`
- Currency migration cursors and values: `currency_migration_cursor.go`, `currency_migration_values.go`, `currency_migration_identity.go`, `currency_migration_pages.go`
- Currency migration errors and archive: `currency_migration_errors.go`, `currency_migration_archive.go`, and the bounded Pricing page in `../connections/pricing_list_page.go`
- Settings scalar value projections: `settings_values.go`
- Frontend settings consumers: `../../../../../frontend/src/pages/settings/`

## CONVENTIONS

- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep costing, timezone, and API-family audit settings pinned to Default profile id `1`; `X-Profile-Id` may be accepted but is ignored. Keep storage `profile_id` columns untouched.
- Keep `/api/settings/audit` as exactly three rows (`openai`, `anthropic`, `gemini`) using the three-state `disabled|metadata_only|body_capture` mode union with full replacement PUT semantics; body capture requires audit enabled.
- Timezone is part of the costing CAS (`GET/PUT /api/settings/costing`); there is no standalone timezone route. Reporting currency has a single active epoch; currency-code change with an existing epoch requires the migration preview/commit flow.
- Currency migrations use bounded owner snapshots, chunked drafts, signed cursors, immutable template/evidence identities, and one atomic active-epoch cutover. Draft/chunk/preview/ledger payloads carry `template_kind` plus the complete role-keyed card set; missing/extra roles fail closed. `pricing_migration_legacy_template_evidence` remains legacy-only and is never used as the current card source. `archive_unused_fx` is the only archive path: it records an archive ledger and clears unused FX evidence without changing the active epoch, templates, or source FX authoring.
- Fresh installs keep all four retention fields `NULL`; existing values and legacy rows are classified without silent clamping or cleanup. `actual_coverage` is consumed from the Observe owner materialization cut, including source revision, coverage revision/hash, generation/fence, freshness, and gaps.
- Auth enablement consumes the Proxy owner's counted readiness snapshot and 30-second safe-active horizon; key writes and affected Requests/Audit writers share the DB admission/fence lane before changing or reading the transition proof.
- Log retention: every destructive change (enable `null -> N`, shorten, one-time cleanup) requires a fresh server preflight token plus keyword confirmation; manual job acceptance only seals a durable queued intent (202 / replay), never revokes coverage; final publish advances the domain revocation epoch/floor.
- Keep log-retention settings global and trigger cleanup through the management-jobs executor instead of request-path deletes.
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
