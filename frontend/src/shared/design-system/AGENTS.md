# FRONTEND DESIGN SYSTEM KNOWLEDGE BASE

## OVERVIEW
`frontend/src/shared/design-system/` owns Prism's reusable operator UI layer above shadcn primitives: page shells, sections, controls, status surfaces, metrics, and table chrome.

## STRUCTURE
```text
design-system/
├── foundation.ts       # Shared class helpers and foundation tokens
├── page.tsx            # Page/frame primitives
├── section.tsx         # Section layout primitives
├── controls.tsx        # Common control wrappers
├── status.tsx          # Status/badge helpers
├── state-surfaces.tsx  # Empty/loading/error surfaces
├── metrics.tsx         # Metric display primitives
├── table-shell.tsx     # Dense operator table chrome
└── index.ts            # Public barrel
```

## WHERE TO LOOK
- Public imports: `index.ts`
- Layout primitives: `page.tsx`, `section.tsx`
- Dense tables and metrics: `table-shell.tsx`, `metrics.tsx`
- Controls and statuses: `controls.tsx`, `status.tsx`, `state-surfaces.tsx`
- Design rules: `../../../DESIGN.md`
- Underlying generated primitives: `../../components/ui/AGENTS.md`

## CONVENTIONS
- Import this layer as `@/shared/design-system` before reaching into `@/components/ui`.
- Keep components route-agnostic: no API calls, route params, selected-profile state, websocket subscriptions, or i18n copy ownership.
- Use semantic tokens, operator surface classes, density variables, and existing shadcn/lucide primitives.
- Keep variants small and current-use only. Add a component when two routes really share the same UI structure.
- Preserve accessibility basics from the underlying primitive: labels, focus state, disabled state, and keyboard behavior.

## ANTI-PATTERNS
- Do not add marketing hero layouts, decorative gradients, blur blobs, raw Tailwind status colors, or page-local color systems.
- Do not wrap every shadcn primitive "just in case"; keep wrappers tied to Prism operator surfaces.
- Do not move feature-specific table columns, forms, or copy into this directory.
