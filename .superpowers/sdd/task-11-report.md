STATUS: DONE_WITH_CONCERNS

COMMITS
- feat!: remove OTel telemetry path

FILES CHANGED
- Removed OTel/exporter and async metric sources under backend/internal/platform/telemetry, backend/internal/platform/asyncmetrics, backend/internal/platform/db/telemetry.go, backend/internal/pgxutil/telemetry.go, backend/internal/platform/http/telemetry.go, backend/internal/httpapi/runtime/runtime_tracing.go, and backend/internal/httpapi/management/auth/telemetry.go.
- Removed OTel provider lifecycle wiring from backend/cmd/prism-backend/main.go and backend/internal/platform/lifecycle.
- Removed telemetry middleware/span/metric/asyncmetrics calls from platform HTTP, scheduler, management jobs, log retention, management side effects, auth proxy-key usage, runtime feedback, runtime side effects, runtime telemetry outbox, runtime execution, gateway context, and pgx transaction helpers.
- Kept runtime_telemetry database lane and runtime telemetry outbox request-log/usage-event behavior; telemetry_outbox.go changes are limited to removing tracing/asyncmetrics calls and imports.
- Preserved startup telemetry config parsing and added the required ponytail parsed-but-unused comment on TelemetryConfig.
- Removed OTel modules from backend/go.mod/go.sum with go mod tidy.
- Updated README, backend/README.md, docs/API_SPEC.md, docs/ARCHITECTURE.md, and the affected AGENTS.md files.
- Deleted or updated tests that asserted the old OTel source shape.

TESTS AND COMMANDS
- PASS: rg -in "opentelemetry|asyncmetrics|startRuntimeSpan" backend --glob '!docs/**' produced no matches (rg exit 1 for no matches).
- PASS: grep -c opentelemetry backend/go.mod printed 0 (grep exit 1 for zero matches).
- PASS: cd backend && go test ./internal/httpapi/runtime ./internal/platform/http ./internal/platform/lifecycle ./internal/platform/config ./internal/httpapi/management/auth.
- PASS: cd backend && go build ./cmd/prism-backend.
- PASS: cd backend && go test ./tests/priority/integration.
- DONE_WITH_CONCERNS: cd backend && go test ./tests/contract failed before assertions because the local Docker Postgres harness did not publish 5432/tcp for prism-s5-7e690b43.
- DONE_WITH_CONCERNS: cd backend && go test ./tests/runtime failed before assertions because the local Postgres harness container on port 33236 did not become ready in time.
- DONE_WITH_CONCERNS: cd backend && go test ./internal/... ran non-Docker packages successfully, but Docker-backed internal packages failed before assertions with the same local Postgres harness issue: missing published 5432/tcp or containers not becoming ready in time.

CONCERNS
- Additional Docker-backed regression suites could not complete in this local environment because the known Postgres harness issue prevented tests from reaching assertions.
- Existing unrelated local changes were left untouched: .superpowers/sdd/task-9-report.md, docs/IMPLEMENTATION_PLAN.md, and docs/TEST_REDUCTION_*.md.
