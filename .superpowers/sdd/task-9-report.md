STATUS: DONE_WITH_CONCERNS

Commits created:
- `7d89b3af15e7afa925848fc66aae582376b63666` (`feat!: freeze multi-profile to pinned Default (id=1)`)

Files changed:
- Backend profile pinning: `backend/internal/profiledomain/scope.go`
- Backend management route removal/wiring: `backend/internal/httpapi/management/profiles/*`, `backend/internal/platform/http/*`, `backend/internal/platform/lifecycle/production.go`
- Backend tests/harness: `backend/tests/contract/*`, `backend/tests/runtime/*`
- Frontend profile UI/provider/client deletion and pinned header: `frontend/src/context/*`, `frontend/src/components/layout/app-layout/*`, `frontend/src/lib/api*`, route/page/features/hooks/tests
- Frontend profile types/query cleanup: `frontend/src/lib/types*`, `frontend/src/shared/api/queryKeys.ts`
- Docs/i18n/AGENTS: `docs/{AGENTS.md,API_SPEC.md,ARCHITECTURE.md,DATA_MODEL.md}`, backend/frontend AGENTS, `frontend/src/i18n/messages/{en.ts,zh-CN.ts}`

Requirement checklist:
- Preserved all backend `profile_id` columns/FKs/indexes and kept `backend/internal/profiledomain/`.
- Kept `ResolveEffectiveProfile` signature unchanged, ignored headers, loaded Default id `1`, and added the required `ponytail:` comment.
- Kept frontend `X-Profile-Id: 1` emission in `frontend/src/lib/api/core.ts`; exactly one `X-Profile-Id` occurrence remains there.
- Deleted backend management profiles package/routes and frontend profile switcher/provider/client surfaces.
- Extracted runtime harness into `backend/tests/runtime/runtime_harness_test.go`, moved proxy selector coverage, and shrank `profile_scope_test.go` to the pinned Default regression.
- Kept `frontend/tests/lib/profile_scope_header_contract.test.mjs`; deleted the profile selection contract test and profile e2e scope tests.
- Updated management route contract JSON after removing profile activation.
- Updated docs, i18n, and AGENTS for frozen Default-profile behavior.
- Did not edit/stage the pre-existing dirty/untracked test-reduction docs.

Verification commands:
- `rg 'management/profiles' backend` -> exit 1, no output.
- `rg 'ProfileSwitcher|ProfileContext|setApiProfileId' frontend/src` -> exit 1, no output.
- `rg -c 'X-Profile-Id' frontend/src/lib/api/core.ts` -> exit 0, output `1`.
- `wc -l backend/tests/runtime/profile_scope_test.go` -> exit 0, output `49`.
- `cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...` -> exit 1, failed on local Docker Postgres harness readiness/port: `docker port prism-s5-db6c98fc failed: exit status 1; no public port '5432/tcp' published`.
- `cd backend && go test ./tests/priority/... ./internal/platform/http ./internal/profiledomain ./internal/platform/lifecycle && go build ./cmd/prism-backend` -> exit 0.
- `cd frontend && pnpm run build && pnpm run lint && pnpm run test && pnpm run test:lib && pnpm run test:server` -> exit 0. Build succeeded with existing Vite chunk-size warnings; Vitest 31/31 passed, node lib 105/105 passed, server 4/4 passed.
- `git diff --check -- . ':(exclude)docs/IMPLEMENTATION_PLAN.md' ':(exclude)docs/TEST_REDUCTION_HANDOFF.md' ':(exclude)docs/TEST_REDUCTION_PLAN.md' ':(exclude)docs/TEST_SUITE_REDUCTION.md'` -> exit 0.

Self-review notes and concerns:
- Concern: full backend Docker-backed suites could not complete because the local Postgres harness container did not publish `5432/tcp`; focused non-Docker/package verification and backend build passed.
- The frontend still retains internal route/query scope labels named `selected-profile` where they are generic cache/shell scope tokens; user-facing switching and providers were removed.

## Review Fix Follow-up

STATUS: DONE_WITH_CONCERNS

Commit:
- This commit: `fix: finish profile freeze cleanup`

Fixes:
- Rewrote model and endpoint contract coverage so missing `X-Profile-Id` succeeds against Default profile id `1`.
- Rewrote S11 audit settings coverage so a non-default profile header is ignored for reads and writes, with persisted rows staying on Default profile id `1`.
- Pinned runtime cache invalidation planning IDs to Default profile id `1`; route-contract guard now expects `1` even when the raw header is `42`.
- Updated durable docs and request-page supporting prose to the frozen Default-profile contract, removed stale selected-profile/profile-route wording, and changed the stale API error example away from `profile_scope_header_missing`.
- Changed the visible shell scope badge from `Selected profile` to `Default profile`.
- Removed stale `/api/profiles/bootstrap` e2e mocks and deleted now-unused profile fixture declarations.

Verification:
- `rg -n 'profile_scope_header_missing|/api/profiles|Selected profile|selected profile|profiles/bootstrap' docs frontend/src frontend/tests backend/tests/contract backend/internal/platform/http --glob '!docs/DEVELOPMENT_DIRECTION.md'` -> remaining matches only in untouched plan docs (`docs/IMPLEMENTATION_PLAN.md`, `docs/TEST_SUITE_REDUCTION.md`) or deliberate removed-route/frozen-contract docs (`docs/API_SPEC.md`, `docs/SMOKE_TEST_PLAN.md`).
- `cd backend && go test ./internal/platform/http ./tests/contract -run 'Profile|Model|Endpoint|S11|Route'` -> `internal/platform/http` passed; `tests/contract` failed before assertions: `postgres container on port 33225 did not become ready in time`.
- `cd backend && go test ./internal/platform/http` -> passed.
- `cd frontend && pnpm run build && pnpm run lint && pnpm run test:lib` -> passed; build kept existing Vite chunk-size warnings.
- `rg 'management/profiles' backend` -> exit `1`, no output.
- `rg 'ProfileSwitcher|ProfileContext|setApiProfileId' frontend/src` -> exit `1`, no output.
- `rg -c 'X-Profile-Id' frontend/src/lib/api/core.ts` -> `1`.
- `wc -l backend/tests/runtime/profile_scope_test.go` -> `49`.

Concerns:
- Docker-backed backend contract verification remains blocked by the local Postgres harness readiness/port issue before test assertions.
- The required stale-reference rg still reports pre-existing plan docs that the task explicitly said not to touch.

## Review Fix Final

STATUS: DONE_WITH_CONCERNS

Commit:
- `1c65a86c` (`fix: finish profile freeze cleanup`)
- Range: `7d89b3af..1c65a86c`

Fixes:
- Backend contract tests now assert missing `X-Profile-Id` resolves to Default profile id `1` and succeeds for model/endpoint routes.
- S11 audit settings coverage now asserts non-default headers are ignored and reads/writes stay on Default profile id `1`.
- Runtime cache invalidation planning is pinned to Default profile id `1`; the route guard expects `1` instead of raw header `42`.
- Durable docs now describe frozen Default-profile behavior and removed `/api/profiles*`/selected-profile UI behavior.
- Shell scope badge visible text now says `Default profile`.
- Stale `/api/profiles/bootstrap` e2e mocks were removed from remaining tests.

Verification:
- `rg -n 'profile_scope_header_missing|/api/profiles|Selected profile|selected profile|profiles/bootstrap' docs frontend/src frontend/tests backend/tests/contract backend/internal/platform/http --glob '!docs/DEVELOPMENT_DIRECTION.md'` -> remaining matches are acceptable: untouched plan docs (`docs/IMPLEMENTATION_PLAN.md`, `docs/TEST_SUITE_REDUCTION.md`) and deliberate removed-route/frozen-contract docs (`docs/API_SPEC.md`, `docs/SMOKE_TEST_PLAN.md`).
- `cd backend && go test ./internal/platform/http ./tests/contract -run 'Profile|Model|Endpoint|S11|Route'` -> `internal/platform/http` passed; `tests/contract` failed before assertions: `postgres container on port 33226 did not become ready in time`.
- `cd backend && go test ./internal/platform/http` -> passed.
- `cd frontend && pnpm run build && pnpm run lint && pnpm run test:lib` -> passed; build kept existing Vite chunk-size warnings.
- `rg 'management/profiles' backend` -> exit `1`, no output.
- `rg 'ProfileSwitcher|ProfileContext|setApiProfileId' frontend/src` -> exit `1`, no output.
- `rg -c 'X-Profile-Id' frontend/src/lib/api/core.ts` -> `1`.
- `wc -l backend/tests/runtime/profile_scope_test.go` -> `49`.

Concerns:
- Docker-backed backend contract verification remains blocked by the local Postgres harness readiness/port issue before test assertions.
- The required stale-reference rg still reports pre-existing plan docs that the task explicitly said not to touch.

## Task 9 Doc Fix Follow-up

STATUS: DONE

Commit:
- `docs: finish profile freeze guidance`

Fixes:
- Removed the last stale `/api/profiles` wording from `docs/SMOKE_TEST_PLAN.md` and kept the Default-profile smoke coverage.
- Kept `docs/REQUESTS_PAGE.md`, `docs/TEST_CASE_GENERATION_METHODOLOGY.md`, and the management package AGENTS files aligned to frozen Default profile id `1` with ignored `X-Profile-Id`.

Verification:
- `rg -n 'selected profile|selected-profile|profile lifecycle|profile activation|Profiles A/B/C|useProfileContext|activate profile|/api/profiles' docs/REQUESTS_PAGE.md docs/SMOKE_TEST_PLAN.md docs/TEST_CASE_GENERATION_METHODOLOGY.md backend/internal/httpapi/management/settings/AGENTS.md backend/internal/httpapi/management/stats/AGENTS.md backend/internal/httpapi/management/models/AGENTS.md backend/internal/httpapi/management/endpoints/AGENTS.md` -> no matches.
- `cd frontend && pnpm run build` -> passed.
- `cd backend && go test ./internal/profiledomain ./internal/platform/http` -> passed.

Concerns:
- None beyond the pre-existing unrelated dirty docs that were left untouched per instruction.

## Task 9 AGENTS Fix Follow-up

STATUS: DONE

Commit:
- `docs: finish profile freeze AGENTS guidance`

Fixes:
- Rewrote the remaining AGENTS docs that still described selected-profile or profile-switching behavior.
- Froze management guidance on Default profile id `1`, with `X-Profile-Id` treated as compatibility-only and ignored.
- Kept storage `profile_id` columns referenced where the owning package still persists them.

Verification:
- `rg -n 'selected profile|selected-profile|active profile|profile switch|profile activation|activate profile|selectedProfile|ProfileSwitcher' --glob 'AGENTS.md' /Users/qingli/Documents/proj/prism` -> no matches.
- `cd backend && go test ./internal/profiledomain ./internal/platform/http` -> passed.
- `cd frontend && pnpm run build` -> passed.

Concerns:
- The pre-existing dirty/untracked docs named in the task were left untouched.

## Task 9 Stale Profile Guidance Fix

STATUS: DONE

Commit:
- `docs: finish profile freeze stale references`

Fixes:
- Replaced the stale `expected_active_profile_id` guidance in `frontend/src/lib/AGENTS.md` with the frozen Default profile id `1` contract.
- Updated `backend/internal/httpapi/management/configrules/AGENTS.md` to state that profiles are frozen on Default id `1` and `/api/profiles*` CRUD is removed.
- Reworded the dashboard routing contract fixture to frozen Default-profile language without changing test behavior.

Verification:
- `rg -n 'expected_active_profile_id|selected-profile routing topology|selected profile|selected-profile|profile CRUD|profile lifecycle|/api/profiles' frontend/src/lib/AGENTS.md backend/internal/httpapi/management/configrules/AGENTS.md frontend/tests/lib/dashboard_routing_flow_layout_contract.test.mjs`
- `cd frontend && pnpm run test:lib`
- `cd frontend && pnpm run build`

Concerns:
- The pre-existing dirty/untracked docs named in the task were left untouched.

## Task 9 Remaining Doc Fix

STATUS: DONE

Commit:
- `docs: finish profile freeze durable docs`

Fixes:
- Rewrote the remaining durable docs in `docs/API_SPEC.md`, `docs/ARCHITECTURE.md`, `docs/DATA_MODEL.md`, and `docs/SMOKE_TEST_PLAN.md` to use frozen Default profile id `1` wording.
- Kept `X-Profile-Id` compatibility language explicit while stating that the backend ignores it and `profile_id` columns remain storage attribution only.
- Updated `frontend/README.md` to remove the stale selected-profile/navigation ownership references and describe the current no-switcher shell structure.

Verification:
- `rg -n 'active profile|selected profile|selected-profile|profile navigation|navigationProfileConfig|profile lifecycle|X-Profile-Id.*active|active.*X-Profile-Id' docs/API_SPEC.md docs/ARCHITECTURE.md docs/DATA_MODEL.md docs/SMOKE_TEST_PLAN.md frontend/README.md`
- `cd frontend && pnpm run build`
- `cd backend && go test ./internal/profiledomain ./internal/platform/http`

Concerns:
- The unrelated pre-existing dirty/untracked docs were left untouched per instruction.
