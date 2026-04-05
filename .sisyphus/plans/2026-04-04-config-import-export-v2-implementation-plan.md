# Prism config import/export v2 implementation plan

## Source of truth

- Requirements doc: `/Users/liqing/Documents/PersonalProjects/prism-workspace/prism/docs/plans/2026-04-04-config-import-export-v2-requirements.md`
- Worktree branch: `feature/config-import-export-v2`
- Root worktree path: `/Users/liqing/Documents/PersonalProjects/prism-workspace/model-share-feature-config-import-export-v2`

## Core v2 contract summary

- Replace the current config contract with `version: 2` only. No runtime support for `version: 1`.
- Split bundle authority into two workflows:
  - `bundle_kind: "profile_config"` for profile-scoped config
  - `bundle_kind: "vendor_catalog"` for global vendor metadata
- Profile bundles are authoritative for endpoints, pricing templates, strategies, models, connections, profile settings, and non-system header blocklist rules.
- Profile bundles are not authoritative for global vendor metadata. Profile import resolves vendors by `vendor_key` only, reuses existing global vendors, and never mutates existing global vendor rows based on hint drift.
- Exported profile bundles must not contain plaintext endpoint API keys. Endpoint secrets must move through `secret_payload` with encrypted entries referenced by stable secret refs.
- Introduce `CONFIG_BUNDLE_ENCRYPTION_KEY`, defaulting to `SECRET_ENCRYPTION_KEY` when unset.
- Add a preview-first import flow. Backend preflight must fully validate schema, references, vendor resolution, and secret decryption before any destructive mutation.
- Frontend validation becomes structural only. Backend preview is the authoritative source for import readiness and failure reporting.

## Repository and commit model

- Root repo contains docs and the `backend/` and `frontend/` submodule gitlinks.
- `backend/` and `frontend/` are separate git repositories. They must be committed inside their own repos before the root repo records updated submodule SHAs.
- Because submodules are checked out at pinned commits, create local working branches inside each changed submodule before editing.

## Phase 1 — prepare branch, artifacts, and implementation map

### Work

- Confirm the worktree is clean on `feature/config-import-export-v2`.
- Create local working branches inside submodules before code changes:
  - backend: `feature/config-import-export-v2-backend`
  - frontend: `feature/config-import-export-v2-frontend`
- If the requirements doc is absent from the worktree branch, add it to `docs/plans/` so the branch carries the design source alongside the implementation plan.
- Save the approved implementation plan to `docs/plans/2026-04-04-config-import-export-v2-implementation-plan.md` in the root repo before implementation starts.
- Map each requirement to owner files in backend, frontend, and root docs.

### Primary file targets

- root: `docs/plans/2026-04-04-config-import-export-v2-requirements.md`
- root: `docs/plans/2026-04-04-config-import-export-v2-implementation-plan.md`
- backend: `app/routers/config_domains/{import_export,export_builder,import_validator,import_executor}.py`
- backend: `app/schemas/domains/admin.py`
- backend: `app/schemas/schemas.py`
- backend: `app/core/{config,crypto}.py`
- frontend: `src/lib/{configImportValidation.ts,types/config-audit-settings.ts}`
- frontend: `src/lib/api/observability.ts`
- frontend: `src/pages/settings/useConfigBackupData.ts`
- frontend: `src/pages/settings/sections/BackupSection.tsx`

### QA scenario

- Root repo:
  - `git status --short`
  - `git branch --show-current`
- Backend submodule:
  - `git status --short`
  - `git checkout -b feature/config-import-export-v2-backend`
- Frontend submodule:
  - `git status --short`
  - `git checkout -b feature/config-import-export-v2-frontend`
- Expected result:
  - all three repos are on explicit working branches with clean status
  - requirements doc path is known and the implementation plan file exists in the root repo before code edits begin

## Phase 2 — backend v2 contract, routes, and secret handling

### Work

- Replace the current config request/response schemas with explicit `version: 2` bundle schemas for both profile and vendor catalog workflows.
- Update config route shells to expose the new profile export, profile preview import, profile import, vendor export, vendor preview import, and vendor import paths required by the requirements doc.
- Rework export assembly so profile bundles emit:
  - `bundle_kind: "profile_config"`
  - `vendor_refs`
  - `profile_settings`
  - `secret_payload`
  - no plaintext `endpoints[].api_key`
- Rework import validation so profile bundles validate bundle kind, version, internal references, secret refs, and duplicate conditions against the new contract.
- Rework import execution so it:
  - resolves vendors by `vendor_key` only
  - pre-decrypts every required secret before mutation
  - fails closed on decrypt errors or missing secret entries
  - preserves atomic replace semantics for the selected profile
- Add `CONFIG_BUNDLE_ENCRYPTION_KEY` and bundle `key_id` derivation.
- Make export fail loudly if stored endpoint secrets cannot be decrypted.

### Primary file targets

- `backend/app/routers/config_domains/import_export.py`
- `backend/app/routers/config_domains/export_builder.py`
- `backend/app/routers/config_domains/import_validator.py`
- `backend/app/routers/config_domains/import_executor.py`
- `backend/app/schemas/domains/admin.py`
- `backend/app/schemas/schemas.py`
- `backend/app/core/config.py`
- `backend/app/core/crypto.py`

### QA scenario

- From `backend/`, run focused contract tests after the backend implementation lands:
  - `uv run pytest tests/multi_profile_isolation/test_config_import_export_cases/profile_scoped_config_export_import_isolation_tests.py -v`
  - `uv run pytest tests/smoke_defect_regressions/test_config_cases/config_roundtrip_numeric_ids_and_system_rule_timestamp_tests.py -v`
  - `uv run pytest tests/smoke_defect_regressions/test_config_cases/user_settings_seed_and_config_schema_validation_tests.py -v`
  - `uv run pytest tests/smoke_defect_regressions/test_startup_cases/auth_management_flows_tests.py -k "config_export or secret" -v`
- Expected result:
  - v2 bundle tests pass
  - no plaintext-secret export behavior remains
  - wrong-shape and wrong-secret cases fail before profile mutation

## Phase 3 — frontend v2 types, preview flow, and backup UX

### Work

- Replace the current v1 config types with the v2 bundle types.
- Remove frontend assumptions that `endpoints[].api_key` is plaintext.
- Update structural validation to accept only the new `bundle_kind`-based v2 shapes.
- Change the backup/import flow so the browser uploads a file, gets preview results from the backend, shows warnings/errors, and only then executes import.
- Update backup UI copy to describe encrypted secret bundles instead of plaintext-secret export warnings.
- Update import summary logic for the new bundle shape.

### Primary file targets

- `frontend/src/lib/types/config-audit-settings.ts`
- `frontend/src/lib/configImportValidation.ts`
- `frontend/src/lib/api/observability.ts`
- `frontend/src/pages/settings/useConfigBackupData.ts`
- `frontend/src/pages/settings/sections/BackupSection.tsx`

### QA scenario

- From `frontend/`, run:
  - `pnpm run test`
  - `pnpm run build`
  - `pnpm run lint`
- Expected result:
  - the frontend compiles and tests cleanly against the v2 contract
  - config import validation tests cover the new bundle kinds, preview-driven flow, and invalid-file cases

## Phase 4 — add and align automated tests

### Work

- Extend backend tests for:
  - encrypted secret export
  - missing secret entry failure
  - wrong-key preview failure
  - wrong-key import failure with no mutation
  - vendor reuse by `vendor_key`
  - vendor creation when `vendor_key` is absent
  - repeated import idempotency
- Extend frontend tests for:
  - structural validation of v2 bundles
  - preview-before-import behavior in the backup flow
  - UI handling for preview warnings and blocking errors

### Primary file targets

- backend config/import/export test files under:
  - `backend/tests/multi_profile_isolation/test_config_import_export_cases/`
  - `backend/tests/smoke_defect_regressions/test_config_cases/`
  - `backend/tests/smoke_defect_regressions/test_startup_cases/`
- frontend tests under:
  - `frontend/src/lib/__tests__/configImportValidation.test.ts`
  - backup-flow tests added near the settings page data or backup hook area

### QA scenario

- Backend full config-related regression pass:
  - `uv run pytest tests/test_smoke_defect_regressions.py -v`
  - `uv run pytest tests/test_multi_profile_isolation.py -v`
- Frontend full validation/build pass:
  - `pnpm run test`
  - `pnpm run build`
- Expected result:
  - all newly-added v2 scenarios are covered and passing
  - no remaining test encodes v1 as the live import/export contract

## Phase 5 — manual QA on the real app

### Work

- Run the real launcher from the root worktree.
- Exercise the settings backup UI with a valid v2 export/import cycle.
- Exercise the invalid-file path.
- Confirm the exported JSON shows v2 fields and encrypted secret payloads rather than plaintext API keys.

### QA scenario

- From the root worktree, run:
  - `./start.sh full`
- Use Playwright or Chrome DevTools to:
  1. open the app
  2. navigate to Settings → Backup
  3. export the current profile bundle
  4. inspect the saved JSON and confirm:
     - `version: 2`
     - `bundle_kind: "profile_config"`
     - `secret_payload` exists
     - no plaintext endpoint `api_key` fields exist
  5. import the valid exported bundle through preview and execution
  6. confirm preview readiness and final success feedback
  7. upload a malformed or wrong-version file
  8. confirm the UI shows a blocking error and no successful import occurs
- Expected result:
  - valid v2 export/import works end-to-end
  - invalid-file handling is user-visible and non-destructive

## Phase 6 — docs and root repo alignment

### Work

- Update the live docs that become the final source of truth:
  - `docs/API_SPEC.md`
  - `docs/ARCHITECTURE.md`
  - `docs/DATA_MODEL.md`
- Keep the approved implementation plan saved under `docs/plans/` in the root repo.
- Update root gitlinks only after backend and frontend submodule commits are finalized.

### QA scenario

- Root repo checks:
  - `git status --short`
  - `git diff -- docs/API_SPEC.md docs/ARCHITECTURE.md docs/DATA_MODEL.md docs/plans/2026-04-04-config-import-export-v2-implementation-plan.md`
- Expected result:
  - root docs describe the v2 split-bundle and encrypted-secret workflow accurately
  - root repo changes are limited to docs and submodule pointer updates

## Phase 7 — commit, rebase, and cleanup

### Work

- Commit backend changes inside `backend/` on `feature/config-import-export-v2-backend`.
- Commit frontend changes inside `frontend/` on `feature/config-import-export-v2-frontend`.
- In the root repo, commit:
  - approved implementation plan
  - requirements doc if newly added to the branch
  - live doc updates
  - updated backend/frontend gitlinks
- Rebase backend and frontend branches onto their respective `main` branches if needed for clean submodule history.
- Rebase the root worktree branch `feature/config-import-export-v2` onto root `main`.
- If gitlink conflicts occur in the root rebase, resolve them by checking out the intended rebased backend/frontend submodule SHAs, staging the gitlinks, and re-running verification.
- Delete the worktree only after all rebased commits and verification steps are safely recorded.

### QA scenario

- Backend submodule:
  - `git status --short`
  - `git log -1 --oneline`
- Frontend submodule:
  - `git status --short`
  - `git log -1 --oneline`
- Root repo before and after rebase:
  - `git status --short`
  - `git log --oneline --decorate -5`
- Post-rebase verification rerun:
  - `uv run pytest tests/multi_profile_isolation/test_config_import_export_cases/profile_scoped_config_export_import_isolation_tests.py -v`
  - `uv run pytest tests/test_smoke_defect_regressions.py -v`
  - `pnpm run test`
  - `pnpm run build`
  - `pnpm run lint`
- Expected result:
  - backend, frontend, and root repos are clean after their commits
  - rebased history is clean
  - verification still passes after rebase
  - the worktree can be removed without losing any uncommitted work

## Completion standard

The implementation is ready only when:

- the approved plan is saved to `docs/plans/` in the root repo
- backend and frontend changes match the v2 requirements doc
- diagnostics/tests/build/manual QA all pass with fresh evidence
- backend and frontend submodule commits are recorded and the root repo points at the intended SHAs
- the root worktree branch is rebased onto `main`
- the worktree has been deleted cleanly after all work is complete
