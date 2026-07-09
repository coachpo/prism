STATUS: DONE

Commits:
- `docs: archive process docs and drop doc-content test` (created by this task; final hash reported by the assistant after commit creation)

Files changed:
- Removed the dashboard/docs integration test that read `SMOKE_TEST_PLAN.md`:
  - `backend/tests/integration/dashboard_contract_docs_test.go`
- Removed the retired process docs:
  - `docs/SMOKE_TEST_PLAN.md`
  - `docs/TEST_CASE_GENERATION_METHODOLOGY.md`
- Removed live doc index/ownership references:
  - `README.md`
  - `AGENTS.md`
  - `docs/AGENTS.md`

Tests and commands:
- PASS with known unrelated matches: `rg -n "SMOKE_TEST_PLAN|TEST_CASE_GENERATION" . --glob '!docs/archive/**' --glob '!docs/DEVELOPMENT_DIRECTION.md'` now only reports the pre-existing dirty `docs/IMPLEMENTATION_PLAN.md` task brief lines.
- PASS with known unrelated matches: `rg -n "Smoke Test Plan|Test Case Generation Methodology|SMOKE_TEST_PLAN|TEST_CASE_GENERATION" . --glob '!docs/archive/**' --glob '!docs/DEVELOPMENT_DIRECTION.md'` now only reports the pre-existing dirty `docs/IMPLEMENTATION_PLAN.md` task brief lines.
- FAIL before assertions: `cd backend && go test ./tests/integration` timed out after 10m because local Docker/Postgres harness containers did not become ready. Failures included `./start.sh` reporting PostgreSQL unhealthy on `localhost:15432`, multiple integration harness ports not becoming ready, and `docker port ... 5432/tcp` reporting no published port.
- PASS: `cd backend && go build ./cmd/prism-backend`.

Resolved concerns:
- The requested integration suite originally failed before assertions because Docker had exhausted volume storage; after Docker cleanup and the startup seed fix below, the suite passes.
- Known unrelated local changes were left unstaged and untouched: `.superpowers/sdd/task-12-report.md`, `.superpowers/sdd/task-9-report.md`, `docs/IMPLEMENTATION_PLAN.md`, and untracked `docs/TEST_REDUCTION_*.md`.

Follow-up verification:
- Docker volume cleanup resolved the earlier harness capacity issue.
- A startup seed/schema mismatch was fixed by explicitly seeding inert `email_verification_attempt_count = 0` values for the remaining singleton app auth row insert paths.
- PASS: `cd backend && go test ./tests/integration -run TestStartupSeeds -count=1`.
- PASS: `cd backend && go test ./tests/integration`.
