# Prism Frontend

React 19 management dashboard for Prism. This package owns the browser UI, the typed frontend API boundary, profile-scoped management flows, realtime updates, and the route shells for observe, request logs, models, model detail, endpoints, loadbalance strategies, settings, proxy API keys, and pricing templates.

## Frontend-only commands

```bash
pnpm install
pnpm run dev
pnpm run test:lib
pnpm run build
pnpm run lint
```

Prism targets Node.js 24+ and uses the `pnpm@10.30.1` toolchain declared in `package.json`.

Use `frontend/.env.example` as the standalone frontend env sample when you want to point the frontend at a non-launcher backend.

When started through the checked-in root launcher, Prism serves the frontend at `http://localhost:5173` and proxies same-origin backend traffic to the selected bootstrap file's listener port. With the checked-in `../config.json`, that backend URL is `http://localhost:18000`. For full-stack local setup, launcher behavior, and shared repository context, start at `../README.md` and `./AGENTS.md`.

## Runtime notes

- `VITE_API_BASE` is optional. If it is unset, the frontend uses same-origin requests to `/api`, `/v1`, and `/v1beta`.
- `../start.sh full` enables a launcher-only Vite proxy so browser traffic stays same-origin while `/api`, `/v1`, and `/v1beta` reach the selected bootstrap file's configured backend port.
- Standalone frontend development can still use explicit `VITE_API_BASE` when you want the dev server to talk to a remote backend.
- The production container serves the built `dist/` output through `server.mjs`, which also exposes `/health`.

## Route and ownership map

- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- `/` redirects to `/dashboard`
- `src/App.tsx` mounts the public auth routes plus the protected shell routes for observe, request logs, models, model detail, endpoints, loadbalance strategies, settings, proxy API keys, and pricing templates.
- `src/pages/` owns compatibility route-domain clusters still imported by feature routes.
- `src/main.tsx` owns browser mounting plus the locale, theme, tooltip, and toast providers.
- `src/lib/api.ts` is the public typed API boundary.
- `src/lib/websocket.ts` owns the realtime client used by `useRealtimeData()`.
- `src/context/` owns auth bootstrap and the frozen Default-profile management scope.
- `src/components/` owns shared shell chrome and cross-route widgets, including loadbalance and statistics helpers.
- `src/components/layout/app-layout/` owns shell nav links, profile-scoped prefixes, and the visible version label. There is no profile switcher in the shell.

For deeper implementation boundaries, use `src/pages/AGENTS.md`, `src/lib/AGENTS.md`, `src/context/AGENTS.md`, and nearby feature docs.

## shadcn workflow

Prism uses the checked-in shadcn registry configuration in `components.json`, and `src/index.css` still imports `shadcn/tailwind.css`. Keep that workflow intact when adding or updating UI primitives.

```bash
pnpm dlx shadcn add button
pnpm dlx shadcn add dialog
pnpm dlx shadcn add table
```

Generated primitives belong under `src/components/ui/`.
