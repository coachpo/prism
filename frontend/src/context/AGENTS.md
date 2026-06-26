# FRONTEND CONTEXT KNOWLEDGE BASE

## OVERVIEW
`src/context/` owns Prism's auth bootstrap, management-profile scoping, and reporting-currency provider state. Keep the selected-profile versus active-runtime split explicit here, because this layer feeds `X-Profile-Id` for management calls and gates selected-profile keyed reporting-currency state without touching proxy routing. `ReportingCurrencyContext.tsx` plus `../lib/reportingCurrency.ts` are the shared reporting-currency cache, prime, and refresh seam.

## STRUCTURE
```
context/
├── AuthContext.tsx              # Provider wiring over auth bootstrap, mutations, and refresh helpers
├── auth-context.ts              # Shared context type and createContext() export
├── useAuth.ts                   # Guard hook for auth consumers
├── auth/                        # Bootstrap, mutation, and passive/proactive refresh helpers
├── auth/AGENTS.md               # Helper-layer auth bootstrap, mutation, and refresh ownership
├── ProfileContext.tsx           # Provider wiring over profile bootstrap, actions, persistence, and selection
├── profile/                     # Bootstrap, actions, persistence, and selection helpers
├── profile/AGENTS.md            # Helper-layer profile bootstrap, persistence, selection, and CRUD ownership
└── ReportingCurrencyContext.tsx # Provider over selected-profile keyed reporting-currency cache, readiness, and refresh
```

## WHERE TO LOOK

- Auth bootstrap mode selection, in-flight reuse, proactive refresh timer, and visibility-triggered refresh: `AuthContext.tsx`
- Auth bootstrap, mutation, and refresh helpers: `auth/AGENTS.md`
- Auth context type/export split and guarded hook: `auth-context.ts`, `useAuth.ts`
- Selected-profile persistence, active-profile sync, `setApiProfileId()` updates, and revision triggers: `ProfileContext.tsx`
- Profile bootstrap, CRUD actions, local-storage persistence, and selected-profile resolution: `profile/AGENTS.md`
- Reporting-currency readiness, selected-profile keyed cache handoff, `prime()` and `refresh()` behavior, default fallback currency, and `useReportingCurrencyContext()`: `ReportingCurrencyContext.tsx`, `../lib/reportingCurrency.ts`

## CHILD DOCS

- `auth/AGENTS.md`: helper-layer auth bootstrap flow, login/logout mutation helpers, and passive/proactive refresh rules.
- `profile/AGENTS.md`: helper-layer profile bootstrap, local-storage persistence, selection fallback, and CRUD action orchestration.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Use `useAuth()`, `useProfileContext()`, and `useReportingCurrencyContext()` instead of consuming context objects directly.
- Keep auth bootstrap async and reuse in-flight work instead of duplicating fetches.
- Keep `selectedProfile` and `activeProfile` distinct in UI and docs. `selectedProfile` scopes management APIs; it does not switch proxy traffic.
- Treat `ProfileContext.revision` as the shared invalidation signal when selected scope changes.
- Keep reporting-currency readiness keyed to `selectedProfileId`, with provider state and fallback in `ReportingCurrencyContext.tsx` and cache, `prime()` or `refresh()` behavior, and normalization in `../lib/reportingCurrency.ts`.
- Keep bootstrap and helper logic in `auth/` and `profile/`, with the provider files focused on composition and exposed state.
- Let the child AGENTS files own helper-layer detail so this parent stays provider-focused.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not invent local profile state in pages.
- Do not inject `X-Profile-Id` from pages or hooks.
- Do not assume `selectedProfile` affects proxy traffic.
- Do not fetch, normalize, or cache reporting currency directly in page hooks when `ReportingCurrencyContext.tsx` and `../lib/reportingCurrency.ts` already own that seam.
- Do not bypass the `auth/` or `profile/` helper modules with duplicate bootstrap, persistence, or selection logic.
