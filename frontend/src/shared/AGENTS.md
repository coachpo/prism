# FRONTEND SHARED KNOWLEDGE BASE

## OVERVIEW
`frontend/src/shared/` owns rewrite-era cross-route helper modules that are not page, feature, or primitive-specific: API query keys/invalidation, server validation shaping, table row helpers, and design-system exports.

## STRUCTURE
```text
shared/
├── api/            # Query-key and mutation-invalidation helpers
├── design-system/  # Shared design-system export barrel
├── forms/          # Backend/server validation issue shaping
├── table/          # Rewrite table column/row helpers
└── index.ts        # Public shared export barrel
```

## WHERE TO LOOK
- Public shared export surface: `index.ts`
- Rewrite query key types, profile-id scope, and invalidation scope helpers: `api/queryKeys.ts`, `api/queryInvalidation.ts`
- Server validation extraction and field-error formatting: `forms/serverValidation.ts`
- Table helper exports: `table/rewriteTable.ts`
- Design-system barrel: `design-system/`
- Consumers in feature routes and seam tests: `../features/`, `../test/`, `../../tests/lib/`

## CONVENTIONS
- Keep this directory framework-level and cross-route. Feature-only schemas, payload builders, and mutation hooks belong beside their route feature.
- Keep selected-profile query scope explicit in shared query keys. Global control surfaces should use global scope helpers, not fake profile ids.
- Keep server validation helpers shape-preserving so backend field paths remain visible to route forms.
- Keep `index.ts` as the public barrel; avoid deep imports from feature code unless a helper intentionally stays private.

## ANTI-PATTERNS
- Do not move shadcn primitives or route-specific table markup here.
- Do not hide backend validation field names behind generic messages.
- Do not create shared query invalidation shortcuts that blur selected-profile, active-runtime, and global scopes.
