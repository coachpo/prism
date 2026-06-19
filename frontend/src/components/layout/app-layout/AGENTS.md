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
├── useAppLayoutState.ts        # Shell composition over auth, profile, dialogs, sidebar state, route-scope checks, and can-create state
├── useProfileDialogState.ts    # Profile dialog open state and mutation handlers
├── useProfileSwitcherState.ts  # Switcher open, close, and select behavior
├── useShellNavigation.ts       # Breadcrumb and route-scope handoff for settings hashes and request-log detail mode
├── sidebarPersistence.ts       # Sidebar collapsed-state localStorage helpers
├── profileConflictMessageParser.ts # Profile-limit and conflict messaging helpers
└── navigationProfileConfig.ts  # Nav links, profile-scoped prefixes, route matching, and version label
```

## WHERE TO LOOK

- Shell composition and `Outlet` handoff: `../page.tsx`
- Sidebar links, profile-scoped route prefixes, route matching, and visible version label: `navigationProfileConfig.ts`
- Max-profile limit from `ProfileContext` and derived `canCreateProfile` state: `useAppLayoutState.ts`
- Breadcrumb and route-scope handoff for settings hashes and request-log detail mode: `useShellNavigation.ts`, `navigationProfileConfig.ts`
- Sidebar collapsed-state persistence helpers: `sidebarPersistence.ts`
- Auth/profile context composition, sidebar state, route-scope detection, and logout flow: `useAppLayoutState.ts`
- Dialog open state and profile CRUD/activate/delete handlers: `useProfileDialogState.ts`, `ProfileDialogs.tsx`
- Switcher open, close, and selection behavior: `useProfileSwitcherState.ts`, `ProfileSwitcher.tsx`
- Conflict copy parsing for profile-limit and duplicate-name flows: `profileConflictMessageParser.ts`
- Shell footer user actions, locale/theme/logout controls, and version label ownership: `NavUser.tsx`
- Shell header chrome and breadcrumb presentation: `SiteHeader.tsx`
- Profile mismatch strip and activate action: `MismatchFooter.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid deprecated compatibility wrappers listed there.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `page.tsx` thin. State composition belongs in `useAppLayoutState.ts`.
- Keep navigation, profile-scoped prefixes, route matching, and version-label formatting in `navigationProfileConfig.ts`.
- Keep max-profile limit consumption and create-button gating in `useAppLayoutState.ts`, fed by `ProfileContext`.
- Keep breadcrumb, settings-hash, and request-log detail handoff in `useShellNavigation.ts`.
- Use `useAuth()` and `useProfileContext()` through `useAppLayoutState.ts`; route shells should not duplicate shell bootstrap logic.
- Keep dialog mutation handlers in `useProfileDialogState.ts`, with `ProfileDialogs.tsx` staying presentation-focused.
- Keep profile switcher open, close, and selection behavior in `useProfileSwitcherState.ts` instead of scattering it across header or sidebar components.
- Keep footer preferences, logout, and version-label concerns in `NavUser.tsx`.
- Keep the post-upgrade shell limited to the mounted `page.tsx` wrapper and the retained seams above. Do not add back the deleted legacy shell wrapper, header, or profile popover surfaces.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move route-specific query or data-fetch logic into the shell cluster.
- Do not duplicate nav-link definitions, profile-scoped prefixes, route matching, or version-label logic outside `navigationProfileConfig.ts`.
- Do not make `navigationProfileConfig.ts` the source of truth for profile caps; `ProfileContext` owns that server-provided limit.
- Do not bypass the dialog or switcher hooks with ad hoc local state in `SiteHeader.tsx`, `AppSidebar.tsx`, or `ProfileSwitcher.tsx`.
- Do not reintroduce deleted shell files or stale shell seams from the old shell wrapper, header, or profile popover path.
- Do not blur selected-profile shell state with active runtime profile semantics.
