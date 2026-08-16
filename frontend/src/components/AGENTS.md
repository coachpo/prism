# FRONTEND COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`src/components/` holds Prism's shared shell chrome and reusable UI. The dense shell-state cluster lives under `layout/app-layout/`, while this parent owns shared widgets and design-system leaves that compose checked-in shadcn/ui primitives without taking over route state.

## STRUCTURE
```text
components/
├── AnimatedListItem.tsx                               # Shared animated list row used across route surfaces
├── ApiFamilyIcon.tsx + ApiFamilySelect.tsx            # Shared API-family icon and picker helpers
├── CopyButton.tsx                                     # Shared copy affordance
├── IconActionGroup.tsx                                # Shared icon action cluster
├── SpendTrustIndicator.tsx                            # Shared spend trust and fallback note
├── layout/app-layout/AGENTS.md                        # Post-upgrade shell cluster behind the mounted page wrapper
├── loadbalance/AGENTS.md                              # Retired loadbalance renderers (superseded by features/loadbalance + features/observe)
├── statistics/TopSpendingCard.tsx                     # Shared statistics renderer
└── ui/AGENTS.md                                       # shadcn/ui primitives and local wrappers
```

## WHERE TO LOOK
- Shell chrome and layout handoff: `layout/page.tsx`
- Shell state cluster plus nav/version ownership: `layout/app-layout/AGENTS.md`
- Shared theme control: `ThemeToggle.tsx`
- Shared list, copy, icon action, and spend-trust note widgets: `AnimatedListItem.tsx`, `ApiFamilyIcon.tsx`, `ApiFamilySelect.tsx`, `CopyButton.tsx`, `IconActionGroup.tsx`, `SpendTrustIndicator.tsx`
- Shared loadbalance rendering is retired: the old badges/table/detail-sheet components were deleted; the routing-policy config surface and the Observe 路由健康 tab own the current renderers
- Shared statistics rendering: `statistics/TopSpendingCard.tsx`
- Design-system primitives and local wrappers: `ui/`
- shadcn registry source of truth for `ui/`: `../../components.json`, `../index.css`

## CHILD DOCS
- `layout/app-layout/AGENTS.md`: mounted shell chrome, sidebar navigation, user footer, and visible version-label ownership.
- `loadbalance/AGENTS.md`: retired loadbalance renderer ownership (no active components).
- Shared statistics renderer: `statistics/TopSpendingCard.tsx`; page-specific orchestration stays under the owning feature or page instructions.
- `ui/AGENTS.md`: shadcn/ui primitives and local wrappers in `src/components/ui/`.

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep shared components presentation-first.
- Keep data fetching and route state out of this tree.
- Keep shell-state ownership in `layout/app-layout/`; the direct components here should stay compositional or presentational.
- Keep theme controls in shared preference widgets instead of duplicating them in auth pages or shell headers.
- Keep shared spend-trust fallback copy in `SpendTrustIndicator.tsx` instead of duplicating it across dashboard, models, statistics, or request-log views.
- Reuse `ui/` primitives before adding one-off markup, and prefer local wrappers in `ui/` when a pattern belongs to the design system.
- Keep semantic Tailwind tokens, `cn(...)` class composition, and shadcn variant/size props in shared components instead of raw color overrides or bespoke primitive copies.
- Keep the leaf docs in `ui/` for primitive-level wrappers, and keep this parent focused on the shared widgets above them.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not move nav-link or version-label logic out of `layout/app-layout/`.
- Do not put page-specific fetches or route-state parsing in shared components.
- Do not refer to deleted shell files or the old shell wrapper, header, or profile popover surfaces as live shared components.
