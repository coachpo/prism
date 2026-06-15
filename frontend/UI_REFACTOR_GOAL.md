Refactor this frontend management Web UI into a consistent design-language-based project without changing business behavior, routes, API contracts, auth logic, data models, or user workflows.

Objective:
Unify the application around one reusable component system, one styling approach, one layout system, one tokenized visual language, and one documented set of common UI patterns. Remove unnecessary design, layout, color, spacing, typography, and component diversity caused by phased development by different people.

Work process:

1. First audit the repository before editing:
   - Detect framework, build tools, package manager, routing, state management, CSS/styling systems, UI libraries, icon libraries, form libraries, table libraries, and test setup.
   - Identify duplicated or inconsistent components, pages, layouts, CSS files, utility classes, color usage, spacing usage, typography usage, table patterns, form patterns, modal/drawer patterns, buttons, filters, cards, tabs, badges, and page headers.
   - Create or update a file named UI_REFACTOR_PLAN.md summarizing:
     - current problems
     - proposed design-system structure
     - component inventory
     - migration order
     - risks
     - validation commands
     - files/components to deprecate

2. Establish a universal design foundation:
   - Create a centralized design token layer for color, typography, spacing, radius, shadows, borders, z-index, layout width, motion, and component states.
   - Prefer CSS variables/design tokens and existing project conventions.
   - Do not introduce a new UI library, styling framework, or major dependency unless the repo already uses it or there is a strong reason documented in UI_REFACTOR_PLAN.md.
   - Derive the final visual language from the existing product identity. Do not create multiple themes or decorative visual variants unless already required.
   - Ensure accessibility basics: focus states, keyboard support, semantic markup, readable contrast, labels, aria attributes where needed.

3. Build a reusable component system:
   Create or consolidate components under a clear location such as:
   - src/components/ui
   - src/components/common
   - src/design-system
     Use the project’s existing naming and folder conventions where possible.

   The reusable component set should include, when applicable:
   - AppShell / PageLayout
   - Sidebar / TopNav / Header
   - PageHeader
   - Card / Section
   - Button
   - IconButton
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
   - Pagination
   - Tabs
   - Modal
   - Drawer
   - ConfirmDialog
   - Dropdown/Menu
   - Badge / StatusBadge
   - Alert
   - Toast / Notification
   - EmptyState
   - LoadingState / Spinner / Skeleton
   - ErrorState
   - Breadcrumbs
   - Date/Time display wrappers
   - Upload component if the app has upload flows

4. Refactor incrementally:
   - Start with low-risk shared primitives and layout components.
   - Then migrate repeated page-level patterns.
   - Then migrate complex pages such as tables, forms, filters, and modal-heavy flows.
   - Replace duplicated components with the universal components.
   - Remove dead styles/components only after confirming they are unused.
   - Preserve all existing functionality and page behavior.
   - Keep changes scoped and reversible.
   - Do not rewrite business logic unless necessary to connect the new components.
   - Do not rename public routes, API fields, environment variables, permissions, or translation keys unless strictly necessary and documented.

5. Styling rules:
   - Use one styling strategy consistently.
   - Eliminate hard-coded colors, random margins, one-off typography, and page-specific visual hacks where possible.
   - Replace them with tokens, shared classes, or component props.
   - Standardize spacing, border radius, shadows, table density, form layout, page width, headers, empty states, error states, loading states, and action button placement.
   - Keep the UI visually simple and management-system appropriate.
   - Avoid unnecessary animation or decorative styling.

6. Documentation:
   Add or update documentation so future developers know how to use the system:
   - DESIGN_SYSTEM.md or docs/design-system.md
   - Component inventory
   - Component usage examples
   - Design tokens
   - Layout rules
   - Form rules
   - Table rules
   - Status/empty/loading/error state rules
   - Deprecated components/styles and replacements

7. Validation:
   After each meaningful checkpoint, run the appropriate available commands discovered from package.json or project config, such as:
   - install/check dependency consistency if needed
   - typecheck
   - lint
   - unit tests
   - component tests
   - build
   - format check
   - storybook build if available
   - e2e tests if available and reasonably configured

   If a command fails, inspect the failure, fix it, and rerun. Do not ignore failing checks unless the failure is clearly pre-existing; if pre-existing, document it.

8. Completion condition:
   Continue until:
   - The project has a clear universal design-system/component structure.
   - The most common duplicated UI patterns have been migrated to shared components.
   - Styling is centralized through tokens/shared styles instead of scattered hard-coded values.
   - Major pages use consistent layout, typography, spacing, buttons, forms, tables, modals, loading, empty, and error states.
   - Duplicate or obsolete components/styles are removed or clearly marked deprecated.
   - Documentation exists for future development.
   - The app builds successfully.
   - Typecheck/lint/tests pass, or remaining failures are documented as pre-existing or blocked with exact reasons.

9. Final report:
   At the end, provide:
   - summary of changes
   - new component/design-system structure
   - migrated pages/components
   - removed/deprecated files
   - validation commands run and results
   - known risks or remaining work
   - recommended next steps for visual QA and developer adoption

Important constraints:

- Do not change product behavior.
- Do not change API contracts.
- Do not change routing.
- Do not change auth/permission behavior.
- Do not introduce unnecessary dependencies.
- Do not do a full rewrite.
- Prefer systematic refactoring over cosmetic one-off edits.
- Make reasonable decisions when information is missing and document those decisions.
