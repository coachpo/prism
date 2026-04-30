<!-- Generated: 2026-04-30 | branch: main | commit: 083e848 -->
# PRISM REPO KNOWLEDGE BASE

## OVERVIEW
Prism is a self-hosted LLM proxy gateway. This repo is a monorepo, and the root owns the launcher, release and deploy helpers, docs, CI wiring, and the checked-in `backend/` and `frontend/` trees.

## STRUCTURE
```text
prism/
├── README.md
├── VERSION
├── backend/
│   └── ...
├── frontend/
│   ├── AGENTS.md
│   ├── components.json
│   ├── server.mjs
│   └── src/
│       ├── pages/AGENTS.md
│       ├── components/AGENTS.md
│       ├── context/AGENTS.md
│       ├── hooks/AGENTS.md
│       ├── i18n/AGENTS.md
│       └── lib/AGENTS.md
├── docs/
│   ├── AGENTS.md
│   ├── archive/
│   │   └── AGENTS.md
│   └── ...
├── .sisyphus/
│   └── plans/
├── .github/workflows/docker-images.yml
├── .github/workflows/cleanup.yml
├── frontend/.env.example
├── deploy.sh
├── release.sh
└── start.sh
```

## HIERARCHY
- `backend/AGENTS.md`: backend monorepo directory root for runtime and test boundaries.
- `backend/tests/AGENTS.md`: backend contract, integration, runtime, and priority regression boundary.
- `frontend/AGENTS.md`: frontend monorepo directory root for routes, shared shell, context, typed browser/backend seams, and child ownership routers under `src/`.
- `frontend/src/pages/AGENTS.md`: route-domain handoff for mounted page surfaces and page-owned drill-down clusters.
- `frontend/src/pages/dashboard/AGENTS.md`, `frontend/src/pages/model-detail/AGENTS.md`, `frontend/src/pages/request-logs/AGENTS.md`, `frontend/src/pages/settings/AGENTS.md`, and `frontend/src/pages/statistics/AGENTS.md`: dense route-domain leaves and their parent-covered local clusters.
- `frontend/src/pages/endpoints/AGENTS.md`, `frontend/src/pages/loadbalance-strategies/AGENTS.md`, `frontend/src/pages/models/AGENTS.md`, `frontend/src/pages/pricing-templates/AGENTS.md`, and `frontend/src/pages/proxy-api-keys/AGENTS.md`: profile-scoped or global management route leaves.
- `frontend/src/components/AGENTS.md`: shared shell and widget handoff for `layout/app-layout`, loadbalance, statistics, and `ui/`.
- `frontend/src/context/AGENTS.md`: provider-layer handoff for auth, selected-profile management scope, and selected-profile keyed reporting-currency readiness.
- `frontend/src/hooks/AGENTS.md`: shared hook handoff for realtime subscriptions, polling, and timezone formatting.
- `frontend/src/i18n/AGENTS.md`: locale and formatting handoff for catalogs, static labels, and shared Intl helpers.
- `frontend/src/lib/AGENTS.md`: typed backend/browser integration handoff for `api/`, websocket helpers, reference data, and reporting currency.
- `frontend/tests/AGENTS.md`: frontend contract and Playwright test boundary.
- `docs/AGENTS.md`: docs ownership, source-of-truth routing, archive boundaries, and active-plan handoff out of `docs/`.
- `docs/archive/AGENTS.md`: archive boundary for finished notes and retained evidence.

## SHARED FACTS
- `start.sh` reads the root `.env`, supports `headless` and `full`, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, and uses backend `18000`, frontend `15173`, and PostgreSQL `5432`.
- `start.sh` keeps a fixed local launcher contract by using plaintext bootstrap ownership, the local PostgreSQL DSN, and in `full` mode keeping browser traffic same-origin by unsetting `VITE_API_BASE` and starting Vite with `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET=http://localhost:18000`.
- Plaintext bootstrap startup is file-backed. Existing bootstrap files must include `runtime.transport.requestTimeout`, usually `"60s"`; legacy encrypted bootstrap files are rejected instead of migrated in place.
- Mail delivery is bootstrap-managed and disabled by default. Enabled SMTP validates at startup; invalid enabled mail config must fail rather than falling back to no-op delivery.
- Backend database capacity is split into named lanes for runtime execution, telemetry, feedback, management, realtime, cache refresh, and background jobs. Background or management work must not borrow protected proxy capacity.
- `.github/workflows/docker-images.yml` checks out the monorepo, builds backend and frontend GHCR images for `linux/arm64`, runs on path-filtered `main` pushes, path-filtered PRs, `v*` tags, and `workflow_dispatch`, and can build one service or both.
- `release.sh` keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata plus the frontend build, then commits, tags, and pushes one root release.
- `.github/workflows/cleanup.yml` handles cleanup only, retaining three workflow runs and pruning untagged backend/frontend container versions.
- `deploy.sh` is a thin root forwarding helper that SSHes to `capy`, changes into `orange_work/curse`, and delegates to the remote `./deploy.sh`.

## WHERE TO LOOK
- Operator-facing launcher, release, and deploy helpers: `README.md`, `start.sh`, `release.sh`, `deploy.sh`, `frontend/.env.example`
- Backend/frontend version surfaces: `backend/VERSION`, `frontend/VERSION`, `frontend/package.json`
- Frontend toolchain and shadcn registry config: `frontend/package.json`, `frontend/components.json`, `frontend/src/index.css`
- Normative architecture and contract docs: `docs/ARCHITECTURE.md`, `docs/API_SPEC.md`, `docs/DATA_MODEL.md`
- Supporting doc surfaces: `docs/PRD.md`, `docs/REQUESTS_PAGE.md`, `docs/SMOKE_TEST_PLAN.md`, `docs/TEST_CASE_GENERATION_METHODOLOGY.md`, `docs/WORKFLOWS.md`
- Backend/frontend ownership trees: `backend/AGENTS.md`, `backend/tests/AGENTS.md`, `frontend/AGENTS.md`, `frontend/src/pages/AGENTS.md`, `frontend/src/components/AGENTS.md`, `frontend/src/context/AGENTS.md`, `frontend/src/hooks/AGENTS.md`, `frontend/src/i18n/AGENTS.md`, `frontend/src/lib/AGENTS.md`, `frontend/tests/AGENTS.md`
- Docs provenance, archive naming, and active-plan handoff: `docs/AGENTS.md`, `docs/archive/AGENTS.md`, `.sisyphus/plans/`

## COMMANDS
```bash
./start.sh headless
./start.sh full
cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority
cd backend && go build ./cmd/prism-backend
cd frontend && pnpm run build
cd frontend && pnpm run lint
cd frontend && pnpm run test:e2e
```

## CONVENTIONS
- Keep this file focused on repo-wide facts and cross-directory boundaries.
- Point downward instead of repeating leaf-level implementation detail here.
- Keep launcher docs aligned with `start.sh`, especially root `.env` loading, `headless|full`, ports, repo-local `config.json` defaults, same-origin proxying, `PRISM_VITE_PROXY_ENABLED`, `PRISM_VITE_PROXY_TARGET`, and local CORS wiring.
- Keep bootstrap docs aligned with the file-backed v1 contract: `runtime.transport.requestTimeout` required, `runtime.secretEncryptionKey` preserve-only in v1, safe secret responses metadata-only, enabled SMTP fail-fast.
- Keep repo-level version docs aligned with `release.sh` and the four version surfaces it updates.
- Keep `README.md` aligned with the same launcher, release, and deploy facts.
- Keep active implementation plans out of `docs/`; store working plans under `.sisyphus/plans/`, and use `docs/archive/` only for finished notes or retained evidence.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not describe `backend/` or `frontend/` as external repos, gitlinks, or separately released submodules. They are root-owned monorepo directories.
- Do not invent CI jobs, extra compose files, unsupported routes, unsupported providers, or extra realtime message types.
- Do not imply `start.sh full` sets a browser-visible backend base URL; it now keeps browser traffic same-origin through the local Vite proxy.
- Do not blur selected profile with active runtime profile, or imply that `X-Profile-Id` affects proxy traffic.
- Do not treat vendor metadata as runtime compatibility. Runtime compatibility comes from model `api_family`; vendor rows and `icon_key` are presentation metadata.
- Do not put request-path side effects back inline when durable outboxes, after-commit wakeups, and background workers own those flows.
- Do not strand upgrade guidance in archive notes or compatibility layers when the live docs can state the target contract directly.
