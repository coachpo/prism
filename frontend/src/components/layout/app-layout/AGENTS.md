# FRONTEND APP LAYOUT CLUSTER KNOWLEDGE BASE

## OVERVIEW
`components/layout/app-layout/` owns the protected shell mounted from `src/components/layout/page.tsx`: sidebar/header composition, navigation metadata, breadcrumb handoff, user footer, and visible version label. Management requests remain pinned to profile id `1` by the API client without visible scope chrome.

## STRUCTURE
```text
app-layout/
├── AppSidebar.tsx              # Sidebar navigation and footer composition
├── SidebarFooterStatus.tsx     # Sidebar footer auth/status strip and version label
├── SiteHeader.tsx              # Shell header chrome, sidebar trigger, and breadcrumbs
├── HeaderAccountMenu.tsx       # Header account menu: username and logout action
├── GlobalSearch.tsx            # Header global search over the sidebar item set
├── DensityToggle.tsx           # Operator density switch rendered in the shell
├── densityMode.ts              # Density read/write/apply helpers and `prism.density.v1` storage key
├── BreadcrumbEntityProvider.tsx # Provider for the route-published breadcrumb entity label
├── breadcrumbEntity.ts         # Breadcrumb entity contexts plus publish/read hooks
├── useAppLayoutState.ts        # Shell composition over auth and sidebar state
├── useShellNavigation.ts       # Nav links, route matching, and breadcrumbs
├── sidebarPersistence.ts       # Sidebar collapsed-state localStorage helpers
└── *.test.ts                   # Shell navigation coverage
```

## WHERE TO LOOK

- Shell composition and protected route children handoff: `../page.tsx`
- Sidebar links, route matching, and breadcrumbs: `useShellNavigation.ts`
- Auth composition, sidebar state, and logout flow: `useAppLayoutState.ts`
- Sidebar collapsed-state persistence helpers: `sidebarPersistence.ts`
- Account menu and logout control: `HeaderAccountMenu.tsx`; sidebar footer status strip: `SidebarFooterStatus.tsx`
- Shell header chrome and breadcrumb presentation: `SiteHeader.tsx`; version surface: `../../../lib/appVersion.ts`
- Breadcrumb entity published by a route and read by the header: `breadcrumbEntity.ts`, `BreadcrumbEntityProvider.tsx`
- Operator density switch and its persistence: `DensityToggle.tsx`, `densityMode.ts`
- Header global search over sidebar items: `GlobalSearch.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `page.tsx` thin. State composition belongs in `useAppLayoutState.ts`.
- Keep navigation, route matching, and breadcrumbs in `useShellNavigation.ts`; keep version-label construction in `../../../lib/appVersion.ts`.
- Use `useAuth()` through `useAppLayoutState.ts`; route shells should not duplicate shell bootstrap logic.
- Keep logout in `HeaderAccountMenu.tsx`; `SidebarFooterStatus.tsx` and `AuthPageShell.tsx` render the version surface from `../../../lib/appVersion.ts`, while the shell footer carries status only.
- Keep the shell limited to the mounted `page.tsx` wrapper and retained seams above.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move route-specific query or data-fetch logic into the shell cluster.
- Do not duplicate nav-link definitions, route matching, or breadcrumb logic outside `useShellNavigation.ts`; do not duplicate version-label construction outside `../../../lib/appVersion.ts`.
- Do not reintroduce profile-selection UI, profile dialogs, or Default-vs-runtime mismatch UI.
- Do not blur pinned management scope with runtime proxy semantics.
