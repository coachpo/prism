<!-- Generated: 2026-07-09 | branch: main | commit: 76f4ab62 -->
# PRISM REPO KNOWLEDGE BASE

## OVERVIEW
Prism is a self-hosted LLM proxy gateway. The repo root owns the local launcher, release helper, CI wiring, durable docs, local run artifacts, and the checked-in `backend/` and `frontend/` trees.

## STRUCTURE
```text
prism/
├── README.md, VERSION
├── start.sh, release.sh
├── Dockerfile, docker-compose.yml, docker/
├── backend/       # Go backend, migrations, backend image, Go tests
├── frontend/      # React/Vite dashboard, shadcn config, frontend tests
├── docs/          # Durable reference docs only
├── artifacts/     # Ignored local run evidence and scratch plans
└── .github/workflows/{ci,docker-images,cleanup}.yml
```

## HIERARCHY
- `backend/AGENTS.md`: Go runtime root, Dockerfile, migrations, and regression boundaries.
- `backend/internal/AGENTS.md`: backend source router for platform, domain, gateway, HTTP API, and small compatibility packages.
- `backend/internal/platform/AGENTS.md`: process lifecycle, HTTP assembly, hot bootstrap runtime, DB lanes, scheduler, retention, side effects; `platform/startup/AGENTS.md` owns startup sequencing and seeds.
- `backend/internal/domain/AGENTS.md`: HTTP-neutral audit, loadbalance, model-routing, stats, and terminal-target helpers; `domain/loadbalance/AGENTS.md` owns Ban Policy runtime state, and `domain/stats/AGENTS.md` owns stats read models.
- `backend/internal/gateway/AGENTS.md`: provider-agnostic envelopes, hooks, routing, reservations, adapters, accounting; `gateway/core/AGENTS.md` owns shared gateway contracts, `gateway/provider/AGENTS.md` owns provider-native adapter rules, and `gateway/provider/openai/AGENTS.md` owns OpenAI Chat/Responses translation.
- `backend/internal/httpapi/AGENTS.md`: mounted management, runtime, proxy-key usage, and request-context seams.
- `backend/internal/httpapi/management/AGENTS.md`: `/api/*` management fanout and shared management conventions; leaf docs live below it.
- `backend/internal/httpapi/runtime/AGENTS.md`: operation-registered proxy surfaces.
- `frontend/AGENTS.md`: React/Vite dashboard root, route shell, providers, shadcn config, and frontend tests.
- `frontend/src/AGENTS.md`: frontend source router for app, features, pages, components, context, hooks, i18n, shared, lib, shell, and tests.
- `frontend/src/{app,features,pages,components,context,hooks,i18n,shared,lib}/AGENTS.md`: frontend route, shell, page, provider, hook, locale, API, and shared UI seams; `shared/design-system/AGENTS.md` and `lib/types/AGENTS.md` own high-centrality leaves.
- `backend/tests/AGENTS.md`: Go regression root; `tests/contract/AGENTS.md`, `tests/integration/AGENTS.md`, and `tests/runtime/AGENTS.md` own the large suite boundaries.
- `frontend/tests/AGENTS.md`: Playwright browser flows plus frontend seam/server/lib contract boundaries; `tests/e2e/AGENTS.md` and `tests/lib/AGENTS.md` own the large runner-specific leaves.
- `docs/AGENTS.md`: durable docs ownership and local artifact handoff rules.

## CODE MAP
| Surface | Location | Role |
|---|---|---|
| Launcher | `start.sh` | Seeds/validates local bootstrap config, starts PostgreSQL/backend/Vite proxy. |
| Backend entry | `backend/cmd/prism-backend/main.go` | Loads bootstrap config, telemetry, startup, and production app. |
| Composition | `backend/internal/platform/lifecycle/production.go` | Wires services, pools, workers, shutdown hooks. |
| HTTP mount | `backend/internal/platform/http/server.go` | Mounts `/health`, `/api`, `/v1`, `/v1beta`. |
| Management mount | `backend/internal/platform/http/management_branch.go` | Mounts management services and mutation/runtime-cache middleware. |
| Runtime allowlist | `backend/internal/httpapi/runtime/operations.go` | Exact supported method/path operations and hook collections. |
| Runtime executor | `backend/internal/httpapi/runtime/service.go`, `backend/internal/httpapi/runtime/runtime.go` | Ingress rejection, planning, provider transport, telemetry side effects. |
| Frontend shell | `frontend/src/main.tsx`, `frontend/src/App.tsx` | Browser mount, providers, router, toasts. |
| Routes | `frontend/src/app/router/appRouter.tsx`, `rewriteRoutes.ts` | Route tree, scopes, and search schemas. |
| Frontend API | `frontend/src/lib/api/core.ts` | Typed HTTP client and same-origin request plumbing. |

## SHARED FACTS
- `start.sh` reads the root `.env`, supports `headless` and `full`, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, keeps frontend `5173` and PostgreSQL `15432`, and follows the selected bootstrap file's backend port; fresh seeds default that port to `8000`.
- `start.sh` keeps a local launcher contract by using plaintext bootstrap ownership and the local PostgreSQL DSN, and in `full` mode keeping browser traffic same-origin by unsetting `VITE_API_BASE` and starting Vite with `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at the effective backend port from the selected bootstrap file.
- Local scratch plans and execution artifacts live under ignored `artifacts/`; run evidence uses `artifacts/evidence/`.
- The root `docker-compose.yml` is the default local/self-hosted bundle. It builds the root single-image app, runs PostgreSQL separately, publishes only the public Prism HTTP port, and persists `prism_postgres_data` plus `prism_config` volumes.
- The root `Dockerfile` builds a single app image with the Go backend, backend migrations/version, optional React static assets, Nginx, and `docker/entrypoint.sh`; `BUILD_FRONTEND=false` keeps backend proxy paths and serves a fallback page.
- The root Nginx template proxies `/health`, `/api`, `/v1`, and `/v1beta` to the private backend upstream and serves SPA assets from `/usr/share/nginx/html`.
- The runtime contract is operation-registered. Supported routes are allowlisted in `backend/internal/httpapi/runtime/operations.go`, and unsupported or wrong-method requests reject before provider transport, telemetry, audit, feedback, or durable runtime side effects.
- Runtime request extraction, non-stream parsing, stream terminal classification, and token-count behavior are split across `operation_request_hooks.go`, `operation_response_hooks.go`, and `operation_stream_hooks.go` beside the shared runtime executor.
- `operation_name` is persisted in `request_logs` and `usage_request_events`, and the route matrix plus hook residency are regression-backed in backend runtime tests.
- `usage_request_events.endpoint_label_snapshot` is the retained endpoint label source for usage snapshots, spending, and Top Endpoints while public stats JSON continues to expose `endpoint_label`.
- Request-log browsing supports `client_rule_id` against caller User-Agent Client Rules only, plus `resolved_target_model_id` for final target filtering.
- Model-owned context routing, overflow-promotion authoring, and exact facade routing were hard-deleted. Keep ordinary operation-registered routing, Terminal Targets, OpenAI sibling-operation translation, Ban Policy strategies, and flat final-target observability as the live contract.
- Plaintext bootstrap startup is file-backed. Backend-owned canonical defaults are the source of truth for fresh seeds: `0.0.0.0:8000`, standalone database URL `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, CORS `5173`, PostgreSQL pools and admission derived from CPU count via `unit = clamp(GOMAXPROCS, 8, 16)` (management `unit+1`, execution `unit`, telemetry `unit/2`, feedback/cache/jobs `unit/4`, total = lane sum 27–53, admission m2 `unit` / m3 `unit/2`), transport `100/16/16/300s/90s/0s/10s/1s`, and side-effect timeout `10s`; the root launcher sets `DATABASE_URL` to local PostgreSQL on host port `15432`. Runtime buffering is automatic and internal. Existing valid files are preserved until manual reset by stop, remove or relocate, and restart.
- Mail config fields are parsed for live `config.json` compatibility only. Mail delivery and transport behavior are removed.
- Backend database capacity is split into named lanes for runtime execution, telemetry, feedback, management, cache refresh, and background jobs. Background or management work must not borrow protected proxy capacity.
- Partitioned log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`; runtime writers ensure daily partitions, and the low-priority platform worker maintains a 15-day horizon.
- `backend/Dockerfile` runs the backend as `prism:prism` (`1000:1000`), owns `/app/config`, and defaults the container bootstrap path to `/app/config/config.json`.
- Backend image builds use the repo root as Docker context (`docker build -f backend/Dockerfile .`), so root `.dockerignore` controls CI/backend image contents.
- `.github/workflows/ci.yml` runs backend regression/build, frontend seam/server/build/lint, blocking Go/frontend dependency scanners, and non-blocking local-image Trivy evidence uploads.
- `.github/workflows/docker-images.yml` publishes backend and frontend GHCR images for `linux/arm64` on `v*` tags and `workflow_dispatch` only, requires a green CI conclusion for tag pushes, builds on native arm64 runners without QEMU, moves `latest` only on release tags, and can build one service or both on manual dispatch.
- `release.sh` keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata plus the frontend build, then commits, tags, and pushes one root release.
- `.github/workflows/cleanup.yml` handles cleanup only, pruning untagged backend/frontend container versions.

## WHERE TO LOOK
- Operator-facing launcher, release, and local bundle helpers: `README.md`, `start.sh`, `release.sh`, `docker-compose.yml`, `Dockerfile`, `docker/`, `frontend/.env.example`
- Local scratch plans and retained execution evidence: `artifacts/plans/`, `artifacts/evidence/`
- Backend/frontend version surfaces: `backend/VERSION`, `frontend/VERSION`, `frontend/package.json`
- Backend container contract: `backend/Dockerfile`, `backend/tests/integration/dockerfile_contract_test.go`
- Runtime operation registry, hook residency, rejection semantics, and `operation_name` persistence: `backend/internal/httpapi/runtime/`, `backend/tests/runtime/`, `docs/API_SPEC.md`, `docs/ARCHITECTURE.md`
- Startup bootstrap loading/parsing contract: `backend/internal/platform/config/`
- Partitioned log retention: `backend/internal/platform/logretention/`, `backend/internal/httpapi/runtime/log_partitions.go`, `backend/migrations/000001_initial_schema.sql`
- Runtime proxy planning, telemetry, request-log detail, final-target attribution, and partition ensuring: `backend/internal/httpapi/runtime/`, `backend/tests/runtime/`, `frontend/src/pages/request-logs/`
- Model access-target authoring and removed exact-facade guards: `backend/internal/httpapi/management/models/`, `frontend/src/pages/models/`
- Management settings and retention jobs: `backend/internal/httpapi/management/settings/`, `frontend/src/pages/settings/`, `docs/WORKFLOWS.md`
- Frontend toolchain and shadcn registry config: `frontend/package.json`, `frontend/components.json`, `frontend/src/index.css`, `frontend/src/main.tsx`. The routing-diagram surface (`frontend/src/pages/dashboard/routing-diagram/`) renders as a plain list; it has no graph-rendering dependency.
- Normative architecture and contract docs: `docs/ARCHITECTURE.md`, `docs/API_SPEC.md`, `docs/DATA_MODEL.md`
- Supporting doc surfaces: `docs/PRD.md`, `docs/REQUESTS_PAGE.md`, `docs/WORKFLOWS.md`
- Backend ownership tree: `backend/AGENTS.md`, `backend/internal/AGENTS.md`, `backend/internal/platform/AGENTS.md`, `backend/internal/platform/startup/AGENTS.md`, `backend/internal/domain/AGENTS.md`, `backend/internal/domain/loadbalance/AGENTS.md`, `backend/internal/domain/stats/AGENTS.md`, `backend/internal/gateway/AGENTS.md`, `backend/internal/gateway/core/AGENTS.md`, `backend/internal/gateway/provider/AGENTS.md`, `backend/internal/gateway/provider/openai/AGENTS.md`, `backend/internal/httpapi/AGENTS.md`, `backend/internal/httpapi/management/AGENTS.md`, `backend/internal/httpapi/runtime/AGENTS.md`, `backend/internal/httpapi/management/*/AGENTS.md`, `backend/tests/AGENTS.md`, `backend/tests/{contract,integration,runtime}/AGENTS.md`
- Frontend ownership tree: `frontend/AGENTS.md`, `frontend/src/AGENTS.md`, `frontend/src/app/AGENTS.md`, `frontend/src/features/AGENTS.md`, `frontend/src/pages/AGENTS.md`, `frontend/src/components/AGENTS.md`, `frontend/src/context/AGENTS.md`, `frontend/src/hooks/AGENTS.md`, `frontend/src/i18n/AGENTS.md`, `frontend/src/shared/AGENTS.md`, `frontend/src/shared/design-system/AGENTS.md`, `frontend/src/lib/AGENTS.md`, `frontend/src/lib/types/AGENTS.md`, `frontend/tests/AGENTS.md`, `frontend/tests/e2e/AGENTS.md`, `frontend/tests/lib/AGENTS.md`
- Docs provenance, scratch-plan handoff, and live evidence routing: `docs/AGENTS.md`, `artifacts/plans/`, `artifacts/evidence/`

## COMMANDS
```bash
./start.sh headless
./start.sh full
docker compose up --build
cd backend && go test ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
cd backend && go test ./internal/... ./cmd/...
cd backend && go build ./cmd/prism-backend
cd frontend && pnpm exec vitest run
cd frontend && pnpm run test:lib
cd frontend && pnpm run test:server
cd frontend && pnpm run build
cd frontend && pnpm run lint
cd frontend && pnpm run test:e2e
```

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep this file focused on repo-wide boundaries instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this file focused on repo-wide facts and cross-directory boundaries.
- Point downward instead of repeating leaf-level implementation detail here.
- Keep launcher docs aligned with `start.sh`, especially root `.env` loading, `headless|full`, ports, repo-local `config.json` defaults, same-origin proxying, `PRISM_VITE_PROXY_ENABLED`, `PRISM_VITE_PROXY_TARGET`, and local CORS wiring.
- Keep local/self-hosted deployment docs aligned with the root `docker-compose.yml`, root `Dockerfile`, and `docker/` Nginx/entrypoint contract.
- Keep runtime docs aligned with the explicit operation registry, operation hook collections, rejected-route isolation, and `operation_name` persistence instead of broad `/v1` or `/v1beta` path-family wording.
- Keep bootstrap docs aligned with the file-backed v1 contract: `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout` required, `runtime.secretEncryptionKey` preserve-only in v1, metadata-only secret snapshots, restart-required external edits, and parse-only mail config compatibility.
- Keep repo-level version docs aligned with `release.sh` and the four version surfaces it updates.
- Keep backend container docs aligned with `backend/Dockerfile`, especially non-root `prism:prism` ownership and `/app/config/config.json` defaults.
- Keep partitioned log-retention docs aligned with the four managed tables, runtime partition ensuring, management retention jobs, and the low-priority platform worker.
- Keep `README.md` aligned with the same launcher and release facts.
- Keep active implementation plans and live execution artifacts out of `docs/`; store local scratch plans under `artifacts/plans/` and run evidence under `artifacts/evidence/`.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not describe `backend/` or `frontend/` as external repos, gitlinks, or separately released submodules. They are root-owned monorepo directories.
- Do not invent CI jobs, extra compose files, unsupported routes, or unsupported providers.
- Do not imply `start.sh full` sets a browser-visible backend base URL; it now keeps browser traffic same-origin through the local Vite proxy.
- Do not describe `/v1` or `/v1beta` as broad passthrough runtime prefixes; supported operations are allowlisted in `backend/internal/httpapi/runtime/operations.go`.
- Do not blur Default-profile management reads or writes with active runtime proxy traffic, or imply that `X-Profile-Id` affects proxy traffic.
- Do not put request-path side effects back inline when durable outboxes, after-commit wakeups, and background workers own those flows.
- Do not bypass partitioned log-retention ownership with direct cleanup or partition creation outside `backend/internal/platform/logretention/` and runtime partition ensuring.
- Do not change backend container execution or bootstrap-path ownership contracts without updating Dockerfile contract tests and docs.
- Do not strand upgrade guidance in transient run notes or compatibility layers when the live docs or owning AGENTS tree can state the target contract directly.
