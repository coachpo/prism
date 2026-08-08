# Development Rules

<!-- write-project-docs:development-source-size:start -->
## General Size and Responsibility Rules

Source code size, responsibility boundaries, long-file review, and splitting requirements are governed by [Source Code Size and Responsibility Rules](source-code-size-and-responsibility-rules.md).

This file does not repeat or restate the general thresholds in that dedicated policy.
<!-- write-project-docs:development-source-size:end -->

## Shared Rules

Follow the shared design and implementation principles and the Definition of Done in [CONTRIBUTING.md](../CONTRIBUTING.md). The unified source size and responsibility policy is a separate authoritative document.

## Backend Rules (Go)

- Runtime operations are registry-allowlisted: `backend/internal/httpapi/runtime/operations.go` owns the supported method/path pairs, hook collections, and model-binding rules; unsupported or wrong-method requests reject before provider transport, telemetry, audit, feedback, or durable runtime side effects.
- `api_family` is runtime compatibility truth; catalog metadata does not participate in runtime compatibility.
- Database capacity is split into named lanes (`runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `cache_refresh`, `background_jobs`); background or management work must not borrow protected proxy capacity.
- Request-path side effects stay on durable outboxes, scheduler-owned workers, or after-commit wakeups; do not put provider sends, cache invalidations, or dashboard materialization inline.
- Partitioned log retention is owned by `backend/internal/platform/logretention/` plus runtime partition ensuring; the managed tables are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- Bootstrap config is plaintext and file-backed: steady-state settings prefer the startup JSON over new environment-variable knobs; `PRISM_CONFIG_PATH` and `DATABASE_URL` remain bootstrap-only env exceptions.
- The backend container contract (non-root `prism:prism` UID/GID `1000:1000`, writable `/app/config` ownership, default `/app/config/config.json`) is regression-backed by `backend/tests/integration/dockerfile_contract_test.go`.
- Regression layers live under `backend/tests/`: contract, integration, runtime (operation matrix, rejected routes, hook residency), and priority (DB lanes and admission) suites.

## Frontend Rules (TypeScript/React)

- React 19 + Vite + TypeScript + Tailwind CSS 4 + shadcn/ui + TanStack Router; Node >= 24, pnpm 10.30.1.
- Any UI/UX-facing, visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change defers to `frontend/DESIGN.md`; use `@/shared/design-system` before `@/components/ui`, and keep design-system components free of route state and API calls.
- `src/app/router/appRouter.tsx` and `src/app/router/rewriteRoutes.ts` are the source of truth for mounted routes, search schemas, and route scopes; `src/App.tsx` stays the thin wrapper.
- Keep backend access on the typed `src/lib/api.ts` boundary; management scope stays pinned to Default profile id `1` (no profile-selection UI).
- Keep the single zh-CN locale state, shared formatting, and static non-hook labels in `src/i18n/`.
- Keep shadcn/ui additions aligned with `components.json`; primitives live under `src/components/ui/`.
- Tests: Vitest/lib suites, server seam tests, and Playwright e2e flows (capped near five journey specs).

## Cross-Cutting Rules

- Each fact or policy has exactly one authoritative source; other places link to it instead of restating it.
- For ordinary removal-only validation, prefer manual confirmation over dedicated "proves not" tests; keep absence assertions only when the missing surface is a shipped contract or guardrail.
- Keep active implementation plans and live execution evidence out of `docs/`: use `artifacts/plans/` and `artifacts/evidence/`.
- LLM upstream changes must evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.
- When a change touches the launcher or deployment surface, keep `start.sh`, the root Compose bundle, and the container docs aligned (ports, same-origin proxying, bootstrap defaults).
