# DOCS REFERENCE MAP

## OVERVIEW
`docs/` holds Prism's normative architecture, API, and data-model docs, plus supporting references, the checked-in OpenAPI artifact, and archive material. Active working plans live outside `docs/` under `../.sisyphus/plans/`.

## STRUCTURE
```text
docs/
├── AGENTS.md
├── ARCHITECTURE.md
├── API_SPEC.md
├── DATA_MODEL.md
├── openapi.json
├── PRD.md
├── REQUESTS_PAGE.md
├── SMOKE_TEST_PLAN.md
├── WORKFLOWS.md
├── TEST_CASE_GENERATION_METHODOLOGY.md
└── archive/
    └── AGENTS.md
```

## OWNERSHIP
- `ARCHITECTURE.md`, `API_SPEC.md`, and `DATA_MODEL.md` are the source of truth.
- `openapi.json` is the checked-in management and health contract artifact served by the backend; keep it aligned with backend ownership docs instead of treating it as the narrative source of truth.
- `PRD.md`, `REQUESTS_PAGE.md`, `SMOKE_TEST_PLAN.md`, `WORKFLOWS.md`, and `TEST_CASE_GENERATION_METHODOLOGY.md` are supporting references.
- `archive/` currently contains only the boundary file and holds finished notes and retained evidence only.
- Archived run notes use `docs/archive/YYYY-MM-DD-llm-test-run-<scope>.md`.
- Active working plans belong in `../.sisyphus/plans/`, not under `docs/`.

## WHERE TO LOOK
- Launcher, release, and deploy facts: `../README.md`, `../start.sh`, `../release.sh`, `../deploy.sh`, `../backend/.env.example`, `../frontend/.env.example`
- Backend/frontend version surfaces: `../backend/VERSION`, `../frontend/VERSION`, `../frontend/package.json`
- Checked-in OpenAPI artifact: `openapi.json`, `../backend/AGENTS.md`
- Backend and frontend ownership boundaries inside the monorepo: `../backend/AGENTS.md`, `../frontend/AGENTS.md`
- Product and request-log context: `PRD.md`, `REQUESTS_PAGE.md`
- Operator workflow map grounded in the mounted route and API surface: `WORKFLOWS.md`
- Test-generation workflow: `TEST_CASE_GENERATION_METHODOLOGY.md`
- Active working plans outside docs: `../.sisyphus/plans/`
- Archive boundary rules: `archive/AGENTS.md`, `archive/` (currently boundary-only)

## CONVENTIONS
- Keep docs Prism-specific.
- Point to child AGENTS files instead of repeating leaf detail.
- Keep launcher facts aligned with `../start.sh`, especially `.env` loading, `headless|full`, ports, PostgreSQL checks, same-origin full-mode proxying via `PRISM_VITE_PROXY_ENABLED` and `PRISM_VITE_PROXY_TARGET`, and local CORS/WebAuthn wiring.
- Keep release facts aligned with `../release.sh` and the version surfaces it updates.
- State CI facts accurately: `.github/workflows/docker-images.yml` builds monorepo images for `linux/arm64` on path-filtered `main` pushes, path-filtered PRs, `v*` tags, and `workflow_dispatch`, and `.github/workflows/cleanup.yml` handles cleanup only.
- Keep active plans out of `docs/`. Use `../.sisyphus/plans/` while work is in flight, and move only finished notes or retained evidence into `archive/`.
- Keep archive wording tight: finished notes first, optional evidence only when needed, never treat archive notes as canonical docs.
- Keep archived test run notes on the `docs/archive/YYYY-MM-DD-llm-test-run-<scope>.md` pattern.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not add generic framework or tool explainers.
- Do not invent CI jobs, unsupported routes, unsupported providers, or extra compose files.
- Do not reintroduce any live-plan sink under `docs/`.
- Do not treat archived notes as the source of truth when a live doc or child AGENTS file already owns the topic.
- Do not leave active implementation details stranded only in `docs/` when the owning backend or frontend AGENTS tree should carry the implementation map.
