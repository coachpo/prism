<!-- Generated: 2026-06-16 | branch: main | commit: 8ae630e -->
# PRISM REPO KNOWLEDGE BASE

## OVERVIEW
Prism is a self-hosted LLM proxy gateway. The repo root owns the local launcher, release and deploy helpers, CI wiring, durable docs, the `.omo/` planning workspace, and the checked-in `backend/` and `frontend/` trees.

## STRUCTURE
```text
prism/
├── README.md
├── VERSION
├── Dockerfile                 # Single-image backend+frontend+Nginx build
├── docker-compose.yml         # Default local/self-hosted bundle
├── docker/                    # Single-image Nginx template and launcher entrypoint
├── backend/
│   └── ...
├── frontend/
│   ├── AGENTS.md
│   ├── components.json
│   ├── server.mjs
│   └── src/
│       ├── app/AGENTS.md
│       ├── features/AGENTS.md
│       ├── pages/AGENTS.md
│       ├── shared/AGENTS.md
│       ├── components/AGENTS.md
│       ├── context/AGENTS.md
│       ├── hooks/AGENTS.md
│       ├── i18n/AGENTS.md
│       └── lib/AGENTS.md
├── docs/
│   ├── AGENTS.md
│   └── ...
├── .omo/
│   ├── plans/
│   ├── evidence/
│   └── ...
├── .github/workflows/ci.yml
├── .github/workflows/docker-images.yml
├── .github/workflows/cleanup.yml
├── frontend/.env.example
├── deploy.sh
├── release.sh
└── start.sh
```

## HIERARCHY
- `backend/AGENTS.md`: backend root for Go runtime, HTTP API, platform, gateway, migrations, Docker image, and tests.
- `backend/internal/platform/AGENTS.md`: process infrastructure, lifecycle assembly, hot bootstrap runtime, DB lanes, scheduler, migrations, log retention, and side effects.
- `backend/internal/domain/AGENTS.md`: audit, loadbalance runtime state, model routing, stats snapshots, and terminal-target domain helpers shared by management/runtime surfaces.
- `backend/internal/gateway/AGENTS.md`: preserved gateway contracts, hook phases, operation records, provider adapters, route planning, reservations, and accounting.
- `backend/internal/httpapi/AGENTS.md`: mounted management, runtime, realtime, proxy-key usage, retention-job, and request-context HTTP seams.
- `backend/internal/httpapi/runtime/AGENTS.md`: operation registry, request planning, hook collections, telemetry outbox, feedback pipeline, partition ensuring, facades, and context overflow promotion.
- `backend/internal/httpapi/realtime/AGENTS.md`: `/api/realtime/ws`, connection manager, auth gate, dashboard publisher, and analytics publisher.
- `backend/internal/httpapi/management/*/AGENTS.md`: management leaves for auth, bootstrap config, config bundle, config rules, connections, endpoints, loadbalance, models, profiles, settings, stats, and audit.
- `frontend/AGENTS.md`: frontend root for React/Vite routes, shell, providers, typed API/browser seams, shadcn config, and tests.
- `frontend/src/app/AGENTS.md`: TanStack router construction, auth/public gates, rewrite route metadata, legacy redirects, route suspense, and QueryClient defaults.
- `frontend/src/features/AGENTS.md`: active protected route modules under `src/app/router`, including selected-profile, global control, mixed settings, and observe surfaces.
- `frontend/src/pages/AGENTS.md`: oracle-compatible auth and legacy route-domain clusters still reused by feature routes and tests; page leaves live below it.
- `frontend/src/components/AGENTS.md`, `frontend/src/context/AGENTS.md`, `frontend/src/hooks/AGENTS.md`, `frontend/src/i18n/AGENTS.md`, `frontend/src/shared/AGENTS.md`, and `frontend/src/lib/AGENTS.md`: shared frontend shell, provider, hook, locale, rewrite-helper, API, websocket, and browser integration ownership.
- `frontend/tests/AGENTS.md`: Playwright e2e plus frontend seam/server/lib contract boundaries.
- `docs/AGENTS.md`: durable docs ownership, source-of-truth routing, and active-plan/evidence handoff out of `docs/`.

## SHARED FACTS
- `start.sh` reads the root `.env`, supports `headless` and `full`, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, keeps frontend `5173` and PostgreSQL `15432`, and follows the selected bootstrap file's backend port; fresh seeds default that port to `8000`.
- `start.sh` keeps a local launcher contract by using plaintext bootstrap ownership and the local PostgreSQL DSN, and in `full` mode keeping browser traffic same-origin by unsetting `VITE_API_BASE` and starting Vite with `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at the effective backend port from the selected bootstrap file.
- Active working plans and execution artifacts live under `.omo/`; current repo planning uses `.omo/plans/` plus `.omo/evidence/`.
- The root `docker-compose.yml` is the default local/self-hosted bundle. It builds the root single-image app, runs PostgreSQL separately, publishes only the public Prism HTTP port, and persists `prism_postgres_data` plus `prism_config` volumes.
- The root `Dockerfile` builds a single app image with the Go backend, backend migrations/version, optional React static assets, Nginx, and `docker/entrypoint.sh`; `BUILD_FRONTEND=false` keeps backend proxy paths and serves a fallback page.
- The root Nginx template proxies `/health`, `/api`, `/api/realtime/ws`, `/v1`, and `/v1beta` to the private backend upstream and serves SPA assets from `/usr/share/nginx/html`.
- The runtime contract is operation-registered. Supported routes are allowlisted in `backend/internal/httpapi/runtime/operations.go`, and unsupported or wrong-method requests reject before provider transport, telemetry, audit, feedback, or durable runtime side effects.
- Runtime request extraction, non-stream parsing, stream terminal classification, media multipart handling, and token-count behavior are split across `operation_request_hooks.go`, `operation_response_hooks.go`, `operation_stream_hooks.go`, and `operation_media_hooks.go` beside the shared runtime executor.
- `operation_name` is persisted in `request_logs` and `usage_request_events`, and the route matrix plus hook residency are regression-backed in backend runtime tests.
- `usage_request_events.endpoint_label_snapshot` is the retained endpoint label source for usage snapshots, spending, and Top Endpoints while public stats JSON continues to expose `endpoint_label`.
- Request-log browsing supports `client_rule_id` against caller User-Agent Client Rules only, plus `resolved_target_model_id` for final target filtering.
- CLIProxyAPI context overflow promotion is an explicit model-scoped replay path: model authoring and config bundles carry `context_overflow_promotion_target_id`, runtime promotion decisions stay additive in request-log context routing and trace metadata, and tests cover backend validation plus frontend authoring/import seams.
- Plaintext bootstrap startup is file-backed. Backend-owned canonical defaults are the source of truth for fresh seeds: `0.0.0.0:8000`, standalone database URL `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, CORS `5173`, pool total `24`, split `4/8/4/2/2/2/2`, transport `100/16/16/300s/90s/0s/10s/1s`, side-effect timeout `10s`, and admission `3/2`; the root launcher sets `DATABASE_URL` to local PostgreSQL on host port `15432`. Runtime buffering is automatic and internal. Existing valid files are preserved until manual reset by stop, remove or relocate, and restart.
- Mail delivery is bootstrap-managed and disabled by default. Enabled SMTP validates at startup; invalid enabled mail config must fail rather than falling back to no-op delivery.
- Backend database capacity is split into named lanes for runtime execution, telemetry, feedback, management, realtime, cache refresh, and background jobs. Background or management work must not borrow protected proxy capacity.
- Partitioned log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`; runtime writers ensure daily partitions, and the low-priority platform worker maintains a 15-day horizon.
- `backend/Dockerfile` runs the backend as `prism:prism` (`1000:1000`), owns `/app/config`, and defaults the container bootstrap path to `/app/config/config.json`.
- `.github/workflows/ci.yml` runs backend regression/build, frontend seam/server/build/lint, focused config E2E, blocking Go/frontend dependency scanners, and non-blocking local-image Trivy evidence uploads.
- `.github/workflows/docker-images.yml` checks out the monorepo, builds backend and frontend GHCR images for `linux/arm64`, runs on path-filtered `main` pushes, path-filtered PRs, `v*` tags, and `workflow_dispatch`, and can build one service or both.
- `release.sh` keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata plus the frontend build, then commits, tags, and pushes one root release.
- `.github/workflows/cleanup.yml` handles cleanup only, retaining three workflow runs and pruning untagged backend/frontend container versions.
- `deploy.sh` is a thin root forwarding helper that SSHes to `capy`, changes into `orange_work/curse`, and delegates to the remote `./deploy.sh`.

## WHERE TO LOOK
- Operator-facing launcher, release, deploy, and local bundle helpers: `README.md`, `start.sh`, `release.sh`, `deploy.sh`, `docker-compose.yml`, `Dockerfile`, `docker/`, `frontend/.env.example`
- Active plans and retained execution evidence: `.omo/plans/`, `.omo/evidence/`
- Backend/frontend version surfaces: `backend/VERSION`, `frontend/VERSION`, `frontend/package.json`
- Backend container contract: `backend/Dockerfile`, `backend/tests/integration/dockerfile_contract_test.go`
- Runtime operation registry, hook residency, rejection semantics, and `operation_name` persistence: `backend/internal/httpapi/runtime/`, `backend/tests/runtime/`, `docs/API_SPEC.md`, `docs/ARCHITECTURE.md`
- Startup bootstrap contract and startup tab ownership: `backend/internal/httpapi/management/bootstrapconfig/`, `backend/internal/platform/config/`, `frontend/src/features/settings/startup/`
- Config-bundle export/import and preview-token flow: `backend/internal/httpapi/management/configbundle/`, `frontend/src/pages/settings/`, `frontend/src/pages/settings/useConfigBackupData.ts`
- Partitioned log retention: `backend/internal/platform/logretention/`, `backend/internal/httpapi/runtime/log_partitions.go`, `backend/migrations/000001_initial_schema.sql`
- Runtime proxy planning, telemetry, request-log detail, context overflow promotion decisions, and partition ensuring: `backend/internal/httpapi/runtime/`, `backend/tests/runtime/`, `frontend/src/pages/request-logs/`
- Context overflow promotion target authoring and import/export validation: `backend/internal/httpapi/management/models/`, `backend/internal/httpapi/management/configbundle/`, `frontend/src/pages/models/`, `frontend/src/lib/configImportValidation.ts`
- Management settings and retention jobs: `backend/internal/httpapi/management/settings/`, `frontend/src/pages/settings/`, `docs/WORKFLOWS.md`
- Frontend toolchain, shadcn registry config, and React Flow routing-diagram dependency: `frontend/package.json`, `frontend/components.json`, `frontend/src/index.css`, `frontend/src/main.tsx`, `frontend/src/pages/dashboard/routing-diagram/`
- Normative architecture and contract docs: `docs/ARCHITECTURE.md`, `docs/API_SPEC.md`, `docs/DATA_MODEL.md`
- Supporting doc surfaces: `docs/PRD.md`, `docs/REQUESTS_PAGE.md`, `docs/SMOKE_TEST_PLAN.md`, `docs/TEST_CASE_GENERATION_METHODOLOGY.md`, `docs/WORKFLOWS.md`
- Backend ownership tree: `backend/AGENTS.md`, `backend/internal/platform/AGENTS.md`, `backend/internal/domain/AGENTS.md`, `backend/internal/gateway/AGENTS.md`, `backend/internal/httpapi/AGENTS.md`, `backend/internal/httpapi/runtime/AGENTS.md`, `backend/internal/httpapi/realtime/AGENTS.md`, `backend/internal/httpapi/management/*/AGENTS.md`, `backend/tests/AGENTS.md`
- Frontend ownership tree: `frontend/AGENTS.md`, `frontend/src/app/AGENTS.md`, `frontend/src/features/AGENTS.md`, `frontend/src/pages/AGENTS.md`, `frontend/src/components/AGENTS.md`, `frontend/src/context/AGENTS.md`, `frontend/src/hooks/AGENTS.md`, `frontend/src/i18n/AGENTS.md`, `frontend/src/shared/AGENTS.md`, `frontend/src/lib/AGENTS.md`, `frontend/tests/AGENTS.md`
- Docs provenance, active-plan handoff, and live evidence routing: `docs/AGENTS.md`, `.omo/plans/`, `.omo/evidence/`

## COMMANDS
```bash
./start.sh headless
./start.sh full
docker compose up --build
cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
cd backend && go build ./cmd/prism-backend
cd frontend && pnpm run test && pnpm run test:lib && pnpm run test:server
cd frontend && pnpm run test:config
cd frontend && pnpm run build
cd frontend && pnpm run lint
cd frontend && pnpm run test:e2e
```

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this file focused on repo-wide facts and cross-directory boundaries.
- Point downward instead of repeating leaf-level implementation detail here.
- Keep launcher docs aligned with `start.sh`, especially root `.env` loading, `headless|full`, ports, repo-local `config.json` defaults, same-origin proxying, `PRISM_VITE_PROXY_ENABLED`, `PRISM_VITE_PROXY_TARGET`, and local CORS wiring.
- Keep local/self-hosted deployment docs aligned with the root `docker-compose.yml`, root `Dockerfile`, and `docker/` Nginx/entrypoint contract.
- Keep runtime docs aligned with the explicit operation registry, operation hook collections, rejected-route isolation, and `operation_name` persistence instead of broad `/v1` or `/v1beta` path-family wording.
- Keep bootstrap docs aligned with the file-backed v1 contract: `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout` required, `runtime.secretEncryptionKey` preserve-only in v1, safe secret responses metadata-only, apply-capability reporting, and enabled SMTP fail-fast.
- Keep repo-level version docs aligned with `release.sh` and the four version surfaces it updates.
- Keep backend container docs aligned with `backend/Dockerfile`, especially non-root `prism:prism` ownership and `/app/config/config.json` defaults.
- Keep partitioned log-retention docs aligned with the four managed tables, runtime partition ensuring, management retention jobs, and the low-priority platform worker.
- Keep `README.md` aligned with the same launcher, release, and deploy facts.
- Keep active implementation plans and live execution artifacts out of `docs/`; store working plans under `.omo/plans/` and run evidence under `.omo/evidence/`.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not describe `backend/` or `frontend/` as external repos, gitlinks, or separately released submodules. They are root-owned monorepo directories.
- Do not invent CI jobs, extra compose files, unsupported routes, unsupported providers, or extra realtime message types.
- Do not imply `start.sh full` sets a browser-visible backend base URL; it now keeps browser traffic same-origin through the local Vite proxy.
- Do not describe `/v1` or `/v1beta` as broad passthrough runtime prefixes; supported operations are allowlisted in `backend/internal/httpapi/runtime/operations.go`.
- Do not blur selected profile with active runtime profile, or imply that `X-Profile-Id` affects proxy traffic.
- Do not put request-path side effects back inline when durable outboxes, after-commit wakeups, and background workers own those flows.
- Do not bypass partitioned log-retention ownership with direct cleanup or partition creation outside `backend/internal/platform/logretention/` and runtime partition ensuring.
- Do not change backend container execution or bootstrap-path ownership contracts without updating Dockerfile contract tests and docs.
- Do not strand upgrade guidance in transient run notes or compatibility layers when the live docs or owning AGENTS tree can state the target contract directly.
