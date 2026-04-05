# FRONTEND UI PRIMITIVES KNOWLEDGE BASE

## OVERVIEW
`src/components/ui/` holds Prism's checked-in shadcn/ui primitives and the local wrappers that sit on top of them. The folder follows the registry config in `components.json`, so treat it as the design-system leaf, not a place for route logic or shared shell state.

## STRUCTURE
```text
ui/
├── button.tsx
├── dialog.tsx
├── sidebar.tsx
├── chart.tsx
├── status-dot.tsx
├── topography.tsx
└── ... other shadcn/ui primitives and local wrappers
```

## WHERE TO LOOK
- Registry-backed primitive set and checked-in component inventory: `components.json`, files in `ui/`
- Recharts-aware chart helpers and i18n format hooks: `chart.tsx`
- Responsive sidebar provider and shell-friendly sidebar pieces: `sidebar.tsx`
- Intent-based status indicator styling: `status-dot.tsx`
- Animated background wrapper used by local surfaces: `topography.tsx`

## CONVENTIONS
- Keep this folder focused on primitives and local wrappers only.
- Keep shell composition, navigation, and route state out of these files.
- Prefer adapting the local wrapper here before adding one-off styling in parent components.
- Preserve the checked-in shadcn registry flow when adding new primitives.

## ANTI-PATTERNS
- Do not move shell navigation or profile logic into `ui/`.
- Do not add route-aware data fetching here.
- Do not treat these files as generic shared widgets when a higher-level component doc owns the seam.
