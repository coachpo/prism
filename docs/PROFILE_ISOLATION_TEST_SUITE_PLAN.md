# Profile-Isolation Test Suite Plan (Backend `pytest` + Frontend Playwright MCP)

## Summary
Build an isolation-first test architecture with two lanes:
1. Backend API/integration tests in `pytest` that prove strict profile boundaries and active-vs-effective semantics.
2. Frontend browser E2E tests authored/debugged with Playwright MCP and checked in as repeatable Playwright specs.

The suite enforces a hard non-overlap contract: profile-scoped data must never leak across profiles, and `/v1/*` runtime routing must always use the active profile regardless of selected management profile.

## Important API/Interface Changes
1. Backend HTTP API: no contract changes planned.
2. Frontend DOM interface: add stable `data-testid` attributes in profile switcher, activation controls, and key list/table rows to make Playwright tests deterministic.
3. Test tooling interfaces:
- Add backend coverage tooling (`pytest-cov`) and isolation markers.
- Add frontend Playwright config/scripts for CI execution.
4. Runtime behavior under test remains unchanged.

## Planned File-Level Additions/Updates

### Backend
- Extend `backend/tests/conftest.py` with:
- `api_client` fixture (ASGI client against FastAPI app).
- `db_session` fixture for direct DB assertions.
- per-test cleanup fixture for scoped tables.
- `_recovery_state` reset fixture.
- mock upstream HTTP client fixture (`httpx.MockTransport`).
- Add helpers:
- `backend/tests/helpers/seed.py`
- `backend/tests/helpers/api.py`
- `backend/tests/helpers/assertions.py`
- Add isolation suites:
- `backend/tests/isolation/test_profile_context_contract.py`
- `backend/tests/isolation/test_models_endpoints_connections_isolation.py`
- `backend/tests/isolation/test_settings_blocklist_isolation.py`
- `backend/tests/isolation/test_config_import_export_isolation.py`
- `backend/tests/isolation/test_proxy_active_profile_isolation.py`
- `backend/tests/isolation/test_observability_isolation.py`

### Frontend
- Add Playwright runner:
- `frontend/playwright.config.ts`
- `frontend/e2e/fixtures/seedIsolation.ts`
- `frontend/e2e/stubs/upstreamStub.mjs`
- Add E2E specs:
- `frontend/e2e/profile-switching-and-header-scope.spec.ts`
- `frontend/e2e/models-endpoints-connections-isolation.spec.ts`
- `frontend/e2e/runtime-active-profile-isolation.spec.ts`
- `frontend/e2e/settings-import-export-isolation.spec.ts`
- `frontend/e2e/observability-isolation.spec.ts`
- Update `frontend/package.json` scripts for `e2e:isolation`, `e2e:isolation:pr`, `e2e:isolation:nightly`.
- Add targeted `data-testid` attributes in:
- `frontend/src/components/layout/AppLayout.tsx`
- `frontend/src/pages/ModelsPage.tsx`
- `frontend/src/pages/EndpointsPage.tsx`
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/pages/RequestLogsPage.tsx`
- `frontend/src/pages/AuditPage.tsx`

## Backend Test Design (Pytest)

### Isolation Oracle (applies to all profile-scoped endpoints)
1. With `X-Profile-Id = A`, response payload must contain only profile A resources.
2. Cross-profile direct ID access must return `404` and cause no mutation.
3. Same business identifiers may exist in different profiles without conflict.
4. Same identifiers in the same profile must enforce existing uniqueness errors.
5. Deletion/import/cleanup operations must affect only target profile rows.

### Contract and Routing Tests
1. Profile-scoped `/api/*` routes require `X-Profile-Id`; missing/invalid/deleted IDs return `400/404` as appropriate.
2. `/api/profiles/*` and provider routes remain global (no profile header requirement).
3. `/v1/*` routes bind to active profile dependency only.
4. `/v1/*` ignores `X-Profile-Id` override attempts.

### Workflow Isolation Tests
1. Profile lifecycle:
- create/update/activate/delete with CAS conflict and inactive-only delete.
- switching active profile changes runtime routing target only.
2. Config entities:
- models/endpoints/connections/settings/FX/header blocklist isolation.
- connection owner lookup isolated by effective profile.
3. Config export/import:
- export includes only selected profile.
- import replace mutates only target profile; neighbors unchanged.
4. Observability:
- request logs, stats summary/spending, audit logs are profile-filtered.
- batch deletions in stats/audit delete only selected profile records.
5. Proxy runtime:
- two-profile setup with local stub upstream fingerprinting.
- runtime call attribution (`request_logs.profile_id`, `audit_logs.profile_id`) follows active profile snapshot.

## Frontend Test Design (Playwright MCP + Checked-In Specs)

### MCP Workflow
1. Use Playwright MCP to capture robust interaction flows and locators.
2. Convert recordings into maintainable Playwright specs with stable `data-testid`/role locators.
3. Keep MCP as authoring/debug tool; CI runs checked-in specs only.

### UI Isolation Scenarios
1. Profile selector/header propagation:
- selecting profile changes management scope.
- `/api/*` requests carry correct `X-Profile-Id`.
2. Selected vs active mismatch:
- mismatch banner shown when selected != active.
- activating selected profile clears mismatch and updates runtime badge.
3. CRUD non-overlap:
- models/endpoints/connections created in profile B are absent in profile A views.
- duplicated names across profiles are allowed and remain isolated in UI lists.
4. Runtime isolation:
- with selected=B but active=A, generated `/v1/*` traffic logs under A.
5. Settings/config isolation:
- export/import and blocklist/costing operations affect only selected profile.
6. Observability isolation:
- Request Logs/Audit/Statistics pages show only selected profile data.

## Test Cases and Scenarios (Execution Split)

### PR Core (fast gate)
1. Backend:
- context/header contract tests.
- core CRUD cross-profile 404/non-mutation checks.
- `/v1/*` active-profile-only tests with local stub.
- observability profile-filter tests.
2. Frontend:
- profile switch + header injection.
- mismatch/activation workflow.
- one CRUD non-overlap scenario (models + endpoints).
- one observability non-overlap scenario.

### Nightly Full
1. Full backend isolation matrix across all scoped routes.
2. Full frontend E2E matrix including import/export and deletion workflows.
3. Expanded filter combinations for stats/audit pages.

## CI and Coverage

### CI Policy
1. PR: run backend core isolation + frontend `@pr-core` Playwright (Chromium only).
2. Nightly: run full backend isolation + full frontend isolation suite.

### Coverage Gate
1. Enforce `>=90%` on isolation-critical backend modules (dependencies + profile-scoped routers + proxy isolation paths), not whole-repo global yet.
2. Fail PR when isolation-module coverage drops below threshold.

## Explicit Assumptions and Defaults Chosen
1. Scope: Broad isolation coverage (selected).
2. Frontend mode: checked-in Playwright specs with MCP authoring/debug (selected).
3. CI: PR core + nightly full (selected).
4. Proxy upstream strategy: local deterministic stub server (selected).
5. Browser matrix: Chromium only (selected).
6. Current backend route surface as of March 2, 2026 includes `/v1/{path:path}` but not `/v1beta/*`; suite covers existing runtime route and keeps `/v1beta` parity as a future extension once implemented.
