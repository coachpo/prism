# Prism Design System

Prism uses a two-layer UI system:

- `src/components/ui`: shadcn/ui primitives checked into the repo. Keep this folder primitive-only.
- `src/shared/design-system`: Prism operator components for reusable management UI patterns.

New feature and page code should use `@/shared/design-system` first, then `@/components/ui` for primitive composition.

## Tokens

Tokens live in `src/index.css` and are described by `operatorTokenContract` in `src/shared/design-system/foundation.ts`.

- Colors: use semantic tokens such as `background`, `foreground`, `card`, `primary`, `muted`, `destructive`, `success`, `warning`, `info`, `healthy`, `downgrade`, and `unhealthy`.
- Spacing and density: use `--density-page-gap`, `--density-card-gap`, `--density-card-pad-x`, `--density-control-h`, and table density variables.
- Typography: use the configured operator sans/mono families. Use tabular numbers for metrics, ids, and numeric table cells.
- Radius and shadows: use shadcn defaults and operator surface classes. Avoid one-off large radii unless the component defines them.
- Motion: motion must not gate functionality. Respect reduced-motion rules in `src/index.css`.

Avoid raw Tailwind status colors like `bg-amber-500/10`, page-local pulse skeletons, and ad hoc dark-mode color overrides.

## Layout Rules

- Use `OperatorPageShell` for protected management pages.
- Use `OperatorPageHeader` for title, description, and actions.
- Use `OperatorSectionCard` for repeated section panels.
- Keep route state and API calls in pages/features, not in design-system components.
- Use `gap-*` or density variables instead of `space-x-*` and `space-y-*`.

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

## Tables

- Use `OperatorTableShell` for card-framed management tables.
- Use `shared/table/operationalTable` helpers for client sorting, pagination, skeleton rows, and sortable headers.
- Empty table states should use `OperatorEmptyState`.
- Keep domain-specific columns and cell renderers colocated with the feature.

## Status And Feedback

- Use `OperatorStatusBadge` for boolean/runtime state.
- Use `OperatorTypeBadge` for categories and classifications.
- Use `OperatorValueBadge` for raw values such as HTTP codes, methods, priorities, and percentages.
- Use `OperatorCallout` for warnings, information, success, destructive, and muted notices.
- Use `OperatorEmptyState`, `OperatorLoadingState`, and `OperatorErrorState` for common state surfaces.
- Use `toast()` from `sonner` for notifications.

## Deprecated Compatibility Wrappers

The following old components are retained as compatibility shims, but product code should import the operator replacements directly. The current migration checkpoint has zero active product imports/usages of these wrappers:

- `@/components/PageHeader`
- `@/components/EmptyState`
- `@/components/SemanticCallout`
- `@/components/StatusBadge`
- `@/components/MetricCard`
- `@/components/CompactMetricTile`
- `@/components/SwitchController`

Use the operator components from `@/shared/design-system` instead.
