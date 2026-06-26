# FRONTEND STATISTICS COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`src/components/statistics/` holds the shared renderers used by the dashboard analytics surface. It currently owns the spending card helper and stays presentation-only.

## WHERE TO LOOK
- `TopSpendingCard.tsx`
- `../../pages/statistics/UsageStatisticsContent.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep these components presentation-first.
- Keep dashboard analytics orchestration and data fetching in the page layer.
- Keep null-vs-zero rendering rules and metric formatting decisions in the page helpers, not in these renderers.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not move snapshot orchestration or request-log drilldown state into these shared renderers.
- Do not duplicate page-local null-vs-zero rendering rules when the dashboard analytics helpers already shape the inputs.
- Do not add page-specific fetches or route state to this shared leaf.
