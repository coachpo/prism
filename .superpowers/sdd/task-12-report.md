STATUS: DONE_WITH_CONCERNS

Commits:
- `feat!: retire realtime websocket in favor of REST polling` (created by this task; final hash reported by the assistant after commit creation)

Files changed:
- Frontend dashboard/statistics polling: `frontend/src/pages/dashboard/useDashboardPolling.ts`, `frontend/src/pages/dashboard/useDashboardPageData.ts`, `frontend/src/pages/dashboard/useDashboardBootstrapData.ts`, `frontend/src/pages/statistics/useUsageStatisticsPageData.ts`, `frontend/src/pages/DashboardPage.tsx`
- Frontend realtime removal: deleted websocket client/helper/hook/status component files under `frontend/src/lib/websocket*`, `frontend/src/hooks/useRealtimeData.ts`, `frontend/src/components/WebSocketStatusIndicator.tsx`, `frontend/src/pages/statistics/useUsageStatisticsRealtimeData.ts`, and deleted websocket-focused frontend tests.
- Backend realtime removal: deleted `backend/internal/httpapi/realtime/`, `backend/internal/httpapi/management/auth/realtime.go`, `backend/tests/runtime/realtime_test.go`, and `backend/testdata/realtime/`.
- Backend wiring/lane cleanup: updated runtime telemetry outbox/service options, auth service/routes, platform HTTP/lifecycle assembly, DB pools, bootstrap config parsing, default pool budget, tests, and removed `github.com/gorilla/websocket`.
- Proxy/docs/contracts: removed nginx websocket proxying, Vite websocket proxying, updated README, durable docs, AGENTS files, i18n catalogs, and dashboard/statistics contract tests.

Verification:
- `rg -in "realtime|websocket" backend/internal frontend/src --glob '!**/AGENTS.md'`: remaining matches are only `backend/internal/platform/config` parsed-but-unused `database.pools.realtime` compatibility fields/tests required by G2; no frontend source matches.
- `rg -n "/api/realtime/ws" . --glob '!docs/TEST_REDUCTION_*.md' --glob '!docs/IMPLEMENTATION_PLAN.md' --glob '!*.log' --glob '!node_modules/**' --glob '!frontend/dist/**'`: no matches.
- `rg -n "gorilla/websocket|WebSocketStatusIndicator|useRealtimeData|frontend/src/lib/websocket|PostgresLaneRealtime|DashboardUpdates|AnalyticsUpdates|RealtimeService" backend frontend docker docs README.md AGENTS.md --glob '!docs/TEST_REDUCTION_*.md' --glob '!docs/IMPLEMENTATION_PLAN.md' --glob '!frontend/dist/**'`: no matches.
- `cd frontend && pnpm run test`: PASS, 11 files / 31 tests.
- `cd frontend && pnpm run test:lib`: PASS, 87 tests.
- `cd frontend && pnpm run test:server`: PASS, 4 tests.
- `cd frontend && pnpm run lint`: PASS.
- `cd frontend && pnpm run build`: PASS; Vite reported existing large chunk warnings.
- `cd backend && go build ./cmd/prism-backend`: PASS.
- `cd backend && go test ./internal/platform/config ./internal/platform/db ./internal/platform/http ./internal/platform/lifecycle ./internal/httpapi/management/auth ./internal/httpapi/runtime`: PASS.
- `cd backend && go test ./tests/priority/db`: PASS.
- `cd backend && go test ./tests/priority/unit ./tests/priority/scheduler`: PASS.
- `cd backend && go test ./tests/contract`: FAIL before assertions due local Docker/Postgres harness readiness: `postgres container on port 33255 did not become ready in time`.
- `cd backend && go test ./tests/runtime`: FAIL before assertions due local Docker/Postgres harness issue: `no public port '5432/tcp' published for prism-s14-runtime-8caaaf70`.
- `cd backend && go test ./tests/priority/...`: FAIL in existing priority cache source-string guard, `runtime cache generation implementation missing "LoadFreshActiveRuntimePlan"`; unrelated to the realtime retirement paths touched here.
- Attempts to run the full Docker-backed backend suite and the full integration package were interrupted after the local harness stalled without further output.

Concerns:
- Backend Docker-backed suites could not run to assertions in this environment because the local Postgres harness did not become ready or did not publish the expected port.
- The full priority suite still has an unrelated cache guard mismatch around `LoadFreshActiveRuntimePlan`; focused DB/scheduler/unit priority checks for this task pass.
