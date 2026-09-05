# Frontend

Read [DESIGN.md](DESIGN.md) before any UI/UX work. It owns visual, interaction, density, accessibility, and feedback rules; use `@/shared/design-system` before primitive imports. Development setup and shared checks belong to [CONTRIBUTING.md](../CONTRIBUTING.md).

- Route construction and search contracts live in `src/app/router/appRouter.tsx` and `src/app/router/rewriteRoutes.ts`; `src/App.tsx` composes providers and clears query/reference snapshots when the auth epoch changes. Update the route definitions, shell navigation, and relevant route tests together.
- `src/features/` owns protected route composition. `src/pages/` contains actively reused page components and domain helpers; existing imports determine ownership, not the directory's age. Keep data lifecycles out of shared presentation components.
- Use the public `src/lib/api.ts` and `src/lib/types.ts` boundaries for management contracts. Runtime self-tests deliberately use their separate runner under `src/features/runtime-self-test/`.
- Management profile scope is pinned to Default id `1`; `src/lib/api/request.ts` owns its header. Global auth/proxy-key controls and runtime proxy traffic have distinct scope. Do not add a profile selector.
- `src/context/ReportingCurrencyContext.tsx` owns reporting-currency readiness, backed by `src/lib/reportingCurrency.ts`. Shared lookups use `src/lib/referenceData.ts`; pages must not establish competing caches.
- The UI locale is `zh-CN`. Reusable copy and formatting belong to `src/i18n/`; timestamp consumers use `src/hooks/useTimezone.ts` and `src/lib/timezone.ts`.
- Bootstrap settings remain file-backed. Frontend environment variables are transport/build wiring; `vite.config.ts` owns optional same-origin proxying. Production serving belongs to the root Dockerfile and Nginx bundle.
- `package.json` pins Node `>=24` and pnpm. Type-check with `pnpm exec tsc -b` or `pnpm run build`; `tsc --noEmit -p tsconfig.json` checks no application files because the root config contains only references.

Run the affected existing test layer, then applicable build/lint gates from `frontend/`: `pnpm exec vitest run`, `pnpm run test:lib`, `pnpm run test:e2e`, `pnpm run build`, and `pnpm run lint`. Runner-specific guidance lives in [tests/AGENTS.md](tests/AGENTS.md).

Local guidance covers [routing](src/app/AGENTS.md), [features](src/features/AGENTS.md), [page domains](src/pages/AGENTS.md), [components](src/components/AGENTS.md), [contexts](src/context/AGENTS.md), [locale](src/i18n/AGENTS.md), [shared helpers](src/shared/AGENTS.md), and [API/browser infrastructure](src/lib/AGENTS.md). Follow the narrowest owning guide rather than copying its rules into a parent.
