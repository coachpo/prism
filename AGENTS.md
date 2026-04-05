<!-- Generated: 2026-04-05 | branch: main | commit: 606970c -->
# PROJECT KNOWLEDGE BASE

## OVERVIEW
Prism is a self-hosted LLM proxy gateway. This repository is a monorepo: the root owns the launcher, local release helper, docs, CI wiring, and the checked-in `backend/` and `frontend/` codebases.

## STRUCTURE
```text
prism/
├── README.md
├── VERSION
├── backend/
│   └── ...
├── frontend/
│   └── ...
├── docs/
│   ├── AGENTS.md
│   ├── archive/
│   │   └── AGENTS.md
│   └── ...
├── .sisyphus/
│   └── plans/
├── .github/workflows/docker-images.yml
├── .github/workflows/cleanup.yml
├── .env.example
├── release.sh
└── start.sh
```

## HIERARCHY
- `backend/AGENTS.md`: backend monorepo directory root for runtime, package, and test boundaries.
- `frontend/AGENTS.md`: frontend monorepo directory root for routes, shared shell, context, and typed browser/backend seams.
- `docs/AGENTS.md`: docs ownership, source-of-truth routing, archive boundaries, and active-plan handoff out of `docs/`.
- `docs/archive/AGENTS.md`: archive handoff, dated historical-note naming, and optional evidence-file boundaries.

## SHARED FACTS
- `start.sh` loads the root `.env` without overwriting already-exported keys, supports `headless` and `full`, and uses backend `18000`, frontend `15173`, and PostgreSQL `15432`.
- `start.sh` adjusts local `CORS_ALLOWED_ORIGINS` and `WEBAUTHN_ORIGIN` for frontend/backend integration, and `start.sh full` also sets `VITE_API_BASE` to the backend URL.
- `.github/workflows/docker-images.yml` uses a standard checkout, builds backend/frontend GHCR images from the monorepo for `linux/arm64`, and still triggers on `v*` tags plus selected PR path changes.
- `release.sh` is the root-owned local monorepo release helper: it keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, runs the backend version-metadata test and frontend build, and commits/tags/pushes one root-repo release.
- The current merged load-balance contract is dual-family: backend and frontend now support `legacy` and `adaptive` strategy types, and backend startup seeds one default strategy of each on the default profile.
- `.github/workflows/cleanup.yml` deletes old workflow runs and untagged container versions only.

## WHERE TO LOOK
- Operator-facing launcher and release summary: `README.md`, `start.sh`, `release.sh`
- Backend/frontend version surfaces: `backend/VERSION`, `backend/pyproject.toml`, `frontend/VERSION`, `frontend/package.json`
- Normative architecture and contract docs: `docs/ARCHITECTURE.md`, `docs/API_SPEC.md`, `docs/DATA_MODEL.md`
- Supporting doc surfaces: `docs/PRD.md`, `docs/REQUESTS_PAGE.md`, `docs/SMOKE_TEST_PLAN.md`, `docs/TEST_CASE_GENERATION_METHODOLOGY.md`, `docs/frontend-chip-design-inventory.md`
- Backend/frontend ownership trees: `backend/AGENTS.md`, `frontend/AGENTS.md`
- Docs provenance, archive naming, and active-plan handoff: `docs/AGENTS.md`, `docs/archive/AGENTS.md`, `.sisyphus/plans/`

## CONVENTIONS
- Keep this file focused on repo-wide facts and cross-directory boundaries.
- Point downward instead of repeating leaf-level implementation detail here.
- Keep launcher documentation aligned with `start.sh`, especially `.env` loading, `headless|full`, PostgreSQL reachability checks, `VITE_API_BASE`, and local CORS/WebAuthn wiring.
- Keep repo-level version docs aligned with the root release helper keeping `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` in sync inside this monorepo.
- Keep `README.md` aligned with the same launcher and version facts; it is the root operator-facing contract.
- Keep active implementation plans out of `docs/`; store working plans under `.sisyphus/plans/`, and use `docs/archive/` only for finished notes or retained evidence worth keeping with the docs set.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not describe `backend/` or `frontend/` as external repos, gitlinks, or separately released submodules. They are root-owned monorepo directories.
- Do not invent CI jobs, extra compose files, unsupported routes, unsupported providers, or extra realtime message types.
- Do not blur selected profile with active runtime profile, or imply that `X-Profile-Id` affects proxy traffic.
- Do not strand upgrade guidance in archive notes or compatibility layers when the live docs can state the target contract directly.
