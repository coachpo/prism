<!-- Generated: 2026-08-08 | branch: docs/write-agents | commit: a4708acb -->
# PRISM REPO KNOWLEDGE BASE

## OVERVIEW

Prism is a self-hosted LLM proxy gateway. The repo root owns the local launcher, release helper, CI wiring, durable docs, local run artifacts, and the checked-in `backend/` and `frontend/` trees.

## STRUCTURE

```text
prism/
├── README.md, VERSION, CONTRIBUTING.md
├── STATUS.md      # Live project status; the data-migration authority named in CONVENTIONS below
├── start.sh, release.sh
├── Dockerfile, docker-compose.yml, docker/
├── backend/       # Go backend, migrations, Go tests
├── frontend/      # React/Vite dashboard, shadcn config, frontend tests
├── docs/          # Durable reference docs only
├── artifacts/     # Normally ignored evidence/scratch; tracked direct-entry runbook and SQL
└── .github/workflows/{ci,docker-images,cleanup}.yml
```

## HIERARCHY

- `backend/AGENTS.md`: Go runtime root, migrations, and regression boundaries.
- `backend/internal/AGENTS.md`: backend source router for platform, domain, gateway, HTTP API, and small compatibility packages.
- `backend/internal/platform/AGENTS.md`: process lifecycle, HTTP assembly, startup config runtime, DB lanes, scheduler, retention, side effects; `platform/startup/AGENTS.md` owns startup sequencing and seeds, `platform/config/AGENTS.md` the bootstrap contract, `platform/http/AGENTS.md` route mounting and admission, and `platform/managementjobs/AGENTS.md` the retention v2 job state.
- `backend/internal/domain/AGENTS.md`: HTTP-neutral audit, loadbalance, model-routing, stats, and terminal-target helpers; `domain/loadbalance/AGENTS.md` owns Ban Policy runtime state, `domain/stats/AGENTS.md` owns stats read models, and `domain/safediag/AGENTS.md` owns the safe-diagnostic bottom line.
- `backend/internal/gateway/AGENTS.md`: provider-agnostic envelopes, hooks, routing, reservations, adapters, accounting; `gateway/core/AGENTS.md` owns shared gateway contracts, `gateway/provider/AGENTS.md` owns provider-native adapter rules, and `gateway/provider/openai/AGENTS.md` owns native OpenAI Chat/Responses handling.
- `backend/internal/httpapi/AGENTS.md`: mounted management, runtime, proxy-key usage, and request-context seams.
- `backend/internal/httpapi/management/AGENTS.md`: `/api/*` management fanout and shared management conventions; leaf docs live below it.
- `backend/internal/httpapi/runtime/AGENTS.md`: operation-registered proxy surfaces.
- `frontend/AGENTS.md`: React/Vite dashboard root, route shell, providers, shadcn config, and frontend tests.
- `frontend/src/AGENTS.md`: frontend source router for app, features, pages, components, context, hooks, i18n, shared, lib, and test.
- `frontend/src/{app,features,pages,components,context,hooks,i18n,shared,lib}/AGENTS.md`: frontend route, shell, page, provider, hook, locale, API, and shared UI seams. Each of those routers names its own nested leaves; `shared/design-system/AGENTS.md` and `lib/types/AGENTS.md` are the highest-centrality ones.
- `backend/tests/AGENTS.md`: Go regression root; `tests/contract/AGENTS.md`, `tests/integration/AGENTS.md`, and `tests/runtime/AGENTS.md` own the large suite boundaries.
- `frontend/tests/AGENTS.md`: Playwright browser flows plus frontend seam/lib contract boundaries; `tests/e2e/AGENTS.md` and `tests/lib/AGENTS.md` own the large runner-specific leaves.
- `docs/AGENTS.md`: durable docs ownership and local artifact handoff rules.

## CODE MAP

| Surface | Location | Role |
| --- | --- | --- |
| Launcher | `start.sh` | Seeds/validates local bootstrap config, starts PostgreSQL/backend/Vite proxy. |
| Backend entry | `backend/cmd/prism-backend/main.go` | Loads bootstrap config, telemetry, startup, and production app. |
| Composition | `backend/internal/platform/lifecycle/production.go` | Wires services, pools, workers, shutdown hooks. |
| HTTP mount | `backend/internal/platform/http/server.go` | Mounts `/health`, `/api`, `/v1`, `/v1beta`. |
| Management mount | `backend/internal/platform/http/management_branch.go` | Mounts management services and mutation/runtime-cache middleware. |
| Runtime allowlist | `backend/internal/httpapi/runtime/operations.go` | Exact supported method/path operations and hook collections. |
| Runtime executor | `backend/internal/httpapi/runtime/service.go`, `backend/internal/httpapi/runtime/ingress.go`, `backend/internal/httpapi/runtime/request_plan.go`, `backend/internal/httpapi/runtime/request_execution.go`, `backend/internal/httpapi/runtime/request_execution_loop.go`, `backend/internal/httpapi/runtime/upstream_attempt.go` | Ingress rejection, planning, provider transport, telemetry side effects. |
| Frontend shell | `frontend/src/main.tsx`, `frontend/src/App.tsx` | Browser mount, providers, router, toasts. |
| Routes | `frontend/src/app/router/appRouter.tsx`, `rewriteRoutes.ts` | Route tree, scopes, and search schemas. |
| Frontend API | `frontend/src/lib/api.ts`, `frontend/src/lib/api/request.ts`, `frontend/src/lib/api/*.ts` | Public typed facade, shared request/auth plumbing, and resource clients. |

## SHARED FACTS

- `start.sh` reads the root `.env`, supports `headless` and `full`, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, keeps frontend `5173` and PostgreSQL `15432`, and follows the selected bootstrap file's backend port; fresh seeds default that port to `8000`.
- `start.sh` keeps a local launcher contract by using plaintext bootstrap ownership and the local PostgreSQL DSN, and in `full` mode keeping browser traffic same-origin by unsetting `VITE_API_BASE` and starting Vite with `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at the effective backend port from the selected bootstrap file.
- Local scratch plans and execution artifacts normally live under ignored `artifacts/`; run evidence uses `artifacts/evidence/`. The shipped operator exceptions are `artifacts/plans/direct-request-entry-reclassification.md` and `artifacts/plans/direct-request-entry-reclassification.sql`; their tracked acceptance test is `backend/tests/integration/direct_request_entry_reclassification_plan_test.go`.
- Model detail is the federated management entry for independent models.dev metadata/pricing provenance and pi.dev export-template bindings. The UI shares candidate paging/evidence interaction only; the two backend clients, binding tables, revisions/CAS, migrations, and downstream pricing/render consumers remain separate. `/route/pricing` and `/route/models/export` retain their scenario-specific shortcuts.
- The root `docker-compose.yml` is the only local/self-hosted bundle. It builds the single-image app, runs PostgreSQL as a separate service, publishes the public Prism HTTP port plus the launcher database port, and persists `prism_postgres_data` plus `prism_config` volumes.
- The root `Dockerfile` always builds the single app image with the Go backend, backend migrations/version, React static assets, Nginx, and `docker/entrypoint.sh`.
- The root Nginx template proxies `/health`, `/api`, `/v1`, and `/v1beta` to the private backend upstream and serves SPA assets from `/usr/share/nginx/html`.
- The runtime contract is operation-registered. Supported routes are allowlisted in `backend/internal/httpapi/runtime/operations.go`, and unsupported or wrong-method requests reject before provider transport, telemetry, audit, feedback, or durable runtime side effects.
- Runtime request extraction, non-stream parsing, stream terminal classification, and token-count behavior are split across `operation_request_hooks.go`, `operation_response_hooks.go`, and `operation_stream_hooks.go` beside the shared runtime executor.
- `operation_name` is persisted in `request_logs` and `usage_request_events`, and the route matrix plus hook residency are regression-backed in backend runtime tests.
- `usage_request_events.endpoint_label_snapshot` is the retained endpoint label source for usage snapshots, spending, and Top Endpoints while public stats JSON continues to expose `endpoint_label`.
- Request-log browsing supports `client_rule_id` against caller User-Agent Client Rules only, plus `resolved_target_model_id` for final target filtering.
- Owner-backed Terminal Targets persist a non-empty `upstream_model_id` independently from the entry and resolved logical model identities; orphan compatibility connections may remain NULL and never enter runtime. Runtime snapshots freeze the value per attempt; request logs expose `upstream_model_id`, finalized usage/chain summaries expose `final_upstream_model_id`, and missing historical evidence remains NULL rather than being inferred.
- `model_configs.direct_request_enabled` is the sole persisted client-entry qualification (000032, `BOOLEAN NOT NULL DEFAULT TRUE`): generic upgrade keeps existing rows true, updates preserve on omission, and no `entry_only`/`outbound_only`/`both` enum is stored. Runtime keeps all enabled nodes for recursive Model Target routing but direct operation lookup, `/v1/models`, route-witness/setup roots and representatives, Pi export, and proxy-key self-tests use only direct entries. Non-entry nodes remain configurable Model Targets and surface incoming-reference counts plus an explanatory warning when unreferenced; the tracked one-off 12-entry/four-mapping runbook and SQL are operator-invoked only, never startup work, and the integration suite owns their disposable acceptance contract.
- Model-owned context routing, overflow-promotion authoring, exact facade routing, and OpenAI sibling-operation translation were hard-deleted. OpenAI text attempts are native-only and use operation-set coverage: the model accepted format and Terminal Target capability may be FULL, PARTIAL, or NONE, with valid differences surfaced as structured warnings and no translation. Invalid enum/nullability remains a management 422; runtime planning uses the stable native-operation errors and ordinary dynamic 503 family. Keep ordinary operation-registered routing, the single mixed peer sequence of Model Target and Terminal Target rows (see `docs/architecture.md`), Ban Policy strategies, per-Terminal-Target routing schedules (recurring weekly windows evaluated per request after Ban filtering; no schedule means no restriction), flat final-target observability, and historical `operation_translation_mode` reads as the live contract.
- Plaintext bootstrap startup is file-backed. Backend-owned canonical defaults are the source of truth for fresh seeds: `0.0.0.0:8000`, standalone database URL `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, CORS `5173`, PostgreSQL pools and admission derived from CPU count via `unit = clamp(GOMAXPROCS, 8, 16)` (management `unit+1`, execution `unit`, telemetry `unit/2`, feedback/cache/jobs `unit/4`, total = lane sum 27–53, admission m2 `unit` / m3 `unit/2`), and side-effect timeout `10s`; the root launcher sets `DATABASE_URL` to local PostgreSQL on host port `15432`. The `runtime.transport` section was removed outright: outbound provider requests carry no connection or timeout limits (the upstream transport sets only `DisableCompression: true` and explicit unlimited `MaxIdleConnsPerHost`), and a leftover `runtime.transport` block fails startup with a readable migration error. Runtime buffering is automatic and internal. Existing valid files are preserved until manual reset by stop, remove or relocate, and restart.
- Mail config fields are parsed for live `config.json` compatibility only. Mail delivery and transport behavior are removed.
- Backend database capacity is split into named lanes for runtime execution, telemetry, feedback, management, cache refresh, and background jobs. Background or management work must not borrow protected proxy capacity.
- Partitioned log retention covers `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`; runtime writers ensure daily partitions, and the low-priority platform worker maintains a 15-day horizon.
- The root single app image runs as `prism:prism` (`1000:1000`), owns `/app/config`, and defaults the container bootstrap path to `/app/config/config.json`; the root `.dockerignore` controls its build context.
- `.github/workflows/ci.yml` runs backend regression/build, frontend seam/build/lint, blocking Go/frontend dependency scanners, and non-blocking single-image Trivy evidence uploads.
- `.github/workflows/docker-images.yml` publishes only `ghcr.io/<owner>/<repo>` for `linux/arm64` on `v*` tags and `workflow_dispatch`, requires a green CI conclusion for tag pushes, builds on native arm64 runners without QEMU, and moves `latest` only on release tags.
- `release.sh` keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata plus the frontend build, then commits, tags, and pushes one root release.
- `.github/workflows/cleanup.yml` handles cleanup only, pruning untagged single-image container versions.

## WHERE TO LOOK

- Operator-facing launcher, release, and local bundle helpers: `README.md`, `start.sh`, `release.sh`, `docker-compose.yml`, `Dockerfile`, `docker/`, `frontend/.env.example`
- Local scratch plans, retained execution evidence, the tracked direct-entry reclassification runbook/SQL, and its acceptance test: `artifacts/plans/`, `artifacts/evidence/`, `backend/tests/integration/direct_request_entry_reclassification_plan_test.go`
- Backend/frontend version surfaces: `backend/VERSION`, `frontend/VERSION`, `frontend/package.json`
- Container contract: `Dockerfile`, `backend/tests/integration/dockerfile_contract_test.go`
- Runtime operation registry, hook residency, rejection semantics, and `operation_name` persistence: `backend/internal/httpapi/runtime/`, `backend/tests/runtime/`, `docs/architecture.md` (§14 API Reference, §15 Data Model Reference)
- Startup bootstrap loading/parsing contract: `backend/internal/platform/config/`
- Partitioned log retention: `backend/internal/platform/logretention/`, `backend/internal/httpapi/runtime/log_partitions.go`, `backend/migrations/000001_initial_schema.sql`
- Pricing/observability v2 migrations 000008–000011: additive pricing-trust fields (000008) and requests/audit v2 columns plus outbox-v2 artifacts (000010), each closed by a fail-closed finalize guard (000009; 000011 requires drained v1 outbox + ready backfill domains); the startup owner (`backend/internal/platform/startup/observability_upgrade.go`) runs v1 drain and three-domain backfill
- Requests/Audit v2 read surfaces: scoped statuses (`upstream/gateway/legacy_status_code`), unified failure projection, retained ingress-chain view, server-side CSV export, and cost segments — all in `backend/internal/domain/stats/`, with the `safediag` safe-diagnostic bottom line in `backend/internal/domain/safediag/`
- Runtime proxy planning, telemetry, request-log detail, final-target attribution, and partition ensuring: `backend/internal/httpapi/runtime/`, `backend/tests/runtime/`, `frontend/src/pages/request-logs/`
- Model access-target authoring and removed exact-facade guards: `backend/internal/httpapi/management/models/`, `frontend/src/pages/models/`
- Management settings and retention jobs: `backend/internal/httpapi/management/settings/`, `frontend/src/pages/settings/`, `docs/product.md` (§9 Workflows Reference)
- Frontend toolchain and shadcn registry config: `frontend/package.json`, `frontend/components.json`, `frontend/src/index.css`, `frontend/src/main.tsx`.
- Normative architecture and contract docs: `docs/architecture.md` (§14 API Reference, §15 Data Model Reference)
- Product, requests-page, and workflow surfaces: `docs/product.md` (§8 Requests Page Specification, §9 Workflows Reference)
- Backend ownership tree: start at `backend/AGENTS.md` and `backend/internal/AGENTS.md`, then follow the nested `AGENTS.md` files under `backend/internal/{platform,domain,gateway,httpapi}/` and `backend/tests/`. Each router names its own leaves, so enumerate the current set with `find backend -name AGENTS.md` rather than trusting a list here.
- Frontend ownership tree: start at `frontend/AGENTS.md` and `frontend/src/AGENTS.md`, then follow the nested `AGENTS.md` files under `frontend/src/` and `frontend/tests/`. Enumerate with `find frontend -name AGENTS.md -not -path '*/node_modules/*'`.
- Docs provenance, scratch-plan handoff, live evidence routing, and the tracked direct-entry operator bundle exception: `docs/AGENTS.md`, `artifacts/plans/`, `artifacts/evidence/`

## COMMANDS

```bash
./start.sh headless
./start.sh full
docker compose up --build
cd backend && go test -timeout 30m ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
cd backend && go test ./internal/... ./cmd/...
cd backend && go build ./cmd/prism-backend
cd frontend && pnpm exec vitest run
cd frontend && pnpm run test:lib
cd frontend && pnpm run build
cd frontend && pnpm run lint
cd frontend && pnpm run test:e2e
```

## CONVENTIONS

- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep this file focused on repo-wide boundaries instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project has no external users and iterates continuously on the operator's own home-LAN instance, so preserve legacy shapes only when explicitly requested.
- That convention governs code, API, and schema shape, not data. The running instance holds retained PostgreSQL history and a plaintext `config.json`, so a change that cannot carry existing data forward needs explicit authorization and a verified backup first; see `STATUS.md`.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this file focused on repo-wide facts and cross-directory boundaries.
- Point downward instead of repeating leaf-level implementation detail here.
- Keep launcher docs aligned with `start.sh`, especially root `.env` loading, `headless|full`, ports, repo-local `config.json` defaults, same-origin proxying, `PRISM_VITE_PROXY_ENABLED`, `PRISM_VITE_PROXY_TARGET`, and local CORS wiring.
- Keep local/self-hosted deployment docs aligned with the root `docker-compose.yml`, root `Dockerfile`, and `docker/` Nginx/entrypoint contract.
- Keep runtime docs aligned with the explicit operation registry, operation hook collections, rejected-route isolation, and `operation_name` persistence instead of broad `/v1` or `/v1beta` path-family wording.
- Keep bootstrap docs aligned with the file-backed v1 contract: `runtime.sideEffects.attemptTimeout` required, `runtime.secretEncryptionKey` preserve-only in v1, metadata-only secret snapshots, restart-required external edits, and parse-only mail config compatibility. The `runtime.transport` section no longer exists and is rejected with a readable migration error.
- Keep repo-level version docs aligned with `release.sh` and the four version surfaces it updates.
- Keep container docs aligned with the root `Dockerfile`, especially non-root `prism:prism` ownership and `/app/config/config.json` defaults.
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
- Do not use v1/v2 generation markers in hand-written source-file names or Go/TypeScript symbols as a shortcut for splitting implementations; follow the whole-set refactor rule in `docs/development-rules.md`.

<!-- write-project-docs:document-navigation:start -->
## Project Documentation Navigation

Before starting related work, read the authoritative documents that cover the scope of the task:

- [Project Status](STATUS.md)
- [Documentation Index](docs/README.md)
- [Product Overview](docs/product.md)
- [Architecture Overview](docs/architecture.md)
- [Development Rules](docs/development-rules.md)
- [Source Code Size and Responsibility Rules](docs/source-code-size-and-responsibility-rules.md)
- [Contributing Guide](CONTRIBUTING.md)

When implementing, reviewing, or verifying an engineering change, use `STATUS.md` and the product overview for current facts and delivery intent, then read the [Current Development Strategy](CONTRIBUTING.md#current-development-strategy). Consume only the relevant "Must Complete at This Tier," "Not Pursued by Default," "Non-negotiable Boundaries," and "Tier Transition Conditions." New user requirements, an accepted seven-line GOAL, hard project rules or invariants, real or non-discardable data, existing users, and compatibility commitments take precedence over tier defaults. The `YOLO_LOCAL`, `EXPERIMENT`, and `MVP` tiers permanently forgo active investment in security, privacy, data, credential and key management, compatibility, audit/monitoring/SLO, and regulatory compliance requirements; the exemption does not override those precedence sources and does not change a tier's applicability, transition conditions, or existing prohibitions. A tier does not expand user authorization, create exclusion proof, or allow checks required by affected paths or core acceptance to be skipped. `YOLO_LOCAL` applies only to a user-declared disposable local workspace with no real data, production credentials, external users or traffic, or external side effects; change tiers before proceeding when any condition fails.

## Project Documentation Content Boundaries

This project does not add process or administrative management for the sake of documentation completeness.

- Unless the user explicitly asks and provides verifiable evidence, do not add approvals, reporting, meetings, scheduling, personnel governance, release governance, commit management, business KPIs/SLOs, or similar content.
- Do not create documents, sections, placeholders, or "to be confirmed" items for those topics.
- Existing and verified development, test, build, and deployment commands remain recorded in their own authoritative documents; this block does not change product, architecture, or engineering facts.
<!-- write-project-docs:document-navigation:end -->

<!-- write-agent-guides:engineering-router:start -->
## Engineering Router

Use the narrowest matching row. Read the linked authority before changing code and run only validation already authorized by the task.

| Trigger | Authority | Invariant IDs | Validation entry |
| --- | --- | --- | --- |
| `backend`: Changing Go entrypoints, configuration, toolchain, dependencies, generated inputs, tests, or release artifacts | [docs/architecture.md](docs/architecture.md#inv-go-229d2b017a9b) | [INV-GO-229D2B017A9B](docs/architecture.md#inv-go-229d2b017a9b) | cd backend && go build ./cmd/prism-backend && go test ./internal/... ./cmd/... |
| `backend`: Changing Context chains, goroutines, connections, commands, servers, workers, admission, or shutdown | [docs/architecture.md](docs/architecture.md#inv-go-2d7d34ee6112) | [INV-GO-2D7D34EE6112](docs/architecture.md#inv-go-2d7d34ee6112) | cd backend && go test -timeout 30m ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/... |
| `backend`: Changing transactions, migrations, messages, caches, remote calls, retries, or cross-resource writes | [docs/architecture.md](docs/architecture.md#inv-go-2e9f9d48ab43) | [INV-GO-2E9F9D48AB43](docs/architecture.md#inv-go-2e9f9d48ab43) | cd backend && go test -timeout 30m ./tests/contract ./tests/integration |
| `backend`: Changing package dependencies, synchronous calls, authoritative facts, shared state, or goroutine ownership | [docs/architecture.md](docs/architecture.md#inv-go-a392eb849715) | [INV-GO-A392EB849715](docs/architecture.md#inv-go-a392eb849715) | cd backend && go vet ./internal/... && go test ./internal/... |
| `backend`: Changing HTTP, CLI, file, identity, secret, diagnostic, limit, or public-error boundaries | [docs/architecture.md](docs/architecture.md#inv-go-fd5b268343ae) | [INV-GO-FD5B268343AE](docs/architecture.md#inv-go-fd5b268343ae) | cd backend && go test -timeout 30m ./tests/runtime |
<!-- write-agent-guides:engineering-router:end -->
