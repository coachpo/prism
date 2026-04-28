# FRONTEND APP LAYOUT CLUSTER KNOWLEDGE BASE

## OVERVIEW
`components/layout/app-layout/` owns the post-upgrade protected shell mounted from `src/components/layout/page.tsx` and wired in `src/App.tsx`. It now covers sidebar and header composition, profile switching, profile mismatch messaging, the visible version label, and the shell navigation handoff into route-scoped links.

## STRUCTURE
```text
app-layout/
├── AppSidebar.tsx              # Sidebar navigation, profile switcher, mismatch footer, and user footer composition
├── MismatchFooter.tsx          # Selected-vs-running profile strip and activate action
├── NavUser.tsx                 # Footer user menu, version label, locale/theme/logout actions
├── ProfileDialogs.tsx          # Create, edit, activate, and delete profile dialogs
├── ProfileSwitcher.tsx         # Sidebar header profile switcher trigger and actions
├── SiteHeader.tsx              # Shell header chrome, sidebar trigger, and breadcrumbs
├── useAppLayoutState.ts        # Shell composition over auth, profile, dialogs, sidebar state, and route-scope checks
├── useProfileDialogState.ts    # Profile dialog open state and mutation handlers
├── useProfileSwitcherState.ts  # Switcher open, close, and select behavior
├── useShellNavigation.ts       # Breadcrumb and route-scope handoff for settings hashes and request-log detail mode
├── profileConflictMessageParser.ts # Profile-limit and conflict messaging helpers
└── navigationProfileConfig.ts  # Nav links, profile-scoped prefixes, profile cap, version label
```

## WHERE TO LOOK

- Shell composition and `Outlet` handoff: `../page.tsx`
- Sidebar links, profile-scoped route prefixes, max profile count, and visible version label: `navigationProfileConfig.ts`
- Breadcrumb and route-scope handoff for settings hashes and request-log detail mode: `useShellNavigation.ts`, `navigationProfileConfig.ts`
- Auth/profile context composition, sidebar state, route-scope detection, and logout flow: `useAppLayoutState.ts`
- Dialog open state and profile CRUD/activate/delete handlers: `useProfileDialogState.ts`, `ProfileDialogs.tsx`
- Switcher open, close, and selection behavior: `useProfileSwitcherState.ts`, `ProfileSwitcher.tsx`
- Conflict copy parsing for profile-limit and duplicate-name flows: `profileConflictMessageParser.ts`
- Shell footer user actions, locale/theme/logout controls, and version label ownership: `NavUser.tsx`
- Shell header chrome and breadcrumb presentation: `SiteHeader.tsx`
- Profile mismatch strip and activate action: `MismatchFooter.tsx`

## CONVENTIONS

- Keep `page.tsx` thin. State composition belongs in `useAppLayoutState.ts`.
- Keep navigation, profile-scoped prefixes, profile cap, and version-label formatting in `navigationProfileConfig.ts`.
- Keep breadcrumb, settings-hash, and request-log detail handoff in `useShellNavigation.ts`.
- Use `useAuth()` and `useProfileContext()` through `useAppLayoutState.ts`; route shells should not duplicate shell bootstrap logic.
- Keep dialog mutation handlers in `useProfileDialogState.ts`, with `ProfileDialogs.tsx` staying presentation-focused.
- Keep profile switcher open, close, and selection behavior in `useProfileSwitcherState.ts` instead of scattering it across header or sidebar components.
- Keep footer preferences, logout, and version-label concerns in `NavUser.tsx`.
- Keep the post-upgrade shell limited to the mounted `page.tsx` wrapper and the retained seams above. Do not add back the deleted legacy shell wrapper, header, or profile popover surfaces.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not move route-specific query or data-fetch logic into the shell cluster.
- Do not duplicate nav-link definitions, profile-scoped prefixes, or version-label logic outside `navigationProfileConfig.ts`.
- Do not bypass the dialog or switcher hooks with ad hoc local state in `SiteHeader.tsx`, `AppSidebar.tsx`, or `ProfileSwitcher.tsx`.
- Do not reintroduce deleted shell files or stale shell seams from the old shell wrapper, header, or profile popover path.
- Do not blur selected-profile shell state with active runtime profile semantics.