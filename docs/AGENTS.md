# DOCS REFERENCE MAP

## OVERVIEW
`docs/` holds the canonical documentation set (index, product, architecture, development rules, and size policy) plus this instruction file. Active working plans live outside `docs/` under `../artifacts/plans/`; live execution artifacts belong in `../artifacts/evidence/`; transient run notes never outrank live docs or owning code AGENTS files.

## STRUCTURE
```text
docs/
├── AGENTS.md
├── README.md                        # Documentation index and authority map
├── product.md                       # Product spec; merged requests-page spec and workflows reference
├── architecture.md                  # Architecture; merged API reference (§14) and data model reference (§15)
├── development-rules.md             # Project-specific implementation rules
└── source-code-size-and-responsibility-rules.md  # Unified size and responsibility policy
```

## OWNERSHIP
- `README.md` is the documentation index and authority map for the set.
- `product.md` owns product problems, target users, goals, scope, flows, requirements, and acceptance facts, including the merged requests-page specification (section 8) and workflows reference (section 9).
- `architecture.md` owns the current architecture, components and responsibilities, dependency direction, interfaces, data model, security boundaries, and local run model, including the merged API reference (section 14) and data model reference (section 15).
- `development-rules.md` owns project- and technology-specific implementation rules; its size-rules block is managed by the `write-project-docs` tooling and must not be hand-edited.
- `source-code-size-and-responsibility-rules.md` is the standalone unified size policy rendered from the shared asset; keep it byte-identical to the asset rendering.
- Active working plans belong in `../artifacts/plans/`, not under `docs/`.
- Live execution evidence and LLM test-run records belong in `../artifacts/evidence/`, not under `docs/`.

## WHERE TO LOOK
- Project status and policy: `../STATUS.md`
- Launcher and release facts: `../README.md`, `../start.sh`, `../release.sh`, `../frontend/.env.example`
- Backend/frontend version surfaces: `../VERSION`, `../backend/VERSION`, `../frontend/VERSION`, `../frontend/package.json`
- Backend container contract: `../backend/Dockerfile`, `../backend/tests/integration/dockerfile_contract_test.go`
- Runtime operation contract, hook residency, rejected-route isolation, and `operation_name` persistence: `architecture.md` (§14 API Reference, §15 Data Model Reference), `../backend/internal/httpapi/runtime/AGENTS.md`, `../backend/internal/httpapi/runtime/operations.go`
- Codex client catalog implementation, refresh worker, embedded fallback, and regression coverage: `../backend/internal/httpapi/runtime/AGENTS.md`, `../backend/internal/httpapi/runtime/codex_models.go`, `../backend/internal/httpapi/runtime/codex_models_updater.go`, `../backend/internal/httpapi/runtime/codex_client_models.json`, `../backend/internal/httpapi/runtime/codex_models_test.go`
- OpenAI strict text mode equality, runtime rejection contract, preflight, and historical translation-mode reads: `../backend/internal/httpapi/runtime/operation_translation.go`, `../backend/internal/providerauth/providerauth.go`, `../backend/internal/openaimodecheck/`, `../backend/internal/domain/stats/request_logs.go`
- Startup bootstrap loading/parsing contract: `../backend/internal/platform/config/`
- Partitioned log retention contract: `../backend/internal/platform/logretention/`, `../backend/internal/httpapi/runtime/log_partitions.go`, `../backend/migrations/000001_initial_schema.sql`
- Backend and frontend ownership boundaries inside the monorepo: `../backend/AGENTS.md`, `../backend/internal/httpapi/AGENTS.md`, `../backend/internal/httpapi/management/`, `../frontend/AGENTS.md`
- Backend management child ownership: `../backend/internal/httpapi/management/AGENTS.md`, `../backend/internal/httpapi/management/auth/AGENTS.md`, `../backend/internal/httpapi/management/audit/AGENTS.md`, `../backend/internal/httpapi/management/connections/AGENTS.md`, `../backend/internal/httpapi/management/configrules/AGENTS.md`, `../backend/internal/httpapi/management/endpoints/AGENTS.md`, `../backend/internal/httpapi/management/loadbalance/AGENTS.md`, `../backend/internal/httpapi/management/models/AGENTS.md`, `../backend/internal/httpapi/management/settings/AGENTS.md`, `../backend/internal/httpapi/management/stats/AGENTS.md`
- Product and request-log context: `product.md` (§8 Requests Page Specification)
- Endpoint label snapshots and request-log filter semantics: `architecture.md` (§14 API Reference, §15 Data Model Reference), `product.md` (§8 Requests Page Specification)
- Operator workflow map grounded in the mounted route and API surface: `product.md` (§9 Workflows Reference)
- Active working plans and live execution evidence: `../artifacts/plans/`, `../artifacts/evidence/`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `../frontend/DESIGN.md`; keep docs focused on durable reference ownership instead of repeating design-system rules.
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep docs Prism-specific.
- Point to child AGENTS files instead of repeating leaf detail.
- Prefer documenting steady-state configuration through the file-backed startup JSON and its owning AGENTS/code paths; mention env vars only when they are bootstrap-critical exceptions such as `PRISM_CONFIG_PATH` or `DATABASE_URL`.
- Keep launcher facts aligned with `../start.sh`, especially root `.env` loading, `headless|full`, ports, repo-local `config.json` defaults, the canonical PostgreSQL host port `15432`, same-origin full-mode proxying via `PRISM_VITE_PROXY_ENABLED` and `PRISM_VITE_PROXY_TARGET`, fresh bootstrap backend port `8000`, and the bootstrap-only startup contract.
- Keep runtime contract docs aligned with the explicit operation allowlist, operation hook collections, rejected-route isolation, and `operation_name` persistence instead of broad vendor path-family wording.
- Keep hard-delete docs aligned: model-owned context routing, overflow-promotion authoring, exact facade routing, context-window preflight filtering, and OpenAI sibling-operation translation are retired, while native Terminal Target capability checks, Ban Policy strategies, flat final-target request-log fields, and historical translation-mode reads remain live.
- Keep bootstrap docs aligned with backend ownership: plaintext file-backed v1, required `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout`, restart-required external edits after R2, unsupported encrypted legacy files, and parse-only legacy mail fields.
- Keep release facts aligned with `../release.sh` and the version surfaces it updates.
- Keep backend container docs aligned with non-root `../backend/Dockerfile` execution, `/app/config` ownership, and `../backend/tests/integration/dockerfile_contract_test.go`.
- Keep log-retention docs aligned with the four managed partitioned tables, management settings/job endpoints, runtime partition ensuring, and platform maintenance worker.
- State CI facts accurately: `../.github/workflows/docker-images.yml` publishes monorepo images for `linux/arm64` on `v*` tags and `workflow_dispatch` only, gated on green CI for the tagged commit, and `../.github/workflows/cleanup.yml` handles cleanup only.
- Keep active plans and execution evidence out of `docs/`. Use `../artifacts/plans/` plus `../artifacts/evidence/` while work is in flight.
- Keep the requests-page spec (`product.md` §8) subordinate to the live request-log route and tests. When request-log audit, clipboard, proxy-key usage, or reporting-currency behavior changes, refresh the page AGENTS and backend runtime tests before supporting prose.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not add generic framework or tool explainers.
- Do not invent CI jobs, unsupported routes, unsupported providers, or extra compose files.
- Do not reintroduce any live-plan sink under `docs/`.
- Do not treat transient run notes as the source of truth when a live doc or child AGENTS file already owns the topic.
- Do not leave active implementation details stranded only in `docs/` when the owning backend or frontend AGENTS tree should carry the implementation map.
- Do not describe bootstrap config as DB-backed, encrypted, or hot-reloaded.
- Do not document log retention as a generic cleanup query; it is partitioned-log ownership across runtime writers, management jobs, and platform maintenance.
- Do not re-create standalone ARCHITECTURE/API_SPEC/DATA_MODEL/PRD-style docs; `architecture.md` (§14, §15) and `product.md` (§8, §9) are the merged authorities.
