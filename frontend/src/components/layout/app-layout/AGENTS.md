# Protected shell

- Keep `../page.tsx` compositional. `useShellNavigation.ts` owns sidebar links, route matching, and breadcrumbs. `GlobalSearch.tsx` searches that same navigation set.
- Route entity labels publish through `breadcrumbEntity.ts` and `BreadcrumbEntityProvider.tsx`; do not duplicate route-name resolution in the header.
- `SidebarFooterStatus.tsx` reads the version surface from `../../../lib/appVersion.ts`; keep version formatting there.
- Shell navigation remains independent of page queries and runtime routing.
