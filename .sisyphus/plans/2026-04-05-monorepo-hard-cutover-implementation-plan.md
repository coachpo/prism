# Prism monorepo hard-cutover implementation plan

## Status

Drafted by Prometheus on 2026-04-05 from repo-local evidence only.
Approved by Momus on 2026-04-05.

## Requirement summary

- Convert Prism from a root repo with `backend/` and `frontend/` submodules into a single monorepo.
- Keep `backend/` and `frontend/` at the same root-relative paths.
- Import backend and frontend history into those existing paths.
- Remove submodule plumbing completely.
- Rewrite release, CI, and live docs for the monorepo contract.
- Do not preserve backward compatibility.

## Worktree and revision context

- Root worktree: `/Users/liqing/Documents/PersonalProjects/prism-workspace/model-share-feature-monorepo-cutover`
- Root branch: `feature/monorepo-cutover`
- Root base ref: `origin/main`
- Root base commit used for worktree creation: `c656826001dd046c9dbc3ab28749dfa0120e1459`
- Pre-cutover local backup tag: `pre-monorepo-cutover-base-c656826`
- Root `.env` copied into worktree: yes
- Submodules initialized in worktree before planning: yes
- Starting backend gitlink SHA: `263e6ff25d4bbad985611e208a9a7b39313689af`
- Starting frontend gitlink SHA: `fb744427fbbc92266f473ce0bad995106f5c6822`
- Starting backend state in worktree: detached `HEAD`
- Starting frontend state in worktree: detached `HEAD`
- Starting version surfaces: root `VERSION=0.2.7`, `backend/VERSION=0.2.7`, `frontend/VERSION=0.2.7`, `frontend/package.json version=0.2.7`

## Observed repo constraints

- `.gitmodules` declares `backend` and `frontend` as submodules with separate remotes.
- `release.sh` currently requires nested backend/frontend `.git` state, validates three repos, and commits/tags/pushes backend, frontend, and root separately.
- `start.sh` hardcodes the current `backend/` and `frontend/` paths and launches PostgreSQL from `backend/docker-compose.yml` while keeping backend `18000`, frontend `15173`, and PostgreSQL `15432`.
- `.github/workflows/docker-images.yml` currently uses recursive submodule checkout, includes `.gitmodules` in PR triggers, and resolves frontend build metadata with `git -C frontend`.
- `.github/workflows/cleanup.yml` cleans backend/frontend container packages independently and only changes if tag or package policy proves it necessary.
- `README.md`, `AGENTS.md`, `docs/AGENTS.md`, `backend/AGENTS.md`, and `frontend/AGENTS.md` still describe backend/frontend as submodules or submodule-owned roots.
- The caller checkout on `/Users/liqing/Documents/PersonalProjects/prism-workspace/prism` was clean but `main` was ahead of `origin/main` by two local commits, so this worktree intentionally starts from `origin/main` to avoid mixing unrelated local-only history into the cutover.

## Goals

- Replace gitlinks with root-owned tracked trees at `backend/` and `frontend/`.
- Preserve the current on-disk paths so launcher, Docker contexts, and docs stay anchored to the same locations.
- Convert release flow to a single-repo model while keeping the current version surfaces unless implementation proves they must change.
- Convert CI to standard checkout without recursive submodules.
- Update live repo contracts so the monorepo is the only documented workflow.

## Non-goals

- No compatibility shim for old submodule-based clones, releases, or CI paths.
- No new repo layout or package renames.
- No feature work inside backend/frontend beyond what the cutover requires.
- No archive-only documentation updates as a substitute for live source-of-truth updates.

## Scope

In scope:

- history-preserving import of backend history into `backend/`
- history-preserving import of frontend history into `frontend/`
- removal of tracked `.gitmodules`
- rewrite of `release.sh`
- rewrite of `.github/workflows/docker-images.yml`
- rewrite of `README.md`, `AGENTS.md`, and `docs/AGENTS.md`
- wording updates in `backend/AGENTS.md` and `frontend/AGENTS.md`
- review and update of live docs that still describe submodule-era release or repo-shape behavior, including `docs/ARCHITECTURE.md`, `docs/SMOKE_TEST_PLAN.md`, and `docs/TEST_CASE_GENERATION_METHODOLOGY.md`

Review-only unless implementation proves otherwise:

- `.github/workflows/cleanup.yml`
- `backend/README.md`
- `frontend/README.md`
- `.env.example`

Out of scope:

- API contract changes
- database or migration changes
- package/image renames unless verification proves they are required
- mixed gitlink/tree support
- commit, rebase, or worktree cleanup during this workflow

## Binary acceptance criteria

- PASS if `backend/` and `frontend/` are ordinary tracked trees in the root repo rather than gitlinks.
- PASS if `.gitmodules` is removed from the tracked repo.
- PASS if `release.sh` no longer checks for nested backend/frontend `.git` state and no longer performs separate backend/frontend commit, tag, or push phases.
- PASS if `release.sh` still updates `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json`, and still runs backend version metadata verification plus the frontend build.
- PASS if `.github/workflows/docker-images.yml` no longer uses `submodules: recursive` and no longer treats `.gitmodules` as part of the active CI contract.
- PASS if Docker builds still use `./backend` and `./frontend` as contexts.
- PASS if live docs no longer instruct `git submodule update --init --recursive` and no longer describe backend/frontend as separately released submodules.
- PASS if `start.sh` still launches backend on `18000`, frontend on `15173`, and PostgreSQL on `15432` using `backend/docker-compose.yml`.
- PASS if `./release.sh patch --dry-run` succeeds under the monorepo contract.
- FAIL if any compatibility shim, mixed gitlink/tree support, or fallback submodule logic remains in the live contract.

## Implementation waves

### Wave 1 - baseline capture and red checks

1. Re-read the saved plan and confirm all branch, worktree, SHA, backup-tag, and version facts still match the worktree.
2. Capture baseline red checks proving the old model still exists: `.gitmodules`, recursive checkout in Docker CI, submodule language in live docs, and nested-repo logic in `release.sh`.
3. Save command output needed to compare the post-cutover state.
- QA tool: Bash + grep
- QA steps:
  - run `test -f .gitmodules`
  - run `grep -n 'submodules: recursive' .github/workflows/docker-images.yml`
  - run `grep -n 'git submodule update --init --recursive' README.md`
  - run `grep -n 'Missing backend submodule\|Missing frontend submodule\|commit_tag_and_push_backend\|commit_tag_and_push_frontend' release.sh`
- Expected result:
  - all four checks return positive matches, proving the old contract is still the live starting point before changes.

### Wave 2 - history import into existing paths

1. Import backend history into `backend/` with a history-preserving, non-interactive sequence sourced from the backend URL in `.gitmodules`.
2. Import frontend history into `frontend/` the same way.
3. Replace gitlinks with normal tracked trees at those exact paths and remove tracked `.gitmodules` only after both imports are in place.
4. Keep path-level contracts unchanged: `backend/docker-compose.yml`, backend `18000`, frontend `15173`, PostgreSQL `15432`, and Docker build contexts remain anchored where they are.
- QA tool: Bash + git inspection
- QA steps:
  - run `GIT_MASTER=1 git add -A -- .gitmodules backend frontend`
  - run `GIT_MASTER=1 git ls-files --stage backend frontend`
  - run `! GIT_MASTER=1 git ls-files --stage backend frontend | grep '^160000 '` 
  - run `test ! -e backend/.git && test ! -e frontend/.git && test ! -f .gitmodules`
- Expected result:
  - the staged index contains normal file entries under `backend/` and `frontend`, not `160000` gitlink entries
  - nested `.git` admin files are absent from `backend/` and `frontend`
  - `.gitmodules` is absent from the worktree state.

### Wave 3 - release and CI cutover

1. Rewrite `release.sh` from a three-repo orchestration script into a one-repo script.
2. Remove backend/frontend `.git` checks, separate clean-state checks, separate branch checks, and separate tag/push phases.
3. Keep the aligned version-surface rule across `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` unless implementation proves it must change.
4. Rewrite `.github/workflows/docker-images.yml` to use normal checkout, remove `.gitmodules` from active trigger paths, and replace frontend submodule SHA resolution with monorepo commit metadata.
5. Review `.github/workflows/cleanup.yml`; change it only if the monorepo tag/package contract makes the current cleanup logic wrong.
- QA tool: Bash + grep
- QA steps:
  - run `grep -n 'Backend submodule\|Frontend submodule\|backend/.git\|frontend/.git\|updated gitlinks' release.sh`
  - run `grep -n 'submodules: recursive\|\.gitmodules\|git -C frontend' .github/workflows/docker-images.yml`
  - run `./release.sh patch --dry-run`
- Expected result:
  - the first two grep commands return no matches in the rewritten files
  - the release dry run exits 0 and reports a one-repo flow rather than separate backend/frontend/root phases.

### Wave 4 - live contract rewrite

1. Rewrite `README.md` quick start, version-management, and release sections for monorepo semantics.
2. Rewrite `AGENTS.md` and `docs/AGENTS.md` so backend/frontend are monorepo-owned directories rather than submodules.
3. Rewrite `backend/AGENTS.md` and `frontend/AGENTS.md` to remove “submodule root” language.
4. Update `docs/ARCHITECTURE.md`, `docs/SMOKE_TEST_PLAN.md`, and `docs/TEST_CASE_GENERATION_METHODOLOGY.md` where they still describe submodule-era release or repo-shape behavior.
5. Update `backend/README.md`, `frontend/README.md`, or `.env.example` only if the cutover makes existing statements inaccurate.
- QA tool: grep
- QA steps:
  - run `grep -nE 'submodule|gitlinks|gitlink|git submodule update --init --recursive|submodules: recursive' README.md AGENTS.md docs/AGENTS.md docs/ARCHITECTURE.md docs/SMOKE_TEST_PLAN.md docs/TEST_CASE_GENERATION_METHODOLOGY.md backend/AGENTS.md frontend/AGENTS.md`
- Expected result:
  - the search returns no matches in the live source-of-truth files after the rewrite, aside from intentionally retained historical notes outside the live contract.

### Wave 5 - verification and signoff

1. Stage the structural repo-shape changes needed for index inspection without creating a commit.
2. Run structural git checks proving `backend/` and `frontend/` are normal tracked trees in the staged index and working tree.
3. Run stale-contract searches for submodule-era wording in live contract files.
4. Run `./release.sh patch --dry-run` at the root.
5. Run backend verification from `backend/` and frontend verification from `frontend/`.
6. Run local Docker builds for backend and frontend contexts.
7. Run `./start.sh headless` and `./start.sh full`, then perform a brief manual browser sanity check.
- QA tool: Bash, pytest, pnpm, docker, and manual browser QA
- QA steps:
  - run every command listed in `## Verification plan`
  - confirm backend docs load at `http://localhost:18000/docs`
  - confirm frontend loads at `http://localhost:15173`
- Expected result:
  - all verification commands exit 0
  - the staged index contains no `160000` gitlink entries for `backend/` or `frontend`
  - launcher ports and Docker build contexts still match the documented contract
  - frontend and backend are both reachable under the monorepo checkout.

## Verification plan

Run from the root worktree unless noted otherwise.

### Structural checks

```bash
GIT_MASTER=1 git add -A -- .gitmodules backend frontend
GIT_MASTER=1 git status --short
GIT_MASTER=1 git ls-files --stage backend frontend
GIT_MASTER=1 git ls-files --stage backend frontend | grep '^160000 ' && exit 1 || true
test ! -e backend/.git && test ! -e frontend/.git && test ! -f .gitmodules
```

### Stale-contract searches

```bash
grep -nE 'submodule|gitlinks|gitlink|git submodule update --init --recursive|submodules: recursive' README.md AGENTS.md docs/AGENTS.md docs/ARCHITECTURE.md docs/SMOKE_TEST_PLAN.md docs/TEST_CASE_GENERATION_METHODOLOGY.md backend/AGENTS.md frontend/AGENTS.md .github/workflows/docker-images.yml release.sh
```

### Release verification

```bash
./release.sh patch --dry-run
```

### Backend verification

```bash
cd backend
uv run pytest tests/test_backend_version_metadata.py
uv run pytest tests/test_smoke_defect_regressions.py -v
```

### Frontend verification

```bash
cd frontend
pnpm run test
pnpm run build
pnpm run lint
```

### Docker build verification

```bash
docker build -f backend/Dockerfile backend
docker build -f frontend/Dockerfile frontend --build-arg VITE_GIT_RUN_NUMBER=local --build-arg VITE_GIT_REVISION=$(GIT_MASTER=1 git rev-parse --short HEAD)
```

### Launcher and manual QA

```bash
./start.sh headless
./start.sh full
```

- Open `http://localhost:18000/docs`.
- Open `http://localhost:15173`.
- Confirm frontend loads and backend remains reachable.
- Confirm the visible version or revision resolves to a coherent monorepo value.

## Risks and rollback notes

- Do not stop in a mixed state where one path is a tracked tree and the other is still a gitlink.
- Frontend build metadata will intentionally shift from a frontend-submodule SHA to a monorepo SHA.
- Existing local clones with initialized submodules may require local cleanup or a fresh clone after cutover; do not add repo-level compatibility code for that case.
- If the import sequence goes wrong before verification is green, abandon or reset the cutover branch back to `pre-monorepo-cutover-base-c656826` instead of salvaging a hybrid state.
- If final verification fails after the cutover stack is in place, revert the cutover changes as a stack; do not restore submodule behavior selectively.

## Implementation guardrails

- No compatibility toggles, fallback paths, or dual submodule/monorepo support.
- Keep all edits inside the created worktree.
- If implementation materially changes this plan, update the plan file and rerun Momus before continuing.
- Stop after implementation and verification. Do not commit, rebase, or remove the worktree as part of this workflow.
