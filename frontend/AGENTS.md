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

- Keep reusable widgets in `src/components/` presentation-focused; route-specific data, filters, and mutation state stay with feature/page owners. Extend the shared API-family icon/label owners (`ApiFamilyIcon.tsx`, `apiFamilyPresentation.ts`, `ApiFamilySelect.tsx`) instead of copying provider labels into pages.

Run the affected test layer, then the applicable build/lint gates; the commands are in [CONTRIBUTING.md](../CONTRIBUTING.md#tests-checks-and-builds) and runner boundaries in [tests/AGENTS.md](tests/AGENTS.md).
