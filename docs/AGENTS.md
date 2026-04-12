# DOCS REFERENCE MAP

## OVERVIEW
`docs/` holds Prism's normative architecture, API, and data-model docs, plus supporting references and archive material. Active working plans live outside `docs/` under `../.sisyphus/plans/`.

## STRUCTURE
```text
docs/
├── AGENTS.md
├── ARCHITECTURE.md
├── API_SPEC.md
├── DATA_MODEL.md
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
- `PRD.md`, `REQUESTS_PAGE.md`, `SMOKE_TEST_PLAN.md`, `WORKFLOWS.md`, and `TEST_CASE_GENERATION_METHODOLOGY.md` are supporting references.
- `archive/` holds finished notes and retained evidence only.
- Active working plans belong in `../.sisyphus/plans/`, not under `docs/`.

## WHERE TO LOOK
- Launcher and release facts: `../README.md`, `../start.sh`, `../release.sh`, `../.env.example`
- Backend/frontend version surfaces: `../backend/VERSION`, `../backend/pyproject.toml`, `../frontend/VERSION`, `../frontend/package.json`
- Backend and frontend ownership boundaries inside the monorepo: `../backend/AGENTS.md`, `../frontend/AGENTS.md`
- Frontend request-log context: `REQUESTS_PAGE.md`
- Operator workflow map grounded in the mounted route and API surface: `WORKFLOWS.md`
- Test-generation workflow: `TEST_CASE_GENERATION_METHODOLOGY.md`
- Active working plans outside docs: `../.sisyphus/plans/`
- Archive boundary rules: `archive/AGENTS.md`, `archive/`

## CONVENTIONS
- Keep docs Prism-specific.
- Point to child AGENTS files instead of repeating leaf detail.
- Keep launcher facts aligned with `../start.sh`, especially `.env` loading, `headless|full`, ports, PostgreSQL checks, `VITE_API_BASE`, and local CORS/WebAuthn wiring.
- Keep release facts aligned with `../release.sh` and the version surfaces it updates.
- State CI facts accurately: `.github/workflows/docker-images.yml` builds monorepo images for `linux/arm64` on `v*` tags and selected PR path changes, and `.github/workflows/cleanup.yml` handles cleanup only.
- Keep active plans out of `docs/`. Use `../.sisyphus/plans/` while work is in flight, and move only finished notes or retained evidence into `archive/`.
- Keep archive wording tight: finished notes first, optional evidence only when needed, never treat archive notes as canonical docs.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not add generic FastAPI, React, Vite, or uv explainers.
- Do not invent CI jobs, unsupported routes, unsupported providers, or extra compose files.
- Do not reintroduce any live-plan sink under `docs/`.
- Do not treat archived notes as the source of truth when a live doc or child AGENTS file already owns the topic.
- Do not leave active implementation details stranded only in `docs/` when the owning backend or frontend AGENTS tree should carry the implementation map.
