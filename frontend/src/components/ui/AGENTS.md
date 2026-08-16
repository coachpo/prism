# FRONTEND UI PRIMITIVES KNOWLEDGE BASE

## OVERVIEW
`src/components/ui/` holds Prism's checked-in shadcn/ui primitives and local wrappers. The folder follows `frontend/components.json`, so treat it as the design-system leaf, not a place for route logic, data fetching, or shell state.

## STRUCTURE
```text
ui/
├── <primitive>.tsx      # One file per checked-in shadcn/ui primitive, named after the registry component
└── sidebar-context.ts   # The one non-component split: sidebar context extracted for fast refresh
```

## WHERE TO LOOK
- Registry-backed primitive set and the current checked-in inventory: `../../../components.json`, plus `ls` of this folder — the file list is not duplicated here because the registry adds to it
- Responsive sidebar provider and shell-friendly sidebar pieces: `sidebar-context.ts`, `sidebar.tsx`
- Empty, field, breadcrumb, alert-dialog, sonner, and table primitives: `empty.tsx`, `field.tsx`, `breadcrumb.tsx`, `alert-dialog.tsx`, `sonner.tsx`, `table.tsx`
- Shared loading primitive: `spinner.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep this folder focused on primitives and local wrappers only.
- Keep shell composition, navigation, and route state out of these files.
- Prefer adapting the local wrapper here before adding one-off styling in parent components.
- Preserve the checked-in shadcn registry flow when adding new primitives: style `new-york`, Tailwind CSS in `src/index.css`, neutral base color, `lucide` icons, and aliases from `components.json`.
- Use semantic tokens and component variants before raw colors or ad hoc CSS. Compose conditional classes with `cn(...)`.
- Keep accessibility composition intact for dialogs, sheets, popovers, tabs, and menus; do not remove required titles or grouping wrappers from vendored primitives.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not move shell navigation or profile logic into `ui/`.
- Do not add route-aware data fetching here.
- Do not treat these files as generic shared widgets when a higher-level component doc owns the seam.
- Do not manually overwrite vendored primitive APIs without checking the local shadcn registry config and existing wrapper usage.
