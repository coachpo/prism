# Frontend

Read [DESIGN.md](DESIGN.md) before any UI/UX work.

- `src/App.tsx` clears query/reference snapshots when the auth epoch changes. Update the route definitions, shell navigation, and relevant route tests together.
- `src/pages/` contains actively reused page components and domain helpers; existing imports determine ownership, not the directory's age. Keep data lifecycles out of shared presentation components.
- Use the public `src/lib/api.ts` and `src/lib/types.ts` boundaries for management contracts. Runtime self-tests deliberately use their separate runner under `src/features/runtime-self-test/`.
- Management profile scope is pinned to Default id `1`. Global auth/proxy-key controls and runtime proxy traffic have distinct scope.
- Shared lookups use `src/lib/referenceData.ts`; pages must not establish competing caches.
- Reusable copy and formatting belong to `src/i18n/`; timestamp consumers use `src/hooks/useTimezone.ts` and `src/lib/timezone.ts`.
- Frontend environment variables are transport/build wiring; `vite.config.ts` owns optional same-origin proxying.
- Type-check with `pnpm exec tsc -b` or `pnpm run build`; `tsc --noEmit -p tsconfig.json` checks no application files because the root config contains only references.

- Keep reusable widgets in `src/components/` presentation-focused; route-specific data, filters, and mutation state stay with feature/page owners. Extend the shared API-family icon/label owners (`ApiFamilyIcon.tsx`, `apiFamilyPresentation.ts`, `ApiFamilySelect.tsx`) instead of copying provider labels into pages.
