# FRONTEND CONTEXT KNOWLEDGE BASE

## OVERVIEW
`src/context/` owns Prism's auth bootstrap, theme/reporting providers, and reporting-currency state. Multi-profile UI state is absent; management requests are pinned to profile id `1` by `../lib/api/core.ts`.

## STRUCTURE
```
context/
├── AuthContext.tsx              # Provider wiring over auth bootstrap, mutations, and refresh helpers
├── auth-context.ts              # Shared context type and createContext() export
├── useAuth.ts                   # Guard hook for auth consumers
├── auth/                        # Bootstrap, mutation, and passive/proactive refresh helpers
├── auth/AGENTS.md               # Helper-layer auth bootstrap, mutation, and refresh ownership
└── ReportingCurrencyContext.tsx # Provider over the pinned reporting-currency cache
```

## WHERE TO LOOK

- Auth bootstrap mode selection, in-flight reuse, proactive refresh timer, and visibility-triggered refresh: `AuthContext.tsx`
- Auth bootstrap, mutation, and refresh helpers: `auth/AGENTS.md`
- Auth context type/export split and guarded hook: `auth-context.ts`, `useAuth.ts`
- Reporting-currency readiness, pinned profile id `1` cache handoff, `prime()` and `refresh()` behavior, default fallback currency, and `useReportingCurrencyContext()`: `ReportingCurrencyContext.tsx`, `../lib/reportingCurrency.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Use `useAuth()` and `useReportingCurrencyContext()` instead of consuming context objects directly.
- Keep auth bootstrap async and reuse in-flight work instead of duplicating fetches.
- Keep reporting-currency readiness keyed to pinned profile id `1`, with provider state and fallback in `ReportingCurrencyContext.tsx` and cache, `prime()` or `refresh()` behavior, and normalization in `../lib/reportingCurrency.ts`.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not invent local profile state in pages.
- Do not inject `X-Profile-Id` from pages or hooks.
- Do not reintroduce profile-selection UI or a profile provider.
- Do not fetch, normalize, or cache reporting currency directly in page hooks when `ReportingCurrencyContext.tsx` and `../lib/reportingCurrency.ts` already own that seam.
