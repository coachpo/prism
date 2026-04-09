# 2026-04-09 Static Timeout Rollback Review and Test Note

## Scope

- Lead-managed review of the active branch rollback removing per-endpoint and per-strategy timeout fields.
- Two bounded Sisyphus-Junior reviews: one for `frontend/`, one for `backend/`.
- No third cross-cutting reviewer was needed because the frontend and backend conclusions aligned.

## Review Outcome

- **Frontend review:** no P0-P3 findings. The rollback was internally consistent across the typed API layer, config import schema, and load-balance strategy form shaping.
- **Backend review:** no P0-P3 findings. The rollback was internally consistent across schemas, config import/export surfaces, proxy runtime handling, and the forward-only Alembic revision.
- **Contract divergence:** none found. Frontend and backend both removed `timeout_policy` and endpoint timeout fields in the same working tree.

## Explored Paths

- Frontend reviewed surfaces included `frontend/src/lib/api/management.ts`, `frontend/src/lib/configImportValidation.ts`, `frontend/src/lib/loadbalanceRoutingPolicy.ts`, `frontend/src/lib/types/{config-audit-settings,loadbalance,routing}.ts`, `frontend/src/pages/loadbalance-strategies/{LoadbalanceStrategyDialog.tsx,loadbalanceStrategyFormState.ts}`, and the existing Node and Playwright test surfaces.
- Backend reviewed surfaces included `backend/app/bootstrap/startup.py`, `backend/app/models/domains/routing.py`, `backend/app/routers/config_domains/{export_builder,import_executor}.py`, `backend/app/routers/endpoints_domains/route_handlers.py`, `backend/app/routers/proxy_domains/*`, `backend/app/schemas/domains/{admin,connection_model,endpoint_pricing}.py`, `backend/app/services/loadbalancer/{executor,policy,strategies,types}.py`, `backend/app/services/proxy_support/transport.py`, `backend/app/alembic/versions/0019_static_timeout_rollback.py`, and the related `pytest` coverage.
- Cross-check context also included the updated live docs in `docs/{ARCHITECTURE,API_SPEC,DATA_MODEL,PRD}.md` and the current root diff file list.

## Test Additions Started From Review Gaps

- Added frontend TypeScript-backed contract tests for:
  - config import schema acceptance/rejection around removed timeout fields
  - management API normalization and endpoint contract handling without removed timeout fields
  - load-balance strategy form payload shaping without timeout policy fields
- Added backend contract tests for:
  - `EndpointUpdate` rejecting removed timeout fields
  - `ConfigImportRequest` accepting timeout-free bundles and rejecting removed timeout fields
  - `build_export_payload()` omitting removed timeout fields from exported endpoints and load-balance strategies

## Files Added

- `frontend/tests/helpers/loadTsModule.mjs`
- `frontend/tests/lib/config_import_validation_contract.test.mjs`
- `frontend/tests/lib/management_contract.test.mjs`
- `frontend/tests/loadbalance/loadbalance_strategy_form_state_contract.test.mjs`
- `backend/tests/test_config_import_timeout_contract.py`
- `backend/tests/test_export_builder_timeout_contract.py`
- `docs/archive/2026-04-09-static-timeout-rollback-review-and-test-note.md`

## Verification Captured During This Pass

- Frontend targeted node tests passed:
  - `node --test tests/lib/config_import_validation_contract.test.mjs tests/lib/management_contract.test.mjs tests/loadbalance/loadbalance_strategy_form_state_contract.test.mjs tests/main/main_entrypoint_structure.test.mjs tests/server/server_health_entrypoint.test.mjs`
- Backend targeted pytest passed:
  - `uv run pytest tests/test_static_timeout_runtime.py tests/test_config_import_timeout_contract.py tests/test_export_builder_timeout_contract.py tests/test_proxy_transport_timeout.py tests/test_proxy_streaming_timeout_runtime.py`
- Diagnostics were clean on `backend/tests` and `frontend/tests`.
- Broader verification also passed:
  - `uv run pytest tests`
  - `pnpm run build`

## Notes

- This archive note is the required docs artifact for the current working tree. No git commit was created because the user did not ask for one.
