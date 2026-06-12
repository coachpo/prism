# DOCS REFERENCE MAP

## OVERVIEW
`docs/` holds Prism's normative architecture, API, and data-model docs plus supporting references. Active working plans live outside `docs/` under `../.omo/plans/`; live execution artifacts belong in `../.omo/evidence/`; transient run notes never outrank live docs or owning code AGENTS files.

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
└── TEST_CASE_GENERATION_METHODOLOGY.md
```

## OWNERSHIP
- `ARCHITECTURE.md`, `API_SPEC.md`, and `DATA_MODEL.md` are the source-of-truth trio.
- `PRD.md`, `REQUESTS_PAGE.md`, `SMOKE_TEST_PLAN.md`, `WORKFLOWS.md`, and `TEST_CASE_GENERATION_METHODOLOGY.md` are supporting references that defer to the normative trio and owning backend/frontend AGENTS files.
- Active working plans belong in `../.omo/plans/`, not under `docs/`.
- Live execution evidence and LLM test-run records belong in `../.omo/evidence/`, not under `docs/`.

## WHERE TO LOOK
- Launcher, release, and deploy facts: `../README.md`, `../start.sh`, `../release.sh`, `../deploy.sh`, `../frontend/.env.example`
- Backend/frontend version surfaces: `../VERSION`, `../backend/VERSION`, `../frontend/VERSION`, `../frontend/package.json`
- Backend container contract: `../backend/Dockerfile`, `../backend/tests/integration/dockerfile_contract_test.go`
- Runtime operation contract, hook residency, rejected-route isolation, and `operation_name` persistence: `API_SPEC.md`, `ARCHITECTURE.md`, `DATA_MODEL.md`, `../backend/internal/httpapi/runtime/AGENTS.md`, `../backend/internal/httpapi/runtime/operations.go`
- Startup bootstrap contract, hot-apply effect reporting, and startup-tab ownership: `../backend/internal/httpapi/management/bootstrapconfig/AGENTS.md`, `../backend/internal/platform/config/`, `../frontend/src/features/settings/startup/`
- Config bundle and vendor catalog export/import ownership: `../backend/internal/httpapi/management/configbundle/AGENTS.md`, `../frontend/src/pages/settings/`, `../frontend/src/pages/settings/useConfigBackupData.ts`
- Partitioned log retention contract: `../backend/internal/platform/logretention/`, `../backend/internal/httpapi/runtime/log_partitions.go`, `../backend/migrations/000001_initial_schema.sql`
- Sidecars control-plane contract: `../backend/internal/httpapi/management/sidecars/AGENTS.md`, `../backend/internal/httpapi/management/sidecars/`, `../backend/migrations/000001_initial_schema.sql`, `../frontend/src/features/sidecars/`
- Backend and frontend ownership boundaries inside the monorepo: `../backend/AGENTS.md`, `../backend/internal/httpapi/AGENTS.md`, `../backend/internal/httpapi/management/`, `../frontend/AGENTS.md`
- Backend management child ownership: `../backend/internal/httpapi/management/audit/AGENTS.md`, `../backend/internal/httpapi/management/connections/AGENTS.md`, `../backend/internal/httpapi/management/configrules/AGENTS.md`, `../backend/internal/httpapi/management/endpoints/AGENTS.md`, `../backend/internal/httpapi/management/loadbalance/AGENTS.md`, `../backend/internal/httpapi/management/models/AGENTS.md`, `../backend/internal/httpapi/management/profiles/AGENTS.md`, `../backend/internal/httpapi/management/stats/AGENTS.md`, `../backend/internal/httpapi/management/vendors/AGENTS.md`
- Product and request-log context: `PRD.md`, `REQUESTS_PAGE.md`
- Operator workflow map grounded in the mounted route and API surface: `WORKFLOWS.md`
- Test-generation workflow: `TEST_CASE_GENERATION_METHODOLOGY.md`
- Active working plans and live execution evidence: `../.omo/plans/`, `../.omo/evidence/`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep docs Prism-specific.
- Point to child AGENTS files instead of repeating leaf detail.
- Prefer documenting steady-state configuration through the file-backed startup JSON and its owning AGENTS/code paths; mention env vars only when they are bootstrap-critical exceptions such as `PRISM_CONFIG_PATH` or `DATABASE_URL`.
- Keep launcher facts aligned with `../start.sh`, especially root `.env` loading, `headless|full`, ports, repo-local `config.json` defaults, the canonical PostgreSQL host port `15432`, same-origin full-mode proxying via `PRISM_VITE_PROXY_ENABLED` and `PRISM_VITE_PROXY_TARGET`, the checked-in `config.json` backend port `8000`, and the bootstrap-only startup contract.
- Keep runtime contract docs aligned with the explicit operation allowlist, operation hook collections, rejected-route isolation, and `operation_name` persistence instead of broad vendor path-family wording.
- Keep CLIProxyAPI context overflow promotion docs aligned with the model-scoped `context_overflow_promotion_target_id`, additive request-log/trace metadata, config-bundle import validation, and frontend authoring/import seams.
- Keep bootstrap docs aligned with backend ownership: plaintext file-backed v1, required `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout`, metadata-only safe secrets, `runtime.secretEncryptionKey` preserve-only, apply-capability reporting, unsupported encrypted legacy files, and enabled SMTP fail-fast.
- Keep release facts aligned with `../release.sh` and the version surfaces it updates.
- Keep backend container docs aligned with non-root `../backend/Dockerfile` execution, `/app/config` ownership, and `../backend/tests/integration/dockerfile_contract_test.go`.
- Keep log-retention docs aligned with the four managed partitioned tables, management settings/job endpoints, runtime partition ensuring, and platform maintenance worker.
- Keep sidecar docs aligned with `/sidecars`, `/api/sidecars/*`, the baseline sidecar schema, the low-priority sidecar sync worker, and the rule that CLIProxyAPI owns live auth/provider state.
- Keep live sidecar implementation contracts, including the strict `/auth-files` top-level `files` envelope rule, in `../backend/internal/httpapi/management/sidecars/AGENTS.md`; run notes are evidence only.
- State CI facts accurately: `.github/workflows/docker-images.yml` builds monorepo images for `linux/arm64` on path-filtered `main` pushes, path-filtered PRs, `v*` tags, and `workflow_dispatch`, and `.github/workflows/cleanup.yml` handles cleanup only.
- Keep active plans and execution evidence out of `docs/`. Use `../.omo/plans/` plus `../.omo/evidence/` while work is in flight.
- Keep `REQUESTS_PAGE.md` subordinate to the live request-log route and tests. When request-log audit, clipboard, proxy-key usage, or reporting-currency behavior changes, refresh the page AGENTS and backend runtime tests before supporting prose.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not add generic framework or tool explainers.
- Do not invent CI jobs, unsupported routes, unsupported providers, or extra compose files.
- Do not reintroduce any live-plan sink under `docs/`.
- Do not treat transient run notes as the source of truth when a live doc or child AGENTS file already owns the topic.
- Do not leave active implementation details stranded only in `docs/` when the owning backend or frontend AGENTS tree should carry the implementation map.
- Do not leave CLIProxyAPI envelope rules, route contracts, or other live sidecar details canonical only in `.omo/evidence/`.
- Do not describe bootstrap config as DB-backed, encrypted, hot-reloaded, or merged with PostgreSQL profile/vendor bundle import.
- Do not document log retention as a generic cleanup query; it is partitioned-log ownership across runtime writers, management jobs, and platform maintenance.

