# FRONTEND DESIGN SYSTEM KNOWLEDGE BASE

## OVERVIEW
`frontend/src/shared/design-system/` owns Prism's reusable operator UI layer above shadcn primitives: page shells, sections, controls, status surfaces, metrics, and table chrome.

## STRUCTURE
```text
design-system/
├── foundation.ts           # Token contract, density modes, status tiers
├── page.tsx                # Page/frame primitives
├── section.tsx             # Section layout primitives
├── destructive-dialog.tsx  # Destructive confirmation shell
├── controls.tsx            # Common control wrappers
├── status.tsx              # Four-tier status/type/value badges and status-label presentation
├── honesty.tsx             # Missing value, staleness badge, clipped badge
├── freshness.tsx           # The "when is this from" bar
├── state-surfaces.tsx      # Empty/loading/error surfaces
├── metrics.tsx             # KPI card and compact metric tile
├── table-shell.tsx         # Dense operator table chrome
└── index.ts                # Public barrel
```

## WHERE TO LOOK
- Public imports: `index.ts`
- Layout primitives: `page.tsx`, `section.tsx`
- Dense tables and metrics: `table-shell.tsx`, `metrics.tsx`
- Controls and statuses: `controls.tsx`, `status.tsx`, `state-surfaces.tsx`
- Honesty Contract surfaces: `honesty.tsx`, `freshness.tsx`, `../../../DESIGN.md` (Honesty Contract)
- Token contract and its executable guard: `foundation.ts`, `../../../tests/lib/design_token_contract.test.mjs`
- Destructive confirmation shell and its adopters: `destructive-dialog.tsx`, `../../../DESIGN.md` (Dialogs And Drawers → Destructive Flows)
- Design rules: `../../../DESIGN.md`
- Underlying generated primitives: `../../components/ui/AGENTS.md`

## CONVENTIONS
- Import this layer as `@/shared/design-system` before reaching into `@/components/ui`.
- Keep components route-agnostic: no API calls, route params, Default-profile management state, polling subscriptions, or i18n copy ownership.
- Use semantic tokens, operator surface classes, density variables, and existing shadcn/lucide primitives.
- Keep variants small and current-use only. Add a component when two routes really share the same UI structure.
- Preserve accessibility basics from the underlying primitive: labels, focus state, disabled state, and keyboard behavior.
- Route every destructive operation through `OperatorDestructiveDialog`; the required preflight/blocking/impact-summary/typed-phrase flow lives in `../../../DESIGN.md` under Dialogs And Drawers.
- Render an absent value with `OperatorMissingValue` or `OperatorValue`; `?? 0` in a render path is a defect. A failed read is `OperatorErrorState`, never an empty state.
- There is exactly one staleness badge design: `OperatorStalenessBadge`. Do not hand-roll a second one.
- Adding or changing a color means updating `operatorColorTokens` and re-running `pnpm run test:lib`; the guard fails on token-value drift and contrast regressions.

## ANTI-PATTERNS
- Do not add marketing hero layouts, decorative gradients, blur blobs, raw Tailwind status colors, or page-local color systems.
- Do not ship visible default copy from this layer; callers pass localized strings from `messages`.
- Do not wrap every shadcn primitive "just in case"; keep wrappers tied to Prism operator surfaces.
- Do not move feature-specific table columns, forms, or copy into this directory.
