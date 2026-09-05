# UI primitives

This directory contains checked-in shadcn primitives and their local adaptations. `../../../components.json` owns registry aliases, the `new-york` style, neutral token base, `lucide` icons, and the `src/index.css` entrypoint.

- Keep primitive APIs and accessibility composition consistent with current callers before replacing generated code. Dialog, sheet, menu, and field wrappers must preserve their titles, labels, focus, and keyboard contracts.
- Keep navigation, API calls, route state, and localized feature copy outside this directory. Higher-level reusable operator components belong to `../../shared/design-system/`.
- Preserve the `sidebar-context.ts` context split when changing sidebar primitives; it separates shared context exports from component refresh boundaries.
