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
