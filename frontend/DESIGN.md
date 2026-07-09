# Prism Design System

Prism uses a two-layer UI system:

- `src/components/ui`: shadcn/ui primitives checked into the repo. Keep this folder primitive-only.
- `src/shared/design-system`: Prism operator components for reusable management UI patterns.

New feature and page code should use `@/shared/design-system` first, then `@/components/ui` for primitive composition.

## Visual Direction

The management UI follows a Google Admin Console / Material Design 3 inspired direction:

- Light-first admin surfaces with calm neutral backgrounds.
- Blue primary actions and selected navigation.
- Surface roles instead of page-local color blends.
- Subtle outline dividers and low elevation.
- Compact but readable density for repeated operational work.
- No decorative gradients, blur blobs, heavy shadows, or marketing-style hero layouts.

## Tokens

Tokens live in `src/index.css` and are described by `operatorTokenContract` in `src/shared/design-system/foundation.ts`.

- Colors: use semantic tokens such as `background`, `foreground`, `surface`, `surface-container-low`, `surface-container`, `surface-container-high`, `primary`, `on-primary`, `primary-container`, `on-primary-container`, `outline`, `outline-variant`, `destructive`, `success`, `warning`, `info`, `healthy`, `downgrade`, and `unhealthy`.
- Spacing and density: use `--density-page-gap`, `--density-card-gap`, `--density-card-pad-x`, `--density-control-h`, and table density variables.
- Typography: use the configured operator sans/mono families. Use tabular numbers for metrics, ids, and numeric table cells.
- Radius and shadows: use shadcn defaults and operator surface classes. Avoid one-off large radii unless the component defines them.
- Motion: motion must not gate functionality. Respect reduced-motion rules in `src/index.css`.

Use these surface classes before creating local card styling:

- `operator-section-surface`: standard page sections, filters, forms, detail cards.
- `operator-table-shell`: card-framed tables and virtualized table containers.
- `operator-state-surface`: loading, empty, and error-state panels.

Avoid raw Tailwind status colors like `bg-amber-500/10`, page-local pulse skeletons, `bg-card/95`, `bg-muted/20`, and ad hoc dark-mode color overrides.

## Layout Rules

- Use `OperatorPageShell` for protected management pages.
- Use `OperatorPageHeader` for title, description, and actions.
- Use `OperatorSectionCard` for repeated section panels.
- Use `OperatorInsetPanel` for nested form groups, dialog summaries, drawer sections, metadata rows, review panels, and other contained sub-sections inside an existing page section.
- Keep route state and API calls in pages/features, not in design-system components.
- Use `gap-*` or density variables instead of `space-x-*` and `space-y-*`.
- Keep page content inside `OperatorPageShell` unless the route is a public auth page.
- Keep app shell, navigation, breadcrumbs, and Default-profile/scope chrome in layout components, not page code.

Example:

```tsx
<OperatorPageShell>
  <OperatorPageHeader title={copy.title} description={copy.description}>
    <Button>{copy.create}</Button>
  </OperatorPageHeader>
  <OperatorSectionCard title={copy.sectionTitle}>
    {children}
  </OperatorSectionCard>
</OperatorPageShell>
```

## Forms

- Prefer `FieldGroup`, `Field`, `FieldLabel`, `FieldDescription`, and `FieldError`.
- Settings toggles should use `OperatorSwitchField`.
- Keep React Hook Form where it already owns validation and submission.
- When touching selects, place `SelectItem`s inside `SelectGroup`.
- Preserve existing labels, disabled states, `aria-invalid`, and server validation messages.
- Group dense settings forms into section cards rather than nested local panels.
- When a form needs a smaller visual group inside a section card or dialog, use `OperatorInsetPanel` instead of local `rounded-lg border bg-muted/*` containers.
- Keep primary form actions in a predictable footer or page-header action area.
- Use `Spinner` inside buttons for submit-in-progress states; use `OperatorLoadingState` for panel-level loading.

## Tables

- Use `OperatorTableShell` for card-framed management tables.
- Use `shared/table/operationalTable` helpers for client sorting, pagination, skeleton rows, and sortable headers.
- Empty table states should use `OperatorEmptyState`.
- Keep domain-specific columns and cell renderers colocated with the feature.
- Horizontally scrollable tables must keep empty/loading states aligned to the visible viewport, not the full scroll width.
- Table headers and footers should use `surface-container-*` and `outline-variant` roles.
- Row actions should use icon buttons with accessible names when the action is already clear from the context.

## Dialogs And Drawers

- Keep dialog and sheet behavior, focus management, and close behavior on the existing Radix/shadcn primitives.
- Use consistent title, description, body spacing, and footer action placement.
- Use destructive button styling only for irreversible destructive actions.
- Use `OperatorCallout` for warnings or server validation summaries.
- Prefer `OperatorInsetPanel`, `OperatorCallout`, and shared form/section components inside dialogs and sheets rather than page-local nested cards.

## Status And Feedback

- Use `OperatorStatusBadge` for boolean/runtime state.
- Use `OperatorTypeBadge` for categories and classifications.
- Use `OperatorValueBadge` for raw values such as HTTP codes, methods, priorities, and percentages.
- Use `OperatorCallout` for warnings, information, success, destructive, and muted notices.
- Use `OperatorEmptyState`, `OperatorLoadingState`, and `OperatorErrorState` for common state surfaces.
- Use `toast()` from `sonner` for notifications.
- Loading surfaces should have `role="status"` and polite live-region behavior.
- Error surfaces should be visually contained and use `role="alert"` where appropriate.
- Empty states should explain what happened and provide the next useful action when one exists.

Product code should import operator components from `@/shared/design-system` directly instead of adding compatibility wrappers under `@/components`.
