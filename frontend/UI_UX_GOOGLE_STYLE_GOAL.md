# UI/UX Renovation Goal: Google Style / Material Design 3 Management Web UI

Use the ui-ux-pro-max skill to renovate the entire management Web UI into a polished, consistent, Google-style admin product experience inspired by Material Design 3 and Google Workspace.

This task starts from an already refactored foundation: the project should already have a universal design language, shared tokens, common components, and reduced component/style duplication. Build on that foundation. Do not restart the design system from scratch unless the existing implementation is clearly broken, and document any major changes.

Primary objective:
Renovate the whole Web UI so it feels like one high-quality Google-style management application: clean, calm, spacious, accessible, professional, component-driven, predictable, and visually consistent across all pages.

Style direction:

- Material Design 3 inspired.
- Google Workspace / Google Admin Console style.
- Clean white or very light neutral surfaces.
- Subtle elevation, soft dividers, clear hierarchy.
- Rounded but not playful.
- Blue primary action language unless the existing brand requires a different primary color.
- Consistent use of semantic colors for success, warning, error, info, disabled, selected, hover, focus, and active states.
- Modern dashboard/admin layout, not decorative marketing UI.
- Avoid visual noise, random gradients, excessive shadows, heavy borders, and inconsistent colors.
- Do not copy Google logos, product names, icons, proprietary layouts, or copyrighted assets. Use the style principles only.

Hard constraints:

- Do not change product behavior.
- Do not change API contracts.
- Do not change routes.
- Do not change auth, permission, role, or data-access logic.
- Do not change business workflows.
- Do not remove features.
- Do not introduce unnecessary dependencies.
- Do not replace the current app framework.
- Do not do a full rewrite.
- Prefer improving the existing shared design-system foundation.
- If a visual or structural change might affect behavior, choose the safer option and document the tradeoff.

Before editing:

1. Read the existing design-system/refactor documents, if present:
   - UI_REFACTOR_PLAN.md
   - DESIGN_SYSTEM.md
   - docs/design-system.md
   - UI_REFACTOR_GOAL.md
   - any Storybook/component docs
   - package.json
   - routing/layout files
   - shared component directories

2. Audit the current UI implementation:
   - Identify pages/components that still look inconsistent.
   - Identify places where the foundation exists but is not fully applied.
   - Identify page layouts, tables, forms, filters, dialogs, drawers, navigation, cards, dashboards, and detail pages needing renovation.
   - Identify hard-coded color, spacing, typography, radius, elevation, and layout values that violate the shared system.
   - Identify UX problems: unclear hierarchy, cramped layouts, weak empty states, inconsistent actions, poor loading states, weak error states, confusing filter placement, inconsistent table actions, unclear form grouping, missing focus states, and inaccessible controls.

3. Create or update UI_UX_GOOGLE_STYLE_PLAN.md with:
   - current visual/UX problems
   - target Google-style direction
   - design-token changes needed
   - component changes needed
   - page groups to renovate
   - migration order
   - validation commands
   - visual QA checklist
   - risks and non-goals

Design-token renovation:
Improve the existing token system to support a Material Design 3 inspired admin UI:

- color roles:
  - primary
  - on-primary
  - primary-container
  - on-primary-container
  - secondary
  - tertiary if needed
  - surface
  - surface-container-low
  - surface-container
  - surface-container-high
  - background
  - outline
  - outline-variant
  - error
  - success
  - warning
  - info
  - disabled
  - focus-ring
- typography:
  - page title
  - section title
  - card title
  - body
  - body small
  - label
  - helper text
  - table header
  - table cell
  - button text
- spacing:
  - consistent 4px/8px-based scale
  - page padding
  - section spacing
  - card padding
  - form spacing
  - table density
  - toolbar spacing
- shape:
  - small, medium, large radius tokens
  - consistent radius assignment by component type
- elevation:
  - subtle surface/elevation levels
  - avoid heavy shadows
- motion:
  - short, subtle transitions for hover, focus, expand/collapse, drawer/modal entry
  - respect reduced motion if the project supports it

Component renovation:
Refine the shared components so they express the Google-style visual language consistently. Renovate these components where applicable:

- AppShell
- Sidebar
- TopNav
- PageLayout
- PageHeader
- Breadcrumbs
- Card
- Section
- Button
- IconButton
- ButtonGroup
- Input
- Textarea
- Select
- Checkbox
- Radio
- Switch
- FormField
- SearchBar
- FilterBar
- Toolbar
- DataTable
- TableToolbar
- Pagination
- Tabs
- Modal
- Drawer
- ConfirmDialog
- Dropdown/Menu
- Badge
- StatusBadge
- Alert
- Toast/Notification
- EmptyState
- LoadingState
- Skeleton
- ErrorState
- Tooltip
- Date/time display
- File upload component if present

Expected component behavior:

- Buttons have clear hierarchy: primary, secondary, tertiary/text, destructive, icon-only.
- Forms have consistent labels, helper text, validation text, spacing, required state, disabled state, and error state.
- Tables have consistent header style, row height, hover state, selected state, empty state, loading state, pagination, sorting, filters, row actions, and bulk actions.
- Cards and sections use consistent padding, border/elevation, title placement, and action placement.
- Dialogs and drawers have consistent title, description, content spacing, footer actions, close behavior, and destructive confirmation patterns.
- Page headers have consistent title, subtitle, breadcrumbs, metadata, and primary/secondary actions.
- Navigation clearly shows selected state, hover state, collapsed behavior if present, and responsive behavior.
- Loading, empty, and error states should feel intentionally designed, not accidental.

Page-level renovation:
Renovate the entire app page by page. Prioritize:

1. Global shell and navigation.
2. Dashboard/home/overview pages.
3. List/table/index pages.
4. Create/edit/detail forms.
5. Detail pages.
6. Settings/admin pages.
7. Modal/drawer-heavy workflows.
8. Error, empty, loading, forbidden, and not-found pages.
9. Any legacy pages that still use old local styles.

For each page:

- Apply the shared AppShell/PageLayout/PageHeader.
- Replace local visual hacks with shared components and tokens.
- Standardize primary action placement.
- Standardize filter/search/table patterns.
- Standardize form grouping and validation.
- Improve hierarchy and whitespace.
- Improve empty/loading/error states.
- Keep behavior unchanged.
- Prefer small safe commits/checkpoints.

UX quality requirements:

- The UI should look like a coherent Google-style management product.
- Every page should have clear information hierarchy.
- Primary actions should be obvious but not visually loud.
- Secondary actions should be discoverable but calm.
- Tables should be scannable.
- Forms should be easy to complete.
- Filters should be predictable and reusable.
- Destructive actions should be clearly distinguished and protected.
- Empty states should explain what happened and what to do next.
- Error states should be useful and non-technical where possible.
- Loading states should avoid layout jump where possible.
- Responsive behavior should remain acceptable for the app’s supported breakpoints.
- Keyboard navigation and visible focus states should be preserved or improved.

Accessibility requirements:

- Maintain semantic HTML.
- Preserve or improve labels, aria attributes, focus handling, keyboard support, contrast, and visible focus rings.
- Do not hide focus outlines without replacing them with accessible focus styling.
- Ensure icon-only buttons have accessible names.
- Ensure dialogs/drawers preserve focus management if the project supports it.
- Avoid using color alone to communicate status.

Visual QA:
If the project has Storybook, Playwright, Cypress, screenshots, visual tests, or a local dev server:

- Use them to inspect renovated components and representative pages.
- Add or update component stories if Storybook exists.
- Add or update visual examples for shared components where appropriate.
- If screenshot or visual regression tooling exists, use it.
- If no visual QA tooling exists, document manual QA steps in UI_UX_GOOGLE_STYLE_PLAN.md.

Validation loop:
After each meaningful checkpoint, run the relevant available commands from package.json or project config:

- typecheck
- lint
- tests
- build
- format check
- Storybook build if available
- e2e tests if available and reasonably configured

If a command fails:

- Investigate and fix failures caused by this work.
- If a failure is clearly pre-existing, document it with evidence.
- Do not ignore failures silently.

Documentation:
Update or create design-system documentation:

- DESIGN_SYSTEM.md or docs/design-system.md
- Add a “Google-style / Material-inspired UI direction” section.
- Document tokens.
- Document component variants.
- Document layout rules.
- Document table rules.
- Document form rules.
- Document dialog/drawer rules.
- Document empty/loading/error states.
- Document deprecated visual patterns and replacements.
- Add examples showing how future developers should build pages using the shared components.

Completion condition:
Continue until:

- The entire Web UI uses the renovated Google-style design language.
- Core layout, navigation, tables, forms, cards, dialogs, drawers, filters, status badges, empty states, loading states, and error states are visually consistent.
- Hard-coded one-off styling is removed or minimized.
- Shared components are reused instead of duplicated local implementations.
- Major legacy visual inconsistencies are removed.
- Documentation is updated.
- Build succeeds.
- Typecheck/lint/tests pass, or remaining failures are documented as pre-existing or blocked with exact reasons.
- UI_UX_GOOGLE_STYLE_PLAN.md contains a final summary of renovated areas, remaining risks, and visual QA recommendations.

Final response:
When finished, provide:

- high-level summary
- design-token changes
- component changes
- renovated pages/areas
- files removed or deprecated
- validation commands run and results
- known risks
- recommended manual visual QA checklist
