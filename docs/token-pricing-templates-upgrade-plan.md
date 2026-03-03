# Token Pricing Templates Upgrade (Hard Break, v2 Config)

## Brief summary
- Decouple token pricing from connection rows by introducing profile-scoped reusable pricing templates.
- Keep endpoint reuse model unchanged; connection keeps endpoint reference and now adds pricing-template reference.
- Deliver template CRUD plus attach/detach plus usage lookup.
- Migrate existing per-connection pricing into deduplicated templates automatically.
- Move config export/import to v2-only template-aware schema.

## Locked product decisions
- Template application: exclusive attach, no per-connection overrides.
- Migration: auto-generate templates from existing distinct pricing configs.
- Delete behavior: block deletion when template is in use, return dependency list.
- API compatibility: hard break for connection pricing fields now.
- Extra functions in v1: attach/detach and usage lookup only.
- Config compatibility: v2 only (legacy import/export rejected).

## Data model and migration
1. Add `PricingTemplate` model in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/models/models.py`.
   - Fields: `id`, `profile_id`, `name`, `description`, `pricing_unit`, `pricing_currency_code`, `input_price`, `output_price`, `cached_input_price`, `cache_creation_price`, `reasoning_price`, `missing_special_token_price_policy`, `version`, `created_at`, `updated_at`.
   - Constraints: unique `(profile_id, name)`, index on `profile_id`.
   - Enum-like values: `pricing_unit="PER_1M"` only; policy remains `MAP_TO_OUTPUT|ZERO_COST`.
2. Change `Connection` model in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/models/models.py`.
   - Add `pricing_template_id` nullable FK to `pricing_templates.id` with `ondelete="RESTRICT"`.
   - Add relationship `pricing_template_rel`.
   - Remove pricing columns from `connections`: `pricing_enabled`, `pricing_currency_code`, all token price fields, `missing_special_token_price_policy`, `pricing_config_version`.
   - Add index `idx_connections_pricing_template_id`.
3. Create Alembic revision in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/alembic/versions/`.
   - Create `pricing_templates` table.
   - Add `connections.pricing_template_id`.
   - Backfill using distinct legacy pricing tuples per profile and create deterministic names `Migrated Pricing Template N`.
   - Set each migrated connection’s `pricing_template_id`.
   - Leave previously non-effective pricing rows detached (`pricing_template_id=NULL`).
   - Drop legacy pricing columns from `connections`.
   - Add FK/indexes after backfill.

## API/interface changes
1. New router `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/pricing_templates.py`, mounted in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/main.py`.
   - `GET /api/pricing-templates`
   - `POST /api/pricing-templates`
   - `GET /api/pricing-templates/{template_id}`
   - `PUT /api/pricing-templates/{template_id}`
   - `DELETE /api/pricing-templates/{template_id}` with `409` dependency payload when in use
   - `GET /api/pricing-templates/{template_id}/connections` usage lookup
2. Attach/detach function endpoint.
   - Add `PUT /api/connections/{connection_id}/pricing-template` with `{ "pricing_template_id": int | null }`.
   - `int` attaches, `null` detaches.
   - Validate template/profile ownership.
3. Hard-break connection schemas in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`.
   - Remove pricing fields from `ConnectionCreate`, `ConnectionUpdate`, `ConnectionResponse`.
   - Add `pricing_template_id` to create/update/response.
   - Add `pricing_template` summary object to `ConnectionResponse`.
   - Set `extra="forbid"` on `ConnectionCreate` and `ConnectionUpdate` so old pricing keys return `422` (not silently ignored).
4. Add template schemas in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`.
   - `PricingTemplateCreate`, `PricingTemplateUpdate`, `PricingTemplateResponse`, `PricingTemplateListItem`, `ConnectionPricingTemplateUpdate`.
   - Validation: trimmed non-empty name, ISO currency code, non-negative decimal prices, policy enum.

## Runtime/service changes
1. Load template during route planning.
   - Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/loadbalancer.py` to `selectinload(Connection.pricing_template_rel)`.
2. Refactor costing in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/services/costing_service.py`.
   - Read prices from `pricing_template`, not `connection`.
   - `pricing_template is None` on successful call => unpriced with `PRICING_DISABLED`.
   - Keep micros math, FX `(model_id, endpoint_id)` logic, and special-token policy behavior.
   - Keep `pricing_snapshot_*` fields; write template version into `pricing_config_version_used`.
3. Update proxy flow in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/proxy.py`.
   - Pass `ep.pricing_template_rel` into cost computation.
   - No change to failover and endpoint routing logic.
4. Update connection router in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/connections.py`.
   - Validate and persist `pricing_template_id`.
   - Remove connection-level pricing diff/version logic.

## Config import/export v2 only
1. Update config schemas in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/schemas/schemas.py`.
   - Add `version: Literal[2]`.
   - Add `pricing_templates: list[ConfigPricingTemplateExport]`.
   - Replace connection inline pricing fields with `pricing_template_id` reference.
2. Update `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/app/routers/config.py`.
   - Export emits `version=2`, template list, and connection template references.
   - Import requires `version=2`; reject anything else with `400`.
   - Replace-mode import order handles template references safely.
   - Validate each referenced `pricing_template_id` exists in imported template set.

## Tests and acceptance criteria
1. Migration tests.
   - Distinct legacy pricing configs produce deduped templates.
   - Connections point to correct templates after migration.
   - Legacy connection pricing columns are removed.
2. Template CRUD tests.
   - Create/list/get/update/delete.
   - Name uniqueness per profile.
   - Version increments only for pricing-affecting edits.
   - Delete in-use template returns `409` with owner list.
3. Attach/detach and usage tests.
   - Attach/detach success paths.
   - Cross-profile attach rejected.
   - Usage lookup returns model/connection owner metadata.
4. Connection API hard-break tests.
   - Old inline pricing payload fields now fail validation (`422`).
   - New `pricing_template_id` paths work in create/update/read.
5. Costing/proxy regressions in `/Users/liqing/Documents/PersonalProjects/My_Proj/prism/backend/tests/test_smoke_defect_regressions.py`.
   - Template-priced requests compute identical micros behavior.
   - Missing template -> unpriced.
   - Special token policy behavior unchanged.
6. Config v2 tests.
   - Export contains `version=2` and template payload.
   - Import v2 round-trip preserves template assignments.
   - Import without `version=2` fails.

## Rollout notes
- Backend and frontend should ship together because connection pricing payload shape intentionally breaks.
- Frontend connection modal should switch from inline pricing editor to template selector plus link to template management.
- Existing DB pricing data is preserved by migration-created templates.

## Explicit assumptions/defaults
- Template scope is profile-level only.
- One connection can reference zero or one template.
- No per-connection template overrides.
- No bulk apply, clone, or template history/rollback in this release.
- No legacy config v1 compatibility.
- `pricing_unit` stays fixed at `PER_1M` for now.
