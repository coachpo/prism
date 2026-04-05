# Prism Frontend

React 19 management dashboard for Prism. This package owns the browser UI, the typed frontend API boundary, profile-scoped management flows, realtime updates, and the route shells for dashboard, models, model detail, endpoints, loadbalance strategies, statistics, settings, proxy API keys, pricing templates, and request logs.

## Frontend-only commands

```bash
pnpm install
pnpm run dev
pnpm run build
pnpm run lint
pnpm run test
```

Prism targets Node.js 24+ and uses the `pnpm@10.30.1` toolchain declared in `package.json`.

When started through the checked-in root launcher, Prism serves the frontend at `http://localhost:15173`. For full-stack local setup, launcher behavior, and shared repository context, start at `../README.md` and `./AGENTS.md`.

## Runtime notes

- `VITE_API_BASE` is optional. If it is unset, the frontend uses same-origin requests to `/api`, `/v1`, and `/v1beta`.
- Local Vite development does not install a dev proxy in `vite.config.ts`; use `VITE_API_BASE` directly or launch the full stack through `../start.sh full`.
- The production container serves the built `dist/` output through `server.mjs`, which also exposes `/health`.

## Route and ownership map

- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- `/` redirects to `/dashboard`
- `src/App.tsx` mounts the public auth routes plus the protected shell routes for dashboard, models, model detail, `/models/:id/proxy`, endpoints, loadbalance strategies, statistics, settings, pricing templates, proxy API keys, and request logs.
- `src/pages/` owns route-domain shells and their dense local feature folders.
- `src/main.tsx` owns browser mounting plus the locale, theme, tooltip, and toast providers.
- `src/lib/api.ts` is the public typed API boundary.
- `src/lib/websocket.ts` owns the realtime client used by `useRealtimeData()`.
- `src/context/` owns auth bootstrap and selected-profile management scope.
- `src/components/` owns shared shell chrome and cross-route widgets, including loadbalance and statistics helpers.
- `src/components/layout/app-layout/navigationProfileConfig.ts` owns shell nav links, profile-scoped prefixes, and the visible version label.

For deeper implementation boundaries, use `src/pages/AGENTS.md`, `src/lib/AGENTS.md`, `src/context/AGENTS.md`, and nearby feature docs.

## shadcn workflow

Prism uses the checked-in shadcn registry configuration in `components.json`, and `src/index.css` still imports `shadcn/tailwind.css`. Keep that workflow intact when adding or updating UI primitives.

```bash
pnpm dlx shadcn add button
pnpm dlx shadcn add dialog
pnpm dlx shadcn add table
```

Generated primitives belong under `src/components/ui/`.
