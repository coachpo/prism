# FRONTEND APP LAYOUT CLUSTER KNOWLEDGE BASE

## OVERVIEW
`components/layout/app-layout/` owns the protected shell mounted from `src/components/layout/page.tsx`: sidebar/header composition, navigation metadata, breadcrumb handoff, user footer, and visible version label. Management requests remain pinned to profile id `1` by the API client without visible scope chrome.

## STRUCTURE
```text
app-layout/
├── AppSidebar.tsx              # Sidebar navigation and user footer composition
├── NavUser.tsx                 # Footer user menu, version label, theme/logout actions
├── SiteHeader.tsx              # Shell header chrome, sidebar trigger, and breadcrumbs
├── useAppLayoutState.ts        # Shell composition over auth and sidebar state
├── useShellNavigation.ts       # Nav links, route matching, breadcrumbs, and version label
└── sidebarPersistence.ts       # Sidebar collapsed-state localStorage helpers
```

## WHERE TO LOOK

- Shell composition and protected route children handoff: `../page.tsx`
- Sidebar links, route matching, breadcrumbs, and visible version label: `useShellNavigation.ts`
- Auth composition, sidebar state, and logout flow: `useAppLayoutState.ts`
- Sidebar collapsed-state persistence helpers: `sidebarPersistence.ts`
- Shell footer user actions, theme/logout controls, and version label ownership: `NavUser.tsx`
- Shell header chrome and breadcrumb presentation: `SiteHeader.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `page.tsx` thin. State composition belongs in `useAppLayoutState.ts`.
- Keep navigation, route matching, breadcrumbs, and version-label formatting in `useShellNavigation.ts`.
- Use `useAuth()` through `useAppLayoutState.ts`; route shells should not duplicate shell bootstrap logic.
- Keep footer preferences, logout, and version-label concerns in `NavUser.tsx`.
- Keep the shell limited to the mounted `page.tsx` wrapper and retained seams above.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not move route-specific query or data-fetch logic into the shell cluster.
- Do not duplicate nav-link definitions, route matching, or version-label logic outside `useShellNavigation.ts`.
- Do not reintroduce profile-selection UI, profile dialogs, or Default-vs-runtime mismatch UI.
- Do not blur pinned management scope with runtime proxy semantics.
