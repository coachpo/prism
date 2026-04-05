# FRONTEND APP LAYOUT CLUSTER KNOWLEDGE BASE

## OVERVIEW
`components/layout/app-layout/` is the dense shell-state cluster behind `../AppLayout.tsx`. It owns sidebar and header composition, profile switcher behavior, profile dialog state, shell-only route scoping, profile mismatch messaging, the visible version label, and the shell-navigation handoff into route-scoped links.

## STRUCTURE
```
app-layout/
├── AppHeader.tsx                 # Shell header, profile switcher trigger, shell controls
├── AppSidebar.tsx                # Sidebar navigation and collapse behavior
├── ProfileDialogs.tsx            # Create, edit, activate, and delete profile dialogs
├── ProfileSwitcherPopover.tsx    # Profile switcher list, search, and actions
├── useAppLayoutState.ts          # Shell composition over auth, profile, dialogs, sidebar state, and route-scope checks
├── useProfileDialogState.ts      # Profile dialog open state and mutation handlers
├── useProfileSwitcherState.ts    # Switcher filtering, focus, and selection behavior
├── useShellNavigation.ts         # Breadcrumb and route-scope handoff for settings hashes and request-log detail mode
├── profileConflictMessageParser.ts # Profile-limit and conflict messaging helpers
└── navigationProfileConfig.ts    # Nav links, profile-scoped prefixes, profile cap, version label
```

## WHERE TO LOOK

- Shell composition and `Outlet` handoff: `../AppLayout.tsx`
- Sidebar links, profile-scoped route prefixes, max profile count, and visible version label: `navigationProfileConfig.ts`
- Breadcrumb and route-scope handoff for settings hashes and request-log detail mode: `useShellNavigation.ts`, `navigationProfileConfig.ts`
- Auth/profile context composition, sidebar state, route-scope detection, and logout flow: `useAppLayoutState.ts`
- Dialog open state and profile CRUD/activate/delete handlers: `useProfileDialogState.ts`, `ProfileDialogs.tsx`
- Switcher filtering, focus management, and selected-profile handoff: `useProfileSwitcherState.ts`, `ProfileSwitcherPopover.tsx`
- Conflict copy parsing for profile-limit and duplicate-name flows: `profileConflictMessageParser.ts`

## CONVENTIONS

- Keep `AppLayout.tsx` thin. State composition belongs in `useAppLayoutState.ts`.
- Keep navigation, profile-scoped prefixes, profile cap, and version-label formatting in `navigationProfileConfig.ts`.
- Keep breadcrumb, settings-hash, and request-log detail handoff in `useShellNavigation.ts`.
- Use `useAuth()` and `useProfileContext()` through `useAppLayoutState.ts`; route shells should not duplicate shell bootstrap logic.
- Keep dialog mutation handlers in `useProfileDialogState.ts`, with `ProfileDialogs.tsx` staying presentation-focused.
- Keep profile switcher filtering and focus behavior in `useProfileSwitcherState.ts` instead of scattering it across header or popover components.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not move route-specific query or data-fetch logic into the shell cluster.
- Do not duplicate nav-link definitions, profile-scoped prefixes, or version-label logic outside `navigationProfileConfig.ts`.
- Do not bypass the dialog or switcher hooks with ad hoc local state in `AppHeader.tsx` or `AppSidebar.tsx`.
- Do not blur selected-profile shell state with active runtime profile semantics.
