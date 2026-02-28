# Prism Profile Isolation Upgrade Plan (Phased E2E)

## Summary

Implement strict profile isolation with one globally active profile for runtime traffic, explicit profile override for management APIs via `X-Profile-Id`, profile-attributed logs/audits, and phased frontend support. Rollout is backward-compatible through DB backfill and default-profile behavior.

## Locked Decisions

1. Providers stay global (`providers` table shared across profiles).
2. Header blocklist is split: system rules global, user rules profile-scoped.
3. Explicit profile override uses `X-Profile-Id` header.
4. Rollout is phased end-to-end (backend first, frontend cutover after backend compatibility is in place).
5. Profile deletion is soft-delete by default (`deleted_at`); active profile deletion is always rejected.
6. Config export/import v7 is ID-agnostic: imported endpoint/connection identifiers are logical references and are remapped server-side.
7. All profile-scoped management endpoints must resolve one effective profile and apply it consistently to list/detail/mutation queries.
8. Product terminology is explicit: selected profile controls management scope; active profile controls runtime `/v1/*` traffic.
9. Profile capacity is capped at 10 non-deleted profiles (`deleted_at IS NULL`).
10. If capacity is reached, profile creation is rejected until at least one profile is deleted.

## Public API / Interface Changes

1. Add profile management APIs in `backend/app/routers/profiles.py`:
   1. `GET /api/profiles`
   2. `GET /api/profiles/active`
   3. `POST /api/profiles`
      - Reject with `409` when non-deleted profile count (`deleted_at IS NULL`) is already `10`.
      - Error detail: user must delete a profile before creating a new one.
   4. `PATCH /api/profiles/{id}`
   5. `POST /api/profiles/{id}/activate` with CAS payload `{ expected_active_profile_id, expected_active_profile_version }`
   6. `DELETE /api/profiles/{id}` (soft-delete inactive profile; reject if active)
2. Add profile context transport:
   1. Management APIs accept optional `X-Profile-Id`; absent means active profile.
   2. Proxy routes (`/v1/*`, `/v1beta/*`) always use active profile and ignore overrides.
   3. Profile-scoped management endpoints (list/detail/create/update/delete) resolve effective profile through one shared dependency.
3. Config API changes in `backend/app/routers/config.py` and `backend/app/schemas/schemas.py`:
   1. Export version bumps from `6` to `7`.
   2. Import defaults to `mode=replace` for target profile only.
   3. No global destructive deletes during import.
   4. v7 payload is ID-agnostic and uses logical endpoint references (`endpoint_ref`) instead of DB primary keys.
   5. Import accepts `v6 | v7`; v6 numeric ids are treated as source-local references and remapped to target-profile rows.
4. Add `profile_id` attribution to request/audit schemas and responses.
5. Frontend request layer in `frontend/src/lib/api.ts` always sends `X-Profile-Id` for management API calls.

## Data Model and Migration Plan

1. Create Alembic revisions under `backend/alembic/versions`:
   1. `0002_profiles_additive`: create `profiles`; add nullable `profile_id` to scoped tables.
   2. `0003_profiles_backfill`: insert default profile, backfill existing rows, move singleton settings to default profile row.
   3. `0004_profiles_constraints`: enforce constraints and per-profile uniqueness, drop old global uniques.
2. New `profiles` table fields:
   1. `id`, `name` (unique), `description`, `is_active`, `version`, `deleted_at`, `created_at`, `updated_at`.
   2. Partial unique index enforcing at most one active profile.
   3. Capacity rule (application-level): at most 10 rows where `deleted_at IS NULL`.
3. Add `profile_id` (FK -> `profiles.id`) to:
   1. `model_configs`, `endpoints`, `connections`, `user_settings`, `endpoint_fx_rate_settings`, `request_logs`, `audit_logs`, `header_blocklist_rules` (nullable for system rules only).
4. Constraint changes:
   1. `model_configs`: unique `(profile_id, model_id)`; remove global `model_id` unique.
   2. `endpoints`: unique `(profile_id, name)`; remove global `name` unique.
   3. `endpoint_fx_rate_settings`: unique `(profile_id, model_id, endpoint_id)`.
   4. `user_settings`: unique `(profile_id)`.
   5. `header_blocklist_rules`: system rows require `profile_id IS NULL`; user rows require `profile_id IS NOT NULL`; user uniqueness is `(profile_id, match_type, pattern)`.
5. Index additions for performance:
   1. `model_configs(profile_id, model_id, is_enabled)`.
   2. `connections(profile_id, model_config_id, is_active, priority)`.
   3. `request_logs(profile_id, created_at)`.
   4. `audit_logs(profile_id, created_at)`.
   5. `endpoint_fx_rate_settings(profile_id, model_id, endpoint_id)`.
6. FK/retention semantics:
   1. Profile-scoped config tables (`model_configs`, `endpoints`, `connections`, `user_settings`, `endpoint_fx_rate_settings`, user `header_blocklist_rules`) are tied to profile ownership and are removable via explicit purge workflow.
   2. `request_logs` and `audit_logs` keep immutable `profile_id` attribution and are not cascaded by routine profile deletion.
   3. Routine `DELETE /api/profiles/{id}` uses soft-delete (`deleted_at`) to preserve historical attribution.

## Backend Implementation Workstreams

1. Profile context foundation:
   1. Add `Profile` ORM model in `backend/app/models/models.py`.
   2. Add profile schemas in `backend/app/schemas/schemas.py`.
   3. Add context resolver helper (new module `app/services/profile_context.py`) used by routers/services, including strict parsing/validation of `X-Profile-Id`.
   4. Update startup seeding in `backend/app/main.py` to ensure default profile and default per-profile settings row.
   5. Add create-profile guard in profile service/router: count non-deleted profiles and reject create at `>= 10` with `409`.
2. Runtime isolation in proxy/load balancer:
   1. Update model lookup signatures in `backend/app/services/loadbalancer.py` to require `profile_id`.
   2. Scope redirect target resolution (`redirect_to`) to same profile.
   3. Change `_recovery_state` key from `connection_id` to `(profile_id, connection_id)`.
   4. Capture profile at request start in `backend/app/routers/proxy.py` and pass through all log/audit/failover calls.
   5. Load runtime header blocklist as `is_system = true OR profile_id = active_profile_id`.
3. Management API scoping:
   1. Scope queries/mutations in `backend/app/routers/models.py`, `backend/app/routers/endpoints.py`, `backend/app/routers/connections.py`, `backend/app/routers/settings.py`, `backend/app/routers/config.py`, `backend/app/routers/stats.py`, and `backend/app/routers/audit.py` by effective profile.
   2. Ensure cross-profile IDs return `404` (not found in current effective profile).
   3. Validate endpoint/model profile consistency on connection create/update.
   4. Exclude soft-deleted profiles from normal profile listings and selection candidates.
4. Costing/settings isolation:
   1. Update `backend/app/routers/settings.py` for per-profile `UserSetting` and per-profile FX replace.
   2. Update `backend/app/services/costing_service.py` to load settings and FX mappings by profile.
5. Config import/export isolation:
   1. Update `backend/app/routers/config.py` to export/import target profile only.
   2. Replace global delete sequence with delete-where-`profile_id = target_profile_id`.
   3. Keep providers global; import never deletes providers globally.
   4. Export/import header blocklist user rules for target profile; system rules remain seeded global.
   5. Introduce v7 logical references (`endpoint_ref`) and server-side remap tables for endpoint/connection IDs during import.
   6. Keep v6 import compatibility by translating v6 `endpoint_id`/`connection_id` into logical references before write.
6. Observability attribution and scoping:
   1. Add `profile_id` write path in `backend/app/services/stats_service.py` and `backend/app/services/audit_service.py`.
   2. Filter list/detail/delete/report endpoints in `backend/app/routers/stats.py` and `backend/app/routers/audit.py` by effective profile by default.

## Frontend Workstreams

1. Add profile types and API methods in `frontend/src/lib/types.ts` and `frontend/src/lib/api.ts`.
2. Add profile context provider (new `frontend/src/context/ProfileContext.tsx`) and wire in `frontend/src/App.tsx`.
3. Add profile selector + active indicator in `frontend/src/components/layout/AppLayout.tsx`.
4. Make all page fetch effects depend on profile context revision so switching profile refetches scoped data.
5. Update settings copy/import UX in `frontend/src/pages/SettingsPage.tsx` to clarify profile-targeted replace behavior.

## Test Cases and Scenarios

1. Migration/backfill:
   1. Existing single-namespace DB upgrades to default profile with no data loss.
   2. Old unique constraints are replaced by composite profile constraints.
2. Core isolation:
   1. Same `model_id` can exist in profile A and B.
   2. Same endpoint `name` can exist in profile A and B.
   3. Active profile routing returns `404` for model existing only in inactive profile.
3. Switch safety:
   1. Concurrent activation requests with stale expected version return `409`.
   2. Active profile cannot be deleted.
   3. Inactive profile delete performs soft-delete and no longer appears in default profile listings.
   4. Creating an 11th non-deleted profile returns `409` with actionable error.
4. Runtime memory isolation:
   1. Failover cooldown in profile A does not affect profile B for same logical endpoint name/model.
5. Config safety:
   1. Import replace in profile A does not mutate profile B/C scoped rows.
   2. No global provider delete occurs during profile import.
   3. v7 import with logical endpoint references succeeds without PK collision assumptions.
   4. v6 import remains accepted and is remapped into target profile-owned rows.
6. Costing/settings:
   1. Updating profile A currency/FX does not change profile B settings.
7. Observability:
   1. Every request/audit row has immutable `profile_id`.
   2. Stats/audit list/detail/delete operations affect only effective profile.
8. Frontend behavior:
   1. Profile selector visible globally.
   2. Switching selected profile refreshes models/endpoints/stats/logs/audit/settings data.
   3. API calls include `X-Profile-Id` consistently.
   4. Create-profile action is blocked with clear guidance when 10 non-deleted profiles already exist.

## Rollout and Validation Sequence

1. Deploy backend with migration chain and profile-aware backend logic.
2. Run backend regression suite and targeted new profile-isolation tests.
3. Smoke-test with three profiles A/B/C and same model IDs across profiles.
4. Deploy frontend profile selector and header propagation.
5. Run frontend build/typecheck and manual UX smoke pass.
6. Update docs in `docs` (`PRD.md`, `ARCHITECTURE.md`, `DATA_MODEL.md`, `SMOKE_TEST_PLAN.md`) to reflect PostgreSQL + profile isolation model.

## Assumptions and Defaults

1. Active profile is global server state (single-operator model), not user/session-specific.
2. Proxy runtime always uses active profile and does not support per-request profile override.
3. Providers are global and shared; provider audit settings remain global.
4. Config import supports `replace` mode only in this upgrade; merge remains future work.
5. Existing v6 config files are accepted through compatibility mapping; exported format is v7 with logical references.
6. Selected profile (management scope) and active profile (runtime scope) are intentionally distinct states.
7. Routine profile deletion is soft-delete; hard purge remains an explicit future admin workflow.
8. Capacity accounting excludes soft-deleted profiles (`deleted_at IS NOT NULL`).
