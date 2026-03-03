# Prism v2 Upgrade Plan: Token Pricing Templates (Whole System)

## Summary
- Adopt the locked backend plan as-is, then complete matching frontend, config-contract, docs, QA, and rollout work so the release is shippable end-to-end.
- Ship as a coordinated hard-break release (backend + frontend together), with DB migration preserving existing effective pricing via auto-created templates.
- Move configuration import/export to strict `version=2` only and reject legacy payloads.

## Implementation status
Implemented in the current codebase. Keep this document as rollout/design history rather than pending work.
## Workstream 1 - Backend Data Model + Migration (authoritative)
- Update `backend/app/models/models.py`:
  - Add `PricingTemplate` ORM with profile scope, `(profile_id, name)` uniqueness, `pricing_unit="PER_1M"`, policy enum, `version`, timestamps.
  - Add `Connection.pricing_template_id` (nullable FK `ondelete="RESTRICT"`), relationship `pricing_template_rel`, and index `idx_connections_pricing_template_id`.
  - Remove legacy connection pricing columns (`pricing_enabled`, currency/prices, policy, `pricing_config_version`).
  - Add `Profile.pricing_templates` relationship.
- Add Alembic revision in `backend/alembic/versions/`:
  - Create `pricing_templates`.
  - Add nullable `connections.pricing_template_id`.
  - Backfill by profile using distinct effective legacy pricing tuples; assign deterministic names `Migrated Pricing Template N`.
  - Set `pricing_template_id` for migrated rows; keep non-effective legacy rows detached (`NULL`).
  - Add FK/index after backfill; drop legacy pricing columns.
- Migration defaults:
  - Dedup key excludes connection identity; template `version` seeds from max source `pricing_config_version` in each dedup group (fallback `1`).
  - Non-effective rows = previously unpriced-by-config semantics (disabled/missing-invalid pricing data).

## Workstream 2 - Backend API, Schema, Runtime
- Add router `backend/app/routers/pricing_templates.py` and mount in `backend/app/main.py`:
  - `GET/POST /api/pricing-templates`
  - `GET/PUT/DELETE /api/pricing-templates/{template_id}`
  - `GET /api/pricing-templates/{template_id}/connections`
  - `DELETE` returns `409` with dependency list when in use.
- Update `backend/app/routers/connections.py`:
  - Add `PUT /api/connections/{connection_id}/pricing-template` (`{pricing_template_id: int|null}`).
  - Validate template ownership by effective profile.
  - Keep create/update support for `pricing_template_id` in connection payloads.
- Update schemas in `backend/app/schemas/schemas.py`:
  - Remove inline pricing from `ConnectionCreate/Update/Response`.
  - Add `pricing_template_id` and `pricing_template` summary on response.
  - Set `extra="forbid"` for `ConnectionCreate`/`ConnectionUpdate` to force `422` on legacy keys.
  - Add template DTOs: create/update/response/list/usage + attach request schema.
- Runtime updates:
  - `backend/app/services/loadbalancer.py`: eager load `Connection.pricing_template_rel`.
  - `backend/app/services/costing_service.py`: read prices from template; missing template => `PRICING_DISABLED`; preserve micros math, FX logic, snapshot fields, policy behavior.
  - `backend/app/routers/proxy.py`: pass `ep.pricing_template_rel` into cost computation.
  - `backend/app/routers/models.py`: include template relation in model-detail connection loads.

## Workstream 3 - Config Export/Import v2 (Strict)
- Update config schemas in `backend/app/schemas/schemas.py`:
  - Require `version: Literal[2]` on export/import contracts.
  - Add `pricing_templates` collection.
  - Replace connection inline pricing with `pricing_template_id`.
- Update `backend/app/routers/config.py`:
  - Export emits `version=2`, template list, and connection template refs.
  - Import rejects non-`2` payloads (`400`).
  - Replace-mode import order: endpoints -> models -> templates -> connections -> settings/rules, with reference validation.
  - Validate each `connection.pricing_template_id` exists in imported template set (or `null`).

## Workstream 4 - Frontend Contract + UX
- Update TS contracts in `frontend/src/lib/types.ts`:
  - Add `PricingTemplate*` interfaces and usage response types.
  - Remove connection inline pricing fields; add `pricing_template_id` + `pricing_template` summary.
  - Update config interfaces to `version: 2`, `pricing_templates`, connection template references.
- Update API client in `frontend/src/lib/api.ts`:
  - Add `api.pricingTemplates.*` CRUD + usage calls.
  - Add `api.connections.setPricingTemplate(...)`.
- Update model connection UI in `frontend/src/pages/ModelDetailPage.tsx`:
  - Replace inline pricing editor with template selector (`None` allowed) + quick link to template management.
  - Update connection cards to display template name/currency/version or "Unpriced".
- Add template management UI in `frontend/src/pages/SettingsPage.tsx` (new `pricing-templates` section):
  - List/create/edit/delete templates.
  - Usage drawer/table (`/connections` endpoint) before delete.
  - Conflict delete (`409`) shows dependent connections.
- Update config import validator in `frontend/src/lib/configImportValidation.ts`:
  - Enforce `version=2`; validate template references.
  - Update Settings import/export copy to "version 2 only".
- Improve API error handling in `frontend/src/lib/api.ts`:
  - Preserve structured `detail` payloads so dependency conflicts render actionable UI.

## Public API / Interface / Type Changes
- New endpoints:
  - `GET/POST /api/pricing-templates`
  - `GET/PUT/DELETE /api/pricing-templates/{template_id}`
  - `GET /api/pricing-templates/{template_id}/connections`
  - `PUT /api/connections/{connection_id}/pricing-template`
- Breaking changes:
  - Connection create/update/read remove inline pricing fields; add `pricing_template_id` and response `pricing_template`.
  - Connection create/update now reject legacy pricing keys with `422`.
  - Config import/export require `version=2`; non-v2 rejected.
- Stable behavior:
  - Proxy routing/failover unchanged.
  - Cost snapshots and micros accounting semantics unchanged.
  - Unpriced reason for missing template on success remains `PRICING_DISABLED`.

## Test Plan and Acceptance Criteria
- Backend automated (`backend/tests/`):
  - Migration: dedup template creation, assignment accuracy, legacy-column removal.
  - Template CRUD: uniqueness, validation, version increment rules, in-use `409`.
  - Attach/detach + cross-profile rejection + usage lookup metadata.
  - Connection hard-break validation (`422` on legacy keys).
  - Costing/proxy regressions preserve prior math/policy behavior.
  - Config v2 roundtrip + non-v2 rejection.
  - Multi-profile isolation for templates and config replace behavior.
- Frontend acceptance:
  - `pnpm run lint` and `pnpm run build` pass in `frontend`.
  - Manual smoke: template CRUD, attach/detach in model dialog, delete-in-use conflict UX, v2 import/export, v1 rejection messaging.
- Docs acceptance:
  - Update `docs/API_SPEC.md`, `docs/DATA_MODEL.md`, `docs/PRD.md`, `docs/SMOKE_TEST_PLAN.md`, and root/backend/frontend READMEs to v2 semantics.

## Rollout and Operations
- Pre-release:
  - Take PostgreSQL backup/snapshot.
  - Announce hard break: legacy frontend and config v1 files unsupported after deploy.
- Deploy strategy (coordinated window):
  1. Disable UI writes / maintenance mode.
  2. Deploy backend and run migration.
  3. Deploy frontend immediately after backend is healthy.
  4. Run smoke checks (template CRUD, connection attach, priced proxy request, config v2 export/import).
- Rollback:
  - Code rollback requires DB restore (columns dropped in migration); no safe in-place backward compatibility path.

## Explicit Assumptions and Defaults
- Template management lives in Settings (`#pricing-templates`) instead of a new top-level route.
- Config v2 uses current ID-based references (`endpoint_id`, `connection_id`, `pricing_template_id`) in this release.
- One connection references zero-or-one template; no per-connection pricing override.
- No bulk apply/clone/history in this release.
- `pricing_unit` fixed to `PER_1M`; only policy enum values `MAP_TO_OUTPUT|ZERO_COST`.
