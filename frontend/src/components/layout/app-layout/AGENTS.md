# Protected shell

- Keep `../page.tsx` compositional. `useAppLayoutState.ts` owns shell state/auth handoff; `useShellNavigation.ts` owns sidebar links, route matching, and breadcrumbs. `GlobalSearch.tsx` searches that same navigation set.
- Route entity labels publish through `breadcrumbEntity.ts` and `BreadcrumbEntityProvider.tsx`; do not duplicate route-name resolution in the header.
- `HeaderAccountMenu.tsx` owns logout. `SidebarFooterStatus.tsx` reads the version surface from `../../../lib/appVersion.ts`; keep version formatting there.
- `sidebarPersistence.ts` owns collapsed-state storage; `densityMode.ts` owns density read/write/application and is consumed by `DensityToggle.tsx`.
- Shell navigation remains independent of page queries and runtime routing. Management profile id `1` does not create a visible profile-selection state.
