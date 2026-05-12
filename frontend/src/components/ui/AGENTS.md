# FRONTEND UI PRIMITIVES KNOWLEDGE BASE

## OVERVIEW
`src/components/ui/` holds Prism's checked-in shadcn/ui primitives and local wrappers. The folder follows `frontend/components.json`, so treat it as the design-system leaf, not a place for route logic, data fetching, or shell state.

## STRUCTURE
```text
ui/
├── button.tsx
├── dialog.tsx
├── sidebar.tsx
├── chart.tsx
├── spinner.tsx
├── status-dot.tsx
├── topography.tsx
└── ... other shadcn/ui primitives and local wrappers
```

## WHERE TO LOOK
- Registry-backed primitive set and checked-in component inventory: `components.json`, files in `ui/`
- Recharts-aware chart helpers and i18n format hooks: `chart.tsx`
- Responsive sidebar provider and shell-friendly sidebar pieces: `sidebar.tsx`
- Shared loading primitive: `spinner.tsx`
- Intent-based status indicator styling: `status-dot.tsx`
- Animated background wrapper used by local surfaces: `topography.tsx`

## CONVENTIONS

- When doing upgrade work, first account for this project stage: This application is under development, it doesn't have users at the moment. Backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested; prefer the best current implementation shape over preserving the old one, and do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.
- Keep this folder focused on primitives and local wrappers only.
- Keep shell composition, navigation, and route state out of these files.
- Prefer adapting the local wrapper here before adding one-off styling in parent components.
- Preserve the checked-in shadcn registry flow when adding new primitives: style `new-york`, Tailwind CSS in `src/index.css`, neutral base color, `lucide` icons, and aliases from `components.json`.
- Use semantic tokens and component variants before raw colors or ad hoc CSS. Compose conditional classes with `cn(...)`.
- Keep accessibility composition intact for dialogs, sheets, popovers, tabs, and menus; do not remove required titles or grouping wrappers from vendored primitives.

## ANTI-PATTERNS
- Do not move shell navigation or profile logic into `ui/`.
- Do not add route-aware data fetching here.
- Do not treat these files as generic shared widgets when a higher-level component doc owns the seam.
- Do not manually overwrite vendored primitive APIs without checking the local shadcn registry config and existing wrapper usage.
