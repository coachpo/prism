STATUS: DONE_WITH_CONCERNS

Commits:
- feat!: remove connection health probe route

Files changed:
- Removed backend connection health route implementation/tests:
  - backend/internal/httpapi/management/connections/health.go
  - backend/internal/httpapi/management/connections/health_test.go
  - backend/tests/runtime/runtime_phase4_health_check_test.go
- Removed route mounts/admission/contract rows:
  - backend/internal/httpapi/management/connections/service.go
  - backend/internal/platform/http/admission.go
  - backend/internal/platform/http/management_route_contract.json
- Updated backend connection/model response internals and affected tests:
  - backend/internal/domain/terminaltarget/terminaltarget.go
  - backend/internal/httpapi/management/connections/{routes_test.go,store.go,types.go}
  - backend/internal/httpapi/management/{models,endpoints}/{store.go,types.go,routes_test.go}
  - backend/tests/contract/{connection_contract_test.go,connection_s10_contract_test.go}
  - backend/tests/runtime/proxy_selector_test.go
- Removed frontend health probe API/types/hooks/UI/test surfaces:
  - frontend/src/lib/{api/management.ts,types/routing.ts}
  - frontend/src/pages/model-detail/{ConnectionDialog.tsx,useConnectionHealthChecks.ts,connectionProbeBehavior.ts,useModelDetailConnectionFlows.ts,useModelDetailDataSupport.ts,useModelDetailDialogState.ts}
  - frontend/src/features/models/detail/{ModelDetailFeaturePage.tsx,useModelDetailFeatureData.ts}
  - frontend/src/pages/models/AccessTargetsEditor.tsx
  - frontend/src/i18n/messages/{en.ts,zh-CN.ts}
  - frontend/src/test/model-detail-feature.test.ts
  - frontend/tests/e2e/{model-detail-access-target-authoring.spec.ts,model-detail-connection-dialog-probe.spec.ts,model-detail-request-logs-handoff.spec.ts}
  - frontend/tests/lib/{management_contract.test.mjs,profile_scope_header_contract.test.mjs}
  - frontend/tests/model-detail/connection_probe_behavior_contract.test.mjs
- Updated ownership/docs:
  - backend/internal/httpapi/management/AGENTS.md
  - backend/internal/httpapi/management/connections/AGENTS.md
  - frontend/src/pages/model-detail/AGENTS.md
  - frontend/tests/AGENTS.md
  - docs/{API_SPEC.md,ARCHITECTURE.md,DATA_MODEL.md,PRD.md,SMOKE_TEST_PLAN.md,TEST_CASE_GENERATION_METHODOLOGY.md,WORKFLOWS.md}

Tests and commands:
- PASS: `rg -in "healthcheck" backend/internal/httpapi/management/connections frontend/src` returned 0.
- PASS: `rg -n "HealthCheckResponse|useConnectionHealthChecks|dialogTestResult|dialogTestingConnection|openai_probe_endpoint_variant|connectionProbeBehavior|health-check" frontend/src frontend/tests` returned 0.
- PASS: `go test ./internal/httpapi/management/connections ./internal/platform/http`.
- PASS: `go build ./cmd/prism-backend`.
- PASS: `go test -c -o /tmp/prism-contract.test ./tests/contract`.
- PASS: `go test -c -o /tmp/prism-runtime.test ./tests/runtime`.
- PASS: `go test -c -o /tmp/prism-management-models.test ./internal/httpapi/management/models`.
- PASS: `go test -c -o /tmp/prism-management-endpoints.test ./internal/httpapi/management/endpoints`.
- PASS: `pnpm run test:lib`.
- PASS: `pnpm run test`.
- PASS: `pnpm run build`.
- PASS: `pnpm run lint`.
- PASS: `pnpm run test:server`.
- FAIL before assertions: `go test ./tests/contract` and `go test ./tests/contract -run '^$'` both failed because the local Postgres harness container did not become ready in time.
- FAIL before assertions: `go test ./tests/runtime` and `go test ./tests/runtime -run '^$'` both failed because the local Postgres harness container did not become ready in time.
- FAIL before assertions for touched management packages: `go test ./internal/httpapi/management/models ./internal/httpapi/management/endpoints` failed because the local Postgres harness container did not become ready in time.
- PARTIAL: `pnpm run test:e2e -- model-detail-access-target-authoring.spec.ts model-detail-request-logs-handoff.spec.ts` ran 4 tests; 3 passed, 1 failed waiting for an `Add target` button inside the Model Settings dialog. The failing assertion is outside the removed connection health-probe path.

Concerns:
- Docker/Postgres-backed Go suites cannot complete in this local environment because the harness Postgres container repeatedly fails readiness before tests reach assertions.
- The focused Playwright run exposed an existing model-settings dialog expectation failure unrelated to connection health probes.
- Known unrelated local changes were left unstaged and untouched: `.superpowers/sdd/task-12-report.md`, `.superpowers/sdd/task-9-report.md`, `docs/IMPLEMENTATION_PLAN.md`, and untracked `docs/TEST_REDUCTION_*.md`.

---

STATUS: REVIEW_FINDINGS_FIXED_WITH_CONCERNS

Fix summary:
- Removed `openai_probe_endpoint_variant` from owner-scoped connection create/update request shapes and write/read adapters. Strict JSON decoding now rejects the deleted field instead of accepting it as compatibility input.
- Removed remaining OpenAI probe helper constants/functions and runtime/domain propagation of probe metadata while preserving `openai_text_capability`.
- Removed dashboard topology `health_status` projection, frontend topology mapping, terminal-target inspector `24h Health` row, stale API example data, and stale i18n health-check copy.
- Removed direct test fixture writes to `openai_probe_endpoint_variant`; remaining scoped hits are negative rejection tests or retained schema/migration history.

Tests and commands:
- PASS: `rg -n "HealthCheckResponse|useConnectionHealthChecks|health-check" frontend/src frontend/tests backend/internal/httpapi/management/connections` returned 0.
- PASS: `rg -n "healthHealthy|healthUnknown|healthUnhealthy|checkedAt|checkingNow|routing24hHealth|24h Health|24h health|health-check|Health Check" frontend/src/i18n/messages/en.ts frontend/src/i18n/messages/zh-CN.ts` returned 0.
- PASS: `rg -n "health_status|HealthStatus|healthStatus" backend/internal/domain/stats frontend/src/pages/dashboard/routing-diagram frontend/src/lib/types/model-stats.ts frontend/tests/e2e/dashboard-aggregate-fixtures.ts docs/API_SPEC.md` returned 0.
- PASS: `rg -n "openai_probe_endpoint_variant" backend/internal/httpapi/management/connections backend/internal/httpapi/management/models backend/internal/httpapi/runtime backend/internal/domain/terminaltarget backend/internal/providercompat frontend/src frontend/tests` returns only the backend rejection test.
- PASS: `go test -count=1 ./internal/providercompat ./internal/httpapi/management/connections ./internal/domain/stats ./internal/httpapi/runtime`
- PASS: `go test -c -o /tmp/prism-contract.test ./tests/contract && go test -c -o /tmp/prism-integration.test ./tests/integration && go test -c -o /tmp/prism-runtime.test ./tests/runtime && go build ./cmd/prism-backend`
- PASS: `pnpm run test:lib`
- PASS: `pnpm exec vitest run src/pages/dashboard/RoutingDiagramCard.test.tsx src/test/model-detail-feature.test.ts`
- PASS: `pnpm run build`
- PASS: `pnpm run lint`
- PASS: `pnpm run test:server`
- FAIL before assertions: `go test -count=1 ./internal/httpapi/management/models ./internal/httpapi/management/endpoints` did not complete because local Postgres harness containers on ports 33268 and 33269 did not become ready in time.
- FAIL before assertions: `go test -count=1 ./tests/contract -run 'TestConnectionCapabilitySnapshotsExposeOpenAITextCapability|TestConnectionProbeEndpointVariantIsRemovedFromWrites|TestObservabilityDashboardTopologyGraphIncludesDisabledAndInactiveNodes'` did not complete because the local Postgres harness container on port 33270 did not become ready in time.
- FAIL before assertions: `go test -count=1 ./tests/integration -run 'TestManagementAuditStatsTopologyGraphDistinguishesTerminalRouteAndEndpointBinding'` did not complete because the local Postgres harness container on port 33271 did not become ready in time.
- FAIL unrelated: `pnpm run test -- RoutingDiagramCard.test.tsx model-detail-feature.test.ts` ran a broader Vitest set and failed in `src/test/rewrite-harness.test.tsx` waiting for `ok:msw-test-harness`; the explicit targeted `pnpm exec vitest run ...` command passed.

Concerns:
- Docker/Postgres-backed Go suites still cannot complete in this local environment because the harness Postgres containers fail readiness before tests reach assertions.
- Existing unrelated dirty files were left untouched except this appended report: `.superpowers/sdd/task-12-report.md`, `.superpowers/sdd/task-9-report.md`, `docs/IMPLEMENTATION_PLAN.md`, and untracked `docs/TEST_REDUCTION_*.md`.

---

STATUS: REVIEW_FINDINGS_FIXED_WITH_CONCERNS

Fix summary:
- Removed probe-era health fields from live connection response structs, model-detail nested connection summaries, terminal-target transfer records, and frontend `Connection` types.
- Stopped owner-scoped create from carrying `HealthStatus: "unknown"` through the live API response path. The retained DB column still receives the schema-required legacy value on insert and is no longer read into management responses.
- Removed stale smoke/API docs language for connection health actions, health-check blocklist coverage, and compatibility connection health metadata.

Tests and commands:
- PASS: `rg -n "HealthStatus|HealthDetail|LastHealthCheck|LastHealthAt" backend/internal/httpapi/management/connections backend/internal/httpapi/management/models backend/internal/domain/terminaltarget --glob '!**/*_test.go'` returned 0.
- PASS: `rg -n "health_detail|last_health_check" backend/internal/httpapi/management/connections backend/internal/httpapi/management/models backend/internal/domain/terminaltarget --glob '!**/*_test.go'` returned 0.
- PASS: `rg -n "health_status" backend/internal/httpapi/management/connections/types.go backend/internal/httpapi/management/models/types.go backend/internal/domain/terminaltarget/terminaltarget.go frontend/src/lib/types/routing.ts docs/API_SPEC.md` returned 0.
- PASS: `rg -n "health_status|health_detail|last_health_check|HealthStatus|HealthDetail|LastHealthCheck|LastHealthAt" frontend/src frontend/tests` returned 0.
- PASS: `rg -n "Connection health actions|Health-check also applies blocklist rules|health metadata" docs/SMOKE_TEST_PLAN.md docs/API_SPEC.md` returned 0.
- PASS: `go test -count=1 ./internal/httpapi/management/connections`.
- PASS: `go build ./cmd/prism-backend`.
- PASS: `go test -c -o /tmp/prism-management-models.test ./internal/httpapi/management/models && go test -c -o /tmp/prism-contract.test ./tests/contract`.
- PASS: `pnpm exec vitest run src/test/model-detail-feature.test.ts`.
- PASS: `pnpm run test:lib`.
- PASS: `pnpm run build`.
- PASS: `pnpm run lint`.
- FAIL before assertions: `go test -count=1 ./internal/httpapi/management/models` did not complete because the local Postgres harness container on port 33273 did not become ready in time.
- FAIL before assertions: `go test -count=1 ./tests/contract -run TestModelScopedConnectionCreateCreatesOwnerTarget` did not complete because the local Postgres harness container on port 33274 did not become ready in time.

Concerns:
- Retained migration/schema history still contains the legacy DB columns, and the connection insert keeps the minimum `health_status='unknown'` write required by the current NOT NULL column; no live management response or frontend type exposes it.
- Existing unrelated dirty files were left untouched except this appended report: `.superpowers/sdd/task-12-report.md`, `.superpowers/sdd/task-9-report.md`, `docs/IMPLEMENTATION_PLAN.md`, and untracked `docs/TEST_REDUCTION_*.md`.
