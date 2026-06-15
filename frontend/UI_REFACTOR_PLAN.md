# UI Refactor Plan

Generated from the current frontend audit for `UI_REFACTOR_GOAL.md`.

## Current Problems

- The app uses one primary UI stack: React 19, Vite, Tailwind CSS v4, shadcn/ui `new-york`, Radix primitives, lucide icons, React Hook Form, TanStack Router/Query/Table, Recharts, React Flow, and Playwright/Vitest-style tests.
- The design-system direction exists but is underused. `src/shared/design-system` had operator state and table helpers, while most pages still imported primitives or older wrappers directly.
- Audit counts at this checkpoint:
  - `@/components/ui/*`: 368 direct imports across 129 files. These are mostly primitive composition sites and should be reduced gradually as higher-level operator components mature.
  - `@/shared/design-system`: 59 imports across 50 files.
  - Deprecated wrapper usage in product code: 0 active imports/usages for `PageHeader`, `EmptyState`, `SemanticCallout`, `StatusBadge`, `MetricCard`, `CompactMetricTile`, and `SwitchController`.
  - `space-x/space-y`: 111 hits across 42 files. These remain a follow-up density cleanup target.
  - Raw Tailwind status colors: 1 hit in the login-page decorative background.
  - Manual `animate-pulse`: 2 hits, both inside shared primitives (`Skeleton` and `status-dot`).
  - Raw page-local callout divs: migrated on the touched management surfaces.
- Common duplicated patterns include page shells, page headers, profile-scope warnings, unavailable-state warnings, search/filter toolbars, status badges, metric cards, empty/loading states, and settings section cards.
- Older surfaces still mix `Label`/ad hoc form rows with newer `Field`/`FieldGroup` forms.
- Some page-local styles bypass semantic tokens, especially amber/emerald/red/sky status surfaces and pulse skeletons.

## Proposed Design-System Structure

- Keep `src/components/ui` as the checked-in shadcn primitive leaf.
- Use `src/shared/design-system` as the universal Prism operator layer for management-system patterns.
- Preserve `src/components/*` legacy names as compatibility wrappers only while migration is in progress.
- Keep route state, API calls, auth, profile selection, and business behavior out of the design-system layer.

Current operator component inventory:

- Foundation: `operatorTokenContract`, density modes, primitive inventory, guardrails.
- Layout: `OperatorPageShell`, `OperatorPageHeader`, `OperatorSectionCard`.
- Feedback/state: `OperatorCallout`, `OperatorEmptyState`, `OperatorLoadingState`, `OperatorErrorState`, `OperatorRetryButton`.
- Status/data markers: `OperatorStatusBadge`, `OperatorTypeBadge`, `OperatorValueBadge`.
- Metrics: `OperatorMetricCard`, `OperatorMetricTile`.
- Controls: `OperatorToolbar`, `OperatorSearchInput`, `OperatorSwitchField`, `OperatorIconButton`.
- Tables: `OperatorTableShell`, plus existing `shared/table/operationalTable` sorting/pagination helpers.
- Command: `OperatorCommandPalette`.

## Migration Order

1. Foundation and wrappers:
   - Done for this checkpoint: tokens and operator components are centralized in `src/shared/design-system`.
   - Done for this checkpoint: `PageHeader`, `EmptyState`, `SemanticCallout`, `StatusBadge`, `MetricCard`, `CompactMetricTile`, and `SwitchController` delegate to operator components.
2. Low-risk repeated surfaces:
   - Done for this checkpoint: active uses of the deprecated wrappers were migrated to operator components.
   - Done for this checkpoint: profile-scope callouts, unavailable warnings, request-log states, dashboard states, metric cards, status badges, and selected settings callouts use operator components.
   - Done for this checkpoint: page-local pulse blocks touched in pricing, load-balance, and retention surfaces use `Skeleton`.
3. Management pages:
   - Move feature pages to `OperatorPageShell`.
   - Move search/filter rows to `OperatorToolbar` and `OperatorSearchInput`.
   - Move section cards to `OperatorSectionCard`.
4. Tables and data screens:
   - Prefer `OperatorTableShell` and `OperationalTable*` helpers for headers, empty rows, skeleton rows, sorting, and pagination.
   - Keep domain-specific table cell renderers colocated with their feature.
5. Forms/dialogs:
   - Prefer `FieldGroup`, `Field`, `FieldLabel`, `FieldDescription`, and `FieldError`.
   - Keep React Hook Form integrations where already used.
   - Group `SelectItem`s with `SelectGroup` when touching selects.
6. Cleanup:
   - Next: remove deprecated wrappers after downstream imports stay at zero for a release-sized checkpoint, or keep them as compatibility shims if external/local code may still import them.
   - Next: reduce remaining `space-x/space-y` usage and move repeated section cards/tables into richer operator primitives.
   - Re-run the audit counts and tighten guardrails after each migration phase.

## Risks

- Visual drift is the main risk. The plan allows visual harmonization but must not change routes, API contracts, auth behavior, data models, persistence, test IDs, or workflow semantics.
- Some existing tests may rely on exact text or accessible structure. Wrapper migrations should preserve labels, roles, disabled states, and `data-testid`s.
- Full e2e coverage is broad. Run focused specs after each affected surface, then the full suite once the component migration is stable.

## Validation Commands

Run from `frontend/`:

```bash
pnpm install --frozen-lockfile
pnpm exec tsc -b --pretty false
pnpm run lint
pnpm run test
pnpm run test:lib
pnpm run test:server
pnpm run build
pnpm run test:e2e -- tests/e2e/protected-shell-sidebar.spec.ts tests/e2e/task-17-ui-semantics.spec.ts
pnpm run test:e2e
```

If a command fails, fix and rerun it. If failure is pre-existing or environmental, document the exact command, output, and reason.

## Deprecated Components And Replacements

- `src/components/PageHeader.tsx` -> `OperatorPageHeader`.
- `src/components/EmptyState.tsx` -> `OperatorEmptyState`.
- `src/components/SemanticCallout.tsx` -> `OperatorCallout`.
- `src/components/StatusBadge.tsx` -> `OperatorStatusBadge`, `OperatorTypeBadge`, `OperatorValueBadge`.
- `src/components/MetricCard.tsx` -> `OperatorMetricCard`.
- `src/components/CompactMetricTile.tsx` -> `OperatorMetricTile`.
- `src/components/SwitchController.tsx` -> `OperatorSwitchField`.
- Page-local raw callout divs -> `OperatorCallout`.
- Page-local search/filter bars -> `OperatorToolbar` plus `OperatorSearchInput`.
- Manual pulse blocks -> `Skeleton` or `OperatorLoadingState`.
