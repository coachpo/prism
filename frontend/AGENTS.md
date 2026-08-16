# FRONTEND KNOWLEDGE BASE

## OVERVIEW
`frontend/` is Prism's monorepo-owned React 19/Vite management dashboard. It owns the browser-side management contract, mounted route surface, protected-shell provider handoff, checked-in shadcn/ui registry config, and the overview/analytics dashboard. The root single-image Dockerfile packages the built assets with the Go backend and Nginx.

## STRUCTURE
```text
frontend/
├── DESIGN.md            # Binding operator design contract; every UI/UX change defers to it
├── src/                 # Dashboard source; `src/AGENTS.md` routes to every owning cluster
├── tests/               # Playwright journeys and Node seam contracts; `tests/AGENTS.md` owns the split
├── scripts/             # Standalone evidence-capture and debug scripts, not part of the build
├── public/, index.html, README.md
├── output/playwright/   # Checked-in route screenshots; the only tracked output directory
├── .env.example         # Transport-only env wiring (`VITE_API_BASE`, launcher proxy)
├── pnpm-workspace.yaml, pnpm-lock.yaml
├── components.json      # shadcn registry config
├── package.json         # Scripts and pnpm/Node pins
├── vite.config.ts, vitest.config.ts, playwright.config.ts, eslint.config.js
├── tsconfig.json        # Solution-style (`files: []` + references); the real checks are in `tsconfig.app.json` and `tsconfig.node.json`
└── VERSION              # One of the four version surfaces `../release.sh` keeps aligned
```

## ROUTE MAP
- Public auth route: `/auth/login`.
- `/` redirects to `/observe`.
- Removed auth compatibility paths are unsupported: `/login`, `/forgot-password`, `/reset-password`.

## HIERARCHY
- `src/App.tsx` is the thin browser wrapper over the rewrite router, query client, auth provider, and TanStack `RouterProvider`.
- `src/AGENTS.md`: source tree router for route shell, features, page clusters, shared UI, contexts, hooks, i18n, API, and tests.
- `src/app/AGENTS.md`: router construction, auth/public gates, rewrite metadata, suspense, and QueryClient defaults.
- `src/features/AGENTS.md`: active protected route modules, pinned management features, global control pages, mixed settings, and observe surfaces.
- `src/pages/AGENTS.md`: auth pages and oracle-compatible route clusters still reused by feature routes and tests.
- `src/components/AGENTS.md`, `src/context/AGENTS.md`, `src/hooks/AGENTS.md`, `src/i18n/AGENTS.md`, `src/shared/AGENTS.md`, and `src/lib/AGENTS.md`: shared shell, providers, hooks, locale, rewrite helpers, API, and browser integration.
- `tests/AGENTS.md`: Playwright browser flows plus frontend seam-contract suites.

## WHERE TO LOOK
- Mounted routes, auth/public split, protected shell mounts, and rewrite route metadata: `src/AGENTS.md`, `src/app/AGENTS.md`, `src/app/router/appRouter.tsx`, `src/app/router/rewriteRoutes.ts`, `src/App.tsx`
- Active protected route modules and feature-local page/data handoffs: `src/features/AGENTS.md`
- Shell chrome, sidebar entries, breadcrumbs, and version label: `src/components/AGENTS.md`, `src/components/layout/app-layout/AGENTS.md`
- Shared widgets, shell-safe controls, and design-system wrappers: `src/components/AGENTS.md`, `src/components/ui/AGENTS.md`
- shadcn registry config and Tailwind entrypoint: `components.json`, `package.json`, `src/index.css`, `src/main.tsx`
- Provider stack and browser mount (`LocaleProvider` -> `ThemeProvider` -> `TooltipProvider` -> `App` + `Toaster`): `src/main.tsx`
- Auth bootstrap, pinned `X-Profile-Id: 1` scoping, and reporting-currency readiness: `src/context/AGENTS.md`, `src/context/auth/AGENTS.md`, `src/context/ReportingCurrencyContext.tsx`
- Typed API boundary, shared request plumbing, and reporting-currency cache and normalization: `src/lib/AGENTS.md`, `src/lib/api/AGENTS.md`, `src/lib/reportingCurrency.ts`, `src/lib/api.ts`
- Shared vendor cache and profile-revision keyed reference-data invalidation: `src/lib/referenceData.ts`
- Frontend zh-CN locale state, shared formatting, and static non-hook labels: `src/i18n/LocaleProvider.tsx`, `src/i18n/format.ts`, `src/i18n/staticMessages.ts`
- Vite version injection, optional same-origin proxying for `/api`, `/health`, `/v1`, and `/v1beta`, dev or preview `/health`, launcher proxy env path, launcher port `5173` to the selected bootstrap file's backend port, and build metadata: `vite.config.ts`, `package.json`
- Production `dist/` packaging, SPA fallback, and `/health` proxying: `../Dockerfile`, `../docker/nginx.conf.template`, `../docker/entrypoint.sh`
- Test split and browser config: `tests/AGENTS.md`, `tests/e2e/`, `tests/lib/`, `playwright.config.ts`
- Cross-route rewrite helpers for query keys, invalidation, server validation, table rows, and design-system barrels: `src/shared/AGENTS.md`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Node is `>=24`, package management is `pnpm@10.30.1`, and frontend scripts are `dev`, `build`, `lint`, `preview`, `test`, `test:lib`, and `test:e2e`.
- Type-check with `pnpm run build` (`tsc -b && vite build`) or `pnpm exec tsc -b`. The root `tsconfig.json` is solution-style with `files: []`, so `tsc --noEmit -p tsconfig.json` checks nothing and exits `0` on a broken tree.
- Treat `src/app/router/appRouter.tsx` and `src/app/router/rewriteRoutes.ts` as the source of truth for mounted routes, search schemas, and route scopes; `src/App.tsx` stays the thin wrapper.
- Keep `src/components/` focused on shared shell chrome, shared widgets, and design-system wrappers, and keep the leaf ownership documented below it.
- Keep model CRUD, access-target authoring, accepted-format controls, and typed/import validation in their owning leaves without reintroducing deleted model-owned context routing fields.
- Keep backend access on the typed `src/lib/api.ts` boundary and the modules it re-exports.
- Keep backend startup configuration out of the dashboard after R2. Operators edit `config.json` and restart; `VITE_API_BASE` plus launcher proxy envs are transport wiring only.
- Keep reporting-currency provider state in `src/context/ReportingCurrencyContext.tsx` and shared cache, `prime()` or `refresh()` behavior, and normalization in `src/lib/reportingCurrency.ts` instead of duplicating settings-side currency bootstrap in pages.
- Keep live dashboard and analytics refresh in page-owned polling hooks that use the typed REST API boundary.
- Keep the single zh-CN locale state, shared formatting, and non-hook static label lookups in `src/i18n/`, not in shell or page code.
- Keep shadcn/ui additions aligned with `components.json`: `style` `new-york`, Tailwind CSS in `src/index.css`, `lucide` icons, aliases rooted at `@/`, and generated primitives under `src/components/ui/`.
- Use existing `ui/` primitives and local wrappers before adding one-off markup in pages or shared widgets.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not add generic React, Vite, or test-runner boilerplate here.
- Do not invent routes, shell entries, or page hierarchies beyond `src/App.tsx` and `src/pages/AGENTS.md`.
- Do not reintroduce profile-selection UI; management scope is pinned to Default id=1.
- Do not duplicate reporting-currency cache, normalization, or readiness logic outside `src/context/ReportingCurrencyContext.tsx` and `src/lib/reportingCurrency.ts`.
- Do not duplicate reference-data or navigation-config ownership in page docs.
- Do not put route state, data fetching, or shell navigation into `src/components/ui/`; it is a design-system leaf.
