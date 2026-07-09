# FRONTEND SRC KNOWLEDGE BASE

## OVERVIEW
`frontend/src/` owns the browser application source below the Vite entrypoint: route construction, feature routes, legacy page clusters, shared shell/components, providers, hooks, locale state, typed API, shared helpers, and frontend test seams.

## STRUCTURE
```text
src/
├── app/          # router, providers, forms, route rewrite metadata
├── features/     # active protected route modules
├── pages/        # legacy/oracle page clusters and dense page helpers
├── components/   # shell chrome, shared widgets, generated UI primitives
├── context/      # auth, theme, reporting currency
├── hooks/        # shared React hooks
├── i18n/         # locale provider, formatting, static messages
├── lib/          # typed API, reference data, browser integration
├── shared/       # cross-route design-system, table, forms, API helpers
└── test/         # Vitest/jsdom setup and MSW seams
```

## WHERE TO LOOK
- Browser entry and provider stack: `main.tsx`, `App.tsx`
- Mounted routes, search schemas, protected/public split, and route metadata: `app/AGENTS.md`
- Active protected route modules and feature-local page/data handoffs: `features/AGENTS.md`
- Legacy page clusters, dense request-log/settings/statistics/dashboard surfaces, and page-local child docs: `pages/AGENTS.md`
- Shell chrome, shared widgets, generated primitives, and component-only ownership: `components/AGENTS.md`
- Auth, pinned profile headers, and reporting-currency readiness: `context/AGENTS.md`
- Shared hooks: `hooks/AGENTS.md`
- Typed API, reference data, and request plumbing: `lib/AGENTS.md`
- Shared design-system wrappers, table/form helpers, and cross-route utilities: `shared/AGENTS.md`
- Locale state and non-hook static labels: `i18n/AGENTS.md`
- Vitest/jsdom setup and MSW handlers: `test/`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `../DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Keep route state and data fetching in `app/`, `features/`, `pages/`, or `lib/`; never in generated UI primitives.
- Keep `features/` thin at the route boundary. Reuse `pages/` clusters until a route is fully migrated.
- Keep management scope pinned to Default id=1. `X-Profile-Id` is still sent for profile-scoped management routes but no UI state chooses it.
- Keep reporting-currency cache and readiness in `context/ReportingCurrencyContext.tsx` and `lib/reportingCurrency.ts`.
- Keep live dashboard and analytics refresh in page-owned polling hooks that use the typed REST API boundary.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep frontend env vars to transport/build wiring such as `VITE_API_BASE`, launcher proxy envs, and build metadata.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response display logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not add routes without updating `app/router/rewriteRoutes.ts`, `app/router/appRouter.tsx`, navigation metadata, tests, and the owning docs.
- Do not duplicate typed API, reference-data, reporting-currency, or navigation-config ownership in page code.
- Do not bypass `../DESIGN.md` with page-local color systems, decorative gradients, raw status colors, or ad hoc dark-mode overrides.
- Do not create child AGENTS files for small one-off helpers when an existing parent already owns the convention.
