# Frontend Iteration Plan For Profile Isolation (Hard Cutover)

## Summary
Implement a frontend hard-cutover to profile-aware management APIs, with a global profile selector that changes management scope only, explicit runtime activation action, and deterministic refetch on profile context changes.
This plan assumes backend profile APIs are already deployed and stable before frontend release.
Baseline at planning time: `frontend` lint and build passed.

## Locked Product Decisions
1. Selector behavior: changing selected profile updates management scope (`X-Profile-Id`) only.
2. Runtime activation: separate explicit action (`activate`) with CAS conflict handling.
3. Compatibility strategy: hard cutover (no legacy fallback path).
4. Profile management surface: app-shell dropdown + modal/dialog UX in layout.
5. Selected profile and active profile are intentionally separate states in UI copy and behavior.
6. Config v7 import/export in frontend is ID-agnostic and uses logical references; UI must not assume stable DB primary keys across profiles.
7. Profile creation is capped at 10 non-deleted profiles; user must delete one before creating another.

## Public Interfaces And Type Changes
1. Update frontend profile models in `frontend/src/lib/types.ts`:
   - Add `Profile`, `ProfileCreate`, `ProfileUpdate`, `ProfileActivateRequest`.
   - Add `profile_id` to `RequestLogEntry`, `AuditLogListItem`, `AuditLogDetail`.
2. Add profile API namespace in `frontend/src/lib/api.ts`:
   - `list`, `getActive`, `create`, `update`, `activate`, `delete`.
   - Treat `409` from `create` as capacity reached (`10` non-deleted profiles).
3. Add profile-aware request transport in `frontend/src/lib/api.ts`:
   - Module-level current profile id setter/getter.
   - Inject `X-Profile-Id` on all `/api/*` management requests after profile bootstrap.
4. Add profile context contract in `frontend/src/context/ProfileContext.tsx`:
   - `profiles`, `activeProfile`, `selectedProfile`, `selectedProfileId`, `revision`, `status`.
   - Actions: `selectProfile`, `refreshProfiles`, `createProfile`, `updateProfile`, `activateProfile`, `deleteProfile`, `bumpRevision`.
5. Update config import/export typing:
   - In `frontend/src/lib/types.ts`, support config version `7` export, accept import `6 | 7`.
   - In `frontend/src/lib/configImportValidation.ts`, accept both versions; for v7 allow optional `mode` and default to `replace` when absent.
   - Model v7 endpoint references with stable logical keys (for example `endpoint_ref`) and avoid hard dependence on numeric endpoint/connection ids.

## Implementation Plan

## Phase F1: Profile Foundation (Types, API, Bootstrap)
1. Add profile types and schema changes in `frontend/src/lib/types.ts`.
2. Refactor request helper in `frontend/src/lib/api.ts`:
   - Keep existing API method signatures for pages.
   - Add `setApiProfileId(profileId: number | null)` and internal header merge logic.
3. Add `profiles` API methods in `frontend/src/lib/api.ts` with exact backend paths.
4. Create `frontend/src/context/ProfileContext.tsx`:
   - Bootstrap sequence: load profiles + active profile, choose selected profile, set API profile id, set ready.
   - Selected profile persistence in `localStorage` (`prism.selectedProfileId`), with fallback to active if missing/invalid.
   - `revision` increments on selected profile change and explicit `bumpRevision`.
5. Wire provider in `frontend/src/App.tsx`:
   - Wrap routed app with `ProfileProvider`.
   - Block route rendering until profile bootstrap completes (loading shell), so management requests always carry `X-Profile-Id`.

## Phase F2: Layout UX (Selector, Active Indicator, CRUD/Activate)
1. Add global profile control to `frontend/src/components/layout/AppLayout.tsx`:
   - Selector for management scope (selected profile).
   - Active runtime indicator (`Active: <name>`).
   - "Activate selected" button visible when selected is not active.
2. Add profile management dialogs in `frontend/src/components/layout/AppLayout.tsx`:
   - Create profile dialog.
   - Edit selected profile dialog.
   - Delete selected profile dialog with explicit confirmation text and active-profile delete guard.
   - Capacity guard for create: when non-deleted profile count is `10`, disable/guard create and show message "Maximum 10 profiles reached. Delete a profile to create a new one."
3. Activation flow:
   - Send CAS payload from current active snapshot.
   - On `409`, show conflict toast, refresh profile list/active state, keep user on same selected profile.
4. Mobile parity:
   - Render same selector + activation affordance in mobile top bar area.
   - Keep same create-capacity guard and message behavior on mobile.

## Phase F3: Scoped Refetch Wiring Across Pages
1. Add `const { revision } = useProfileContext()` in each scoped page and include `revision` in data-fetch effect dependencies:
   - `frontend/src/pages/DashboardPage.tsx`
   - `frontend/src/pages/ModelsPage.tsx`
   - `frontend/src/pages/ModelDetailPage.tsx`
   - `frontend/src/pages/EndpointsPage.tsx`
   - `frontend/src/pages/StatisticsPage.tsx`
   - `frontend/src/pages/RequestLogsPage.tsx`
   - `frontend/src/pages/AuditPage.tsx`
   - `frontend/src/pages/SettingsPage.tsx`
2. Profile switch safety behavior:
   - Reset pagination offsets where relevant (`statistics/request-logs/audit`) on revision change.
   - In detail routes (model detail), handle scoped `404` by toast + navigation back to list.
3. Cross-profile cache safety:
   - Update `frontend/src/hooks/useConnectionNavigation.ts` cache key to include selected profile id or clear cache on revision.
4. Timezone preference isolation:
   - Update `frontend/src/hooks/useTimezone.ts` to refetch preference on profile revision.

## Phase F4: Settings UX And Config Semantics
1. Update destructive import copy in `frontend/src/pages/SettingsPage.tsx`:
   - From global-destructive language to profile-scoped replace language.
   - Explicitly state: replaces selected profile data only; other profiles unaffected.
2. Update import confirmation dialog in `frontend/src/pages/SettingsPage.tsx`:
   - Include selected profile name/id.
   - Mention providers remain global and are not globally deleted.
3. Update validation in `frontend/src/lib/configImportValidation.ts`:
   - Accept v6 and v7 files.
   - Preserve v6 compatibility; for v7, default missing mode to `replace`.
   - Parse v6 endpoint/connection ids as source-local references only; do not treat them as target DB ids.
4. After successful import in `frontend/src/pages/SettingsPage.tsx`:
   - Call `bumpRevision()` so other pages refetch scoped data immediately.

## Test Cases And Scenarios
1. API/header propagation:
   - Verify all `/api/*` calls include `X-Profile-Id` after bootstrap.
2. Selector behavior:
   - Switching selected profile refreshes all scoped pages without activating runtime profile.
3. Activation behavior:
   - Activate selected profile updates active indicator globally.
   - Concurrent stale activation returns `409` and UI refreshes active snapshot.
   - Switching selected profile does not mutate active runtime profile until explicit activation succeeds.
4. CRUD behavior:
   - Create/edit/delete profile from layout dialogs.
   - Active profile delete is blocked in UI and backend response handled gracefully.
   - Attempting to create an 11th non-deleted profile surfaces clear capacity error instructing deletion first.
5. Scoped data integrity:
   - Same route/filter values produce different scoped datasets when switching A/B/C.
6. Settings/import semantics:
   - Import in profile A does not change profile B/C data.
   - Copy accurately reflects profile-scoped replace behavior.
   - v7 imports succeed without requiring endpoint/connection DB primary-key alignment.
   - v6 imports remain accepted via compatibility mapping.
7. Hooks/caches:
   - Connection navigation cache does not leak across profile switches.
   - Timezone preference updates when switching profiles.
8. Regression gates:
   - `cd frontend && pnpm run lint`
   - `cd frontend && pnpm run build`

## Rollout Sequence
1. Deploy backend profile APIs and profile-aware scoping first.
2. Deploy frontend hard-cutover build after backend is live.
3. Run A/B/C smoke pass on: models, endpoints, stats, request logs, audit, settings import/export, activation conflict.

## Assumptions And Defaults
1. Backend profile endpoints return stable profile objects with fields: `id`, `name`, `description`, `is_active`, `version`, `created_at`, `updated_at`.
2. `X-Profile-Id` is accepted on management APIs and ignored on global provider semantics where applicable.
3. Config export format is v7; import accepts v6 and v7.
4. Selected profile persists in browser localStorage and falls back to active if invalid.
5. No legacy-mode fallback will be implemented in this iteration.
6. Backend resolves cross-profile resource ids as not found (`404`) under current selected profile scope.
7. Backend enforces max 10 non-deleted profiles and returns `409` on create beyond capacity.
