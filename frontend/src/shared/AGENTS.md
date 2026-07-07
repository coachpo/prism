# FRONTEND SHARED KNOWLEDGE BASE

## OVERVIEW
`frontend/src/shared/` owns rewrite-era cross-route helper modules that are not page, feature, or primitive-specific: API query keys/invalidation, server validation shaping, table row helpers, and the reusable operator design-system layer.

## STRUCTURE
```text
shared/
├── api/            # Query-key and mutation-invalidation helpers
├── design-system/  # Reusable operator component layer
│   └── AGENTS.md   # Page/section/control/table/status rules
├── forms/          # Backend/server validation issue shaping
├── table/          # Rewrite table column/row helpers
└── index.ts        # Public shared export barrel
```

## WHERE TO LOOK
- Public shared export surface: `index.ts`
- Rewrite query key types, profile-id scope, and invalidation scope helpers: `api/queryKeys.ts`, `api/queryInvalidation.ts`
- Server validation extraction and field-error formatting: `forms/serverValidation.ts`
- Table helper exports: `table/rewriteTable.ts`
- Reusable operator design-system components: `design-system/AGENTS.md`
- Consumers in feature routes and seam tests: `../features/`, `../test/`, `../../tests/lib/`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep this directory framework-level and cross-route. Feature-only schemas, payload builders, and mutation hooks belong beside their route feature.
- Keep selected-profile query scope explicit in shared query keys. Global control surfaces should use global scope helpers, not fake profile ids.
- Keep server validation helpers shape-preserving so backend field paths remain visible to route forms.
- Keep `index.ts` as the public barrel; avoid deep imports from feature code unless a helper intentionally stays private.

## ANTI-PATTERNS
- Do not move shadcn primitives or route-specific table markup here.
- Do not hide backend validation field names behind generic messages.
- Do not create shared query invalidation shortcuts that blur selected-profile, active-runtime, and global scopes.
