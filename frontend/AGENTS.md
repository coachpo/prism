# FRONTEND KNOWLEDGE BASE

## OVERVIEW
`frontend/` is Prism's monorepo-owned management dashboard. It owns the browser-side management contract for dual-family loadbalance strategy selection (`legacy` and `adaptive`) while keeping this doc as the router for the frontend directory, not a deep implementation guide.

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
├── package.json
├── playwright.config.ts
└── vite.config.ts
```

## ROUTE MAP
- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- Protected shell routes: `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/statistics`, `/settings`, `/proxy-api-keys`, `/pricing-templates`, `/request-logs`
- `/` redirects to `/dashboard`

## HIERARCHY
- `src/App.tsx` owns the mounted route surface and stays the source of truth for route mounting and shell boundaries.
- `src/pages/AGENTS.md` owns route-domain handoff for the mounted page surface under `src/pages/`.
- `src/components/AGENTS.md` owns shared shell and widget work, then points down to the layout shell cluster, feature renderers, and `ui/` primitives.
- `src/components/loadbalance/AGENTS.md` and `src/components/statistics/AGENTS.md` own the shared cross-route renderers in those folders.
- `src/components/layout/app-layout/AGENTS.md` owns the shell-navigation and profile-switcher seam, including the handoff from shell state into route-scoped navigation.
- `src/components/ui/AGENTS.md` owns the shadcn/ui primitives and local wrappers checked into `src/components/ui/`.
- `src/lib/websocket/AGENTS.md` owns the helper split beneath the singleton realtime client.
- `tests/AGENTS.md` owns the test split between Playwright flows and contract seams.

## WHERE TO LOOK
- Mounted routes, auth/public split, and protected shell mounts: `src/App.tsx`
- Shell chrome, sidebar entries, profile-prefixed navigation, visible version label, and profile-switcher dialog state: `src/components/AGENTS.md`, `src/components/layout/app-layout/AGENTS.md`
- Shared widgets, shell-safe controls, and design-system wrappers: `src/components/AGENTS.md`, `src/components/ui/AGENTS.md`
- Provider stack, browser mount, and app bootstrap (`LocaleProvider` -> `ThemeProvider` -> `TooltipProvider` -> `App` + `Toaster`): `src/main.tsx`
- Selected-profile state, revision bumps, auth bootstrap, and `X-Profile-Id` management scoping, distinct from active runtime routing: `src/context/AGENTS.md`, `src/context/auth/AGENTS.md`, `src/context/profile/AGENTS.md`
- Typed API boundary and shared request plumbing: `src/lib/AGENTS.md`, `src/lib/api/AGENTS.md`, `src/lib/api.ts`
- Realtime websocket ownership and consumers: `src/lib/websocket.ts`, `src/lib/websocket/AGENTS.md`, `src/hooks/useRealtimeData.ts`
- Shared vendor cache and profile-revision keyed reference-data invalidation: `src/lib/referenceData.ts`
- Frontend locale state and shared formatting: `src/i18n/LocaleProvider.tsx`, `src/i18n/format.ts`
- Vite version injection (`VITE_APP_VERSION`, `VITE_GIT_RUN_NUMBER`, `VITE_GIT_REVISION`), dev or preview `/health`, and build metadata: `vite.config.ts`, `package.json`
- Test split and browser config: `tests/AGENTS.md`, `tests/e2e/`, `tests/lib/`, `playwright.config.ts`
- Page hierarchy and route-domain handoff: `src/pages/AGENTS.md`

## CONVENTIONS
- Treat `src/App.tsx` as the source of truth for routes and shell boundaries.
- Keep page-owned drill-down components in the top-level route map when `src/App.tsx` mounts them, even when they live beside other pages under `src/pages/`.
- Keep selected profile separate from active runtime routing. `selectedProfile` scopes management APIs; it does not switch proxy traffic.
- Keep `src/components/` focused on shared shell chrome, shared widgets, and design-system wrappers, and keep the leaf ownership documented below it.
- Keep backend access on the typed `src/lib/api.ts` boundary and the modules it re-exports.
- Keep realtime ownership in `src/lib/websocket.ts` and consume it through hooks instead of creating ad hoc clients.
- Keep the websocket helper split documented in `src/lib/websocket/AGENTS.md` instead of repeating transport or subscription detail here.
- Keep locale state and shared formatting in `src/i18n/`, not in shell or page code.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not add generic React, Vite, or test-runner boilerplate here.
- Do not invent routes, shell entries, or page hierarchies beyond `src/App.tsx` and `src/pages/AGENTS.md`.
- Do not blur selected-profile management scope with active runtime routing.
- Do not duplicate websocket, reference-data, or navigation-config ownership in page docs.
