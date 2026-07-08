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

## Task 9 Stale Profile Wording Fix

STATUS: DONE_WITH_CONCERNS

Commit:
- `docs: align profile freeze test wording`

Fixes:
- Reworded the frontend design-system chrome note to Default-profile/scope language.
- Updated the contract test needles to the frozen Default profile id `1` / ignored-override wording.
- Renamed the runtime cache invalidation assertion message to Default-profile wording.
- Aligned the workflow reference line to the frozen Default profile id `1` runtime contract.

Verification:
- `rg -n 'active profile|selected profile|selected-profile chrome|selected-profile planning' frontend/DESIGN.md backend/tests/contract/s2_shell_test.go backend/internal/platform/http/server_test.go docs/API_SPEC.md docs/ARCHITECTURE.md docs/DATA_MODEL.md docs/SMOKE_TEST_PLAN.md frontend/README.md`
- `cd backend && go test ./internal/platform/http`
- `cd backend && go test ./tests/contract -run TestNormativeDocsParity` -> failed before assertions: `postgres container on port 33227 did not become ready in time`
- `cd frontend && pnpm run build`

Concerns:
- Docker-backed backend contract verification is blocked by the local Postgres harness readiness timeout before the doc parity assertions run.

## Task 9 Stale E2E Profile Switcher Fix

STATUS: DONE

Commit:
- `test: remove stale profile switcher e2e expectations`

Fixes:
- Removed the stale `shell-profile-switcher` visibility assertions from the protected shell sidebar e2e coverage and kept the mobile drawer checks pinned to the no-switcher contract.
- Rewrote the dashboard reporting currency e2e to assert the frozen Default profile id `1` header and keep the currency pinned to the Default profile route.
- Reworked the reporting-currency-provider e2e to verify Default profile id `1` bootstrap behavior only and dropped the deleted Blue Team switch path.

Verification:
- `rg -n 'shell-profile-switcher|Blue Team|X-Profile-Id.*2|profile switch|switcher' frontend/tests/e2e`
- `cd frontend && pnpm run test:lib`
- `cd frontend && pnpm run build`

Concerns:
- Correction: the touched e2e specs were run directly after this section was written:
  `cd frontend && pnpm run test:e2e -- frontend/tests/e2e/protected-shell-sidebar.spec.ts frontend/tests/e2e/dashboard-reporting-currency.spec.ts frontend/tests/e2e/reporting-currency-provider.spec.ts` passed, 9 tests total.

## Task 9 Runtime Freeze Fix

STATUS: DONE_WITH_CONCERNS

Commit:
- `7f3537e0`

Fixes:
- Pinned runtime cache snapshot publication to frozen Default profile id `1` and stopped resolving runtime planning through the active-profile helper.
- Renamed the runtime cache accessors to frozen Default wording and added the `ponytail:` pin comment at the DB lookup site.
- Added a runtime regression that proves a non-default active profile does not steer runtime planning away from Default id `1`.
- Reworded the remaining durable docs and test helper strings that still described active-profile runtime behavior.

Verification:
- `rg -n 'active profile|active-profile|ResolveActiveProfile|LoadPublishedActiveProfile|published active' backend/internal/httpapi/runtime backend/internal/profiledomain backend/tests/runtime docs/DATA_MODEL.md docs/ARCHITECTURE.md docs/REQUESTS_PAGE.md` -> no matches.
- `cd backend && go test ./internal/httpapi/runtime ./internal/profiledomain` -> passed.
- `cd backend && go test ./tests/runtime -run 'Profile|Cache|Planning'` -> failed before assertions because the local Docker Postgres harness did not publish `5432/tcp`.
- `cd backend && go build ./cmd/prism-backend` -> passed.

Concerns:
- Docker-backed runtime regression coverage is still blocked by the local Postgres harness port publication issue before the assertions run.

## Task 9 R4 Final Review Fix

STATUS: DONE

Commit:
- `fix: finish frozen profile protocol cleanup`

Fixes:
- Froze frontend realtime subscribe and refresh builders to emit `profile_id: 1` regardless of caller input, and updated the websocket contract assertions to expect the frozen Default profile id.
- Pinned the reporting-currency request-log fixtures to Default profile id `1` instead of a non-default fixture id.
- Updated the loadbalance strategy API example to `profile_id: 1`.
- Froze `rewriteProfileScopeSchema` to Default profile id `1`, removed the stale selected-profile nav assertion from the rewrite harness, and updated the app AGENTS wording to match the frozen contract.

Verification:
- `rg -n 'profile_id.: (2|3|42)|profile_id.*(2|3|42)|buildSubscribeMessage\\(7|buildRefreshMessage\\(7|rewriteProfileScopeSchema|selected-profile nav|selected-profile routing|profileId: crypto|profileId.*42' frontend/src/lib/websocket/protocol.ts frontend/tests/lib/websocket_contract.test.mjs frontend/tests/e2e/models-request-logs-reporting-currency.spec.ts docs/API_SPEC.md frontend/src/app/AGENTS.md frontend/src/app/forms/rewriteProfileScopeForm.ts frontend/src/test/rewrite-harness.test.tsx`
  - Remaining matches are the intentional `buildSubscribeMessage(7, ...)` / `buildRefreshMessage(7, ...)` test call sites plus the frozen `rewriteProfileScopeSchema` contract at `profileId: 1`.
- `cd frontend && pnpm run test:lib` -> passed, 105/105.
- `cd frontend && pnpm exec vitest run src/test/rewrite-harness.test.tsx` -> passed.
- `cd frontend && pnpm run build` -> passed with the existing Vite chunk-size warning.

Concerns:
- None beyond the pre-existing unrelated dirty/untracked docs that were left untouched per instruction.

## Task 9 Realtime Freeze Review Fix

STATUS: DONE

Commit:
- `3eca3de5` (`fix: freeze realtime profile client contract`)

Fixes:
- Froze `frontend/src/lib/websocket.ts` to Default profile id `1` and removed client-side profile switching.
- Updated websocket contract tests to expect `profile_id: 1` for realtime subscribe and refresh traffic, including reconnect behavior.
- Reworded `docs/ARCHITECTURE.md` and `frontend/tests/e2e/AGENTS.md` to the frozen Default-profile realtime contract.
- Updated the realtime contract fixtures in `frontend/tests/lib/analytics_websocket_contract.test.mjs` and `frontend/tests/lib/dashboard_realtime_reconnect_contract.test.mjs` to use Default id `1`.

Verification:
- `rg -n 'setProfile|profile-scope|profile browser|profile_id|profile-scoped|profile scoped|dynamic profile|profileId.*7|profileId.*9' frontend/src/lib/websocket.ts frontend/tests/lib/websocket_contract.test.mjs docs/ARCHITECTURE.md frontend/tests/e2e/AGENTS.md`
- `node --test frontend/tests/lib/websocket_contract.test.mjs`
- `node --test frontend/tests/lib/analytics_websocket_contract.test.mjs`
- `node --test frontend/tests/lib/dashboard_realtime_reconnect_contract.test.mjs`
- `cd frontend && pnpm run test:lib`
- `cd frontend && pnpm run build`

Concerns:
- The only remaining `profile_id` mentions in the touched contract tests are the frozen Default id `1` fixtures and message payloads required by the contract.
- The unrelated pre-existing dirty/untracked docs named in the task were left untouched.

## Task 9 Stale README/i18n Profile References Fix

STATUS: DONE

Commit:
- `docs: remove stale profile login copy`

Fixes:
- Reworded `backend/README.md` to describe execution against frozen Default profile id `1`.
- Reworded root `README.md` to say load-balance defaults are created from the frozen Default profile id `1`.
- Removed profile management from the login copy in `frontend/src/i18n/messages/en.ts` and `frontend/src/i18n/messages/zh-CN.ts`.

Verification:
- `rg -n 'active-profile model|active profile|selected profile|manage .*profiles|profiles,|档案|配置档案' README.md backend/README.md frontend/src/i18n/messages/en.ts frontend/src/i18n/messages/zh-CN.ts`
- `cd frontend && pnpm run build`
- `cd backend && go build ./cmd/prism-backend`

Concerns:
- Remaining matches in the broader repo were not touched unless they already stated the frozen Default profile id `1` contract.

## Task 9 Runtime Planning Freeze Fix

STATUS: DONE_WITH_CONCERNS

Commit:
- `1545b823` (`fix: limit runtime planning refresh to Default`)

Fixes:
- Pinned runtime snapshot refreshes to the frozen Default profile id `1` for bootstrap and fresh runtime-plan loads.
- Stopped runtime planning refreshes from listing or building every non-deleted profile; only Default id `1` is rebuilt when planning refresh work runs.
- Added a focused runtime unit test that fails if a planning-all refresh tries to enumerate non-default profiles.
- Reworded the routing and audit/loadbalance user-facing copy in `frontend/src/i18n/messages/en.ts` and `frontend/src/i18n/messages/zh-CN.ts` to Default-profile wording.

Verification:
- `rg -n 'PlanningAll|ListProfilesForPlanning|selected profile|current profile|当前配置档案|所选配置档案|当前档案|所选档案' backend/internal/httpapi/runtime frontend/src/i18n/messages/en.ts frontend/src/i18n/messages/zh-CN.ts`
- `cd backend && go test ./internal/httpapi/runtime ./internal/profiledomain`
- `cd backend && go build ./cmd/prism-backend`
- `cd frontend && pnpm run build`

Concerns:
- The remaining `PlanningAll` matches are internal refresh flags, merge logic, and the new regression test; runtime planning itself now stays on Default id `1`.

## Task 9 R4 Final Review Fix

STATUS: DONE_WITH_CONCERNS

Commit:
- `aba55dd6fe5efabf4c1c9d4e981d9c9ddb47bc60` (`fix: freeze realtime and profile copy`)

Fixes:
- Froze `/api/realtime/ws` on Default profile id `1` by ignoring inbound `profile_id` values and returning/broadcasting the pinned Default id.
- Updated the realtime API spec examples and text to describe the Default-profile websocket contract.
- Reworded the loadbalance pricing-template copy, e2e expectations, and pricing-templates AGENTS guidance to say Default profile instead of this profile / selected profile.

Verification:
- `rg -n 'profile_id.: 2|profile_id.*2|this profile|selected-profile|selected profile' backend/internal/httpapi/realtime docs/API_SPEC.md frontend/src/i18n/messages/en.ts frontend/src/i18n/messages/zh-CN.ts frontend/tests/e2e/models-access-target-authoring.spec.ts frontend/tests/e2e/loadbalance-strategies-recovery.spec.ts frontend/src/pages/pricing-templates/AGENTS.md` -> no matches.
- `cd backend && go test ./internal/httpapi/realtime` -> passed.
- `cd frontend && pnpm run test:e2e -- frontend/tests/e2e/models-access-target-authoring.spec.ts frontend/tests/e2e/loadbalance-strategies-recovery.spec.ts` -> failed with unrelated model-dialog expectations:
  - `main model dialog creates default strategy from empty loadbalance strategy state`: expected `access_targets: []` in the created payload, but the received payload omitted that field.
  - `main model dialog saves targetless disabled drafts` and `main model dialog keeps connection option absent while authoring ordered model access targets`: `No model targets selected.` was not visible.
- `cd frontend && pnpm run build` -> passed.

Concerns:
- The targeted e2e run still has unrelated model-dialog failures that predate this realtime/copy freeze work.
