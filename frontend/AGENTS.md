# FRONTEND KNOWLEDGE BASE

## OVERVIEW
`frontend/` is Prism's monorepo-owned React 19/Vite management dashboard. It owns the browser-side management contract, mounted route surface, protected-shell provider handoff, checked-in shadcn/ui registry config, and the production `dist/` server while keeping this doc as the router for the frontend directory.

## STRUCTURE
```text
frontend/
├── src/
│   ├── App.tsx
│   ├── main.tsx
│   ├── components/AGENTS.md
│   ├── components/layout/app-layout/AGENTS.md
│   ├── components/loadbalance/AGENTS.md
│   ├── components/statistics/AGENTS.md
│   ├── components/ui/AGENTS.md
│   ├── context/AGENTS.md
│   ├── context/auth/AGENTS.md
│   ├── context/profile/AGENTS.md
│   ├── hooks/AGENTS.md
│   ├── i18n/AGENTS.md
│   ├── lib/AGENTS.md
│   ├── lib/api/AGENTS.md
│   ├── lib/websocket/AGENTS.md
│   └── pages/AGENTS.md
├── tests/AGENTS.md
├── components.json
├── package.json
├── server.mjs
└── vite.config.ts
```

## ROUTE MAP
- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- Protected shell routes: `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/settings`, `/proxy-api-keys`, `/sidecars`, `/pricing-templates`, `/request-logs`
- `/` redirects to `/dashboard`

## HIERARCHY
- `src/App.tsx` owns the mounted route surface and stays the source of truth for route mounting and shell boundaries.
- `src/pages/AGENTS.md` owns route-domain handoff for the mounted page surface under `src/pages/`, including the global sidecars route leaf.
- `src/components/AGENTS.md` owns shared shell and widget work, then points down to the layout shell cluster, feature renderers, and `ui/` primitives.
- `src/components/{layout/app-layout,loadbalance,statistics,ui}/AGENTS.md` own shell, feature-renderer, and primitive leaves.
- `src/components/ui/AGENTS.md` owns the shadcn/ui primitives and local wrappers checked into `src/components/ui/`.
- `src/context/AGENTS.md` owns auth bootstrap, selected-profile management scope, and reporting-currency provider state.
- `src/hooks/AGENTS.md` owns the shared realtime, polling, and timezone-formatting hooks.
- `src/i18n/AGENTS.md` owns locale catalogs, static label helpers, and shared formatting.
- `src/lib/AGENTS.md` owns the typed API boundary, websocket singleton, shared reference-data caches, and reporting-currency normalization.
- `src/lib/api/AGENTS.md` owns the typed client module split, shared request plumbing, and selected-profile route matcher beneath `api.ts`.
- `src/lib/websocket/AGENTS.md` owns the helper split beneath the singleton realtime client.
- `tests/AGENTS.md` owns the test split between Playwright flows and contract seams.

## WHERE TO LOOK
- Mounted routes, auth/public split, protected shell mounts, and profile/reporting-currency provider handoff: `src/App.tsx`
- Shell chrome, sidebar entries, profile-prefixed navigation, version label, and profile switcher: `src/components/AGENTS.md`, `src/components/layout/app-layout/AGENTS.md`
- Shared widgets, shell-safe controls, and design-system wrappers: `src/components/AGENTS.md`, `src/components/ui/AGENTS.md`
- shadcn registry config and Tailwind entrypoint: `components.json`, `src/index.css`
- Provider stack and browser mount (`LocaleProvider` -> `ThemeProvider` -> `TooltipProvider` -> `App` + `Toaster`): `src/main.tsx`
- Auth bootstrap, selected-profile `X-Profile-Id` scoping, and reporting-currency readiness: `src/context/AGENTS.md`, `src/context/auth/AGENTS.md`, `src/context/profile/AGENTS.md`, `src/context/ReportingCurrencyContext.tsx`
- Typed API boundary, shared request plumbing, and reporting-currency cache and normalization: `src/lib/AGENTS.md`, `src/lib/api/AGENTS.md`, `src/lib/reportingCurrency.ts`, `src/lib/api.ts`
- Realtime websocket ownership and consumers: `src/lib/websocket.ts`, `src/lib/websocket/AGENTS.md`, `src/hooks/useRealtimeData.ts`
- Shared vendor cache and profile-revision keyed reference-data invalidation: `src/lib/referenceData.ts`
- Frontend locale state and shared formatting: `src/i18n/LocaleProvider.tsx`, `src/i18n/format.ts`
- Vite version injection, optional same-origin proxying, dev or preview `/health`, launcher proxy env path, launcher port `5173` to the selected bootstrap file's backend port, and build metadata: `vite.config.ts`, `package.json`
- Production `dist/` server and `/health`: `server.mjs`
- Test split and browser config: `tests/AGENTS.md`, `tests/e2e/`, `tests/lib/`, `playwright.config.ts`
- Page hierarchy and route-domain handoff, including the global sidecars route: `src/pages/AGENTS.md`, `src/pages/sidecars/AGENTS.md`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Node is `>=24`, package management is `pnpm@10.30.1`, and frontend scripts are `dev`, `build`, `lint`, `preview`, and `test:e2e`.
- Treat `src/App.tsx` as the source of truth for routes and shell boundaries.
- Keep selected profile separate from active runtime routing. `selectedProfile` scopes profile-scoped management APIs; it does not switch proxy traffic or global sidecar management.
- Keep `src/components/` focused on shared shell chrome, shared widgets, and design-system wrappers, and keep the leaf ownership documented below it.
- Keep backend access on the typed `src/lib/api.ts` boundary and the modules it re-exports.
- Keep startup bootstrap rendering dependent on backend-provided values. The frontend owns no canonical backend startup default table; fresh defaults are backend source of truth, including `8000`, `5173`, `15432`, `300s`, and `10s`.
- Keep reporting-currency provider state in `src/context/ReportingCurrencyContext.tsx` and shared cache and normalization in `src/lib/reportingCurrency.ts` instead of duplicating settings-side currency bootstrap in pages.
- Keep realtime ownership in `src/lib/websocket.ts` and consume it through hooks instead of creating ad hoc clients.
- Keep locale state and shared formatting in `src/i18n/`, not in shell or page code.
- Keep shadcn/ui additions aligned with `components.json`: `style` `new-york`, Tailwind CSS in `src/index.css`, `lucide` icons, aliases rooted at `@/`, and generated primitives under `src/components/ui/`.
- Use existing `ui/` primitives and local wrappers before adding one-off markup in pages or shared widgets.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS
- Do not add generic React, Vite, or test-runner boilerplate here.
- Do not invent routes, shell entries, or page hierarchies beyond `src/App.tsx` and `src/pages/AGENTS.md`.
- Do not blur selected-profile management scope with active runtime routing.
- Do not duplicate reporting-currency cache, normalization, or readiness logic outside `src/context/ReportingCurrencyContext.tsx` and `src/lib/reportingCurrency.ts`.
- Do not duplicate websocket, reference-data, or navigation-config ownership in page docs.
- Do not put route state, data fetching, or shell navigation into `src/components/ui/`; it is a design-system leaf.
