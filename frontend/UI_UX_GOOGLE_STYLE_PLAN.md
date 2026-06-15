# Google-Style UI Renovation Plan

Generated: 2026-06-15

This plan renovates Prism's management dashboard toward a Google Admin Console / Material Design 3 inspired product UI. It preserves behavior, routes, API contracts, auth, permissions, data access, and workflows.

## Current Audit

The app already has a good refactor foundation: shadcn primitives in `src/components/ui`, operator components in `src/shared/design-system`, route-specific state kept in pages/features, and compatibility wrappers for old shared components.

Remaining inconsistencies found in the audit:

- Shell surfaces mixed a light page body with older card/sidebar styling, so hierarchy depended on local borders and shadows instead of surface roles.
- Auth pages used a separate decorative shell with gradients, blur, and large shadows rather than the management product language.
- Request logs used a virtualized table whose empty state centered against total scroll width, drifting out of the visible viewport on narrow screens.
- Filter bars, table footers, metrics, cards, and detail headers still used local `bg-card`, `bg-muted`, `border-border/70`, and one-off shadows.
- The latest checkpoint removed page-level `bg-muted/*`, `bg-card`, `border-border/*`, `rounded-2xl`, `space-y-*`, and old icon-spacing patterns from the audited product surfaces. Remaining matches are limited to primitive defaults where no-elevation or tooltip treatment is intentional.
- Dense settings/startup, request-log detail, statistics, dashboard routing, model, endpoint, pricing, load-balance, and proxy-key surfaces now use shared section, inset, table, callout, loading, empty, and badge patterns.

## Target Direction

Use a calm Material Design 3 inspired admin style:

- Light neutral app body with white or near-white section surfaces.
- Blue primary action language and selected navigation.
- Subtle outline variants and minimal elevation.
- Small-to-medium radii that feel practical, not playful.
- Scannable dense tables and forms with predictable control height.
- Shared loading, empty, and error states with accessible roles.
- Consistent page header, filter bar, table shell, card, dialog, and drawer structure.

Do not copy Google brand assets, logos, product names, or proprietary layouts. Use only the general design principles.

## Token Renovation

Completed in this pass:

- Added Material-style color roles in `src/index.css` and `operatorTokenContract`: `on-primary`, `primary-container`, `on-primary-container`, `tertiary`, `surface`, `surface-container-low`, `surface-container`, `surface-container-high`, `outline`, `outline-variant`, `disabled`, and `focus-ring`.
- Replaced the darker "operator control room" tone with `google-admin-material` in `src/shared/design-system/foundation.ts`.
- Reworked light and dark theme roles for Google-like neutral surfaces, blue primary, semantic status colors, and subtle elevation.
- Added reusable surface classes: `operator-section-surface`, `operator-state-surface`, and `operator-table-shell`.
- Standardized `Card`, `Table`, `Input`, outline `Button`, shell header, and sidebar primitives on the new surface and outline tokens.

Remaining token work:

- Add explicit typography helper classes for page title, section title, table header, helper text, and compact labels.
- Audit chart colors in statistics for contrast across light/dark themes.
- Keep the primitive-level no-elevation defaults intentional; the product-layer scan is clean of page-local `bg-muted/*`, `bg-card/*`, `border-border/*`, `rounded-2xl`, `space-y-*`, and old shadow patterns.

## Component Renovation

Completed in this pass:

- `AppShell` and top header: light surface, outline divider, accessible skip link.
- `Sidebar`: light inset surface, subtle elevation, Material selected-state language.
- `PageLayout`: density-token page padding and page gap.
- `Card`: defaults to `bg-surface` and `border-outline-variant`.
- `Button`: outline buttons now use surface/container hover roles.
- `Input`: uses surface fill and shared focus treatment.
- `DataTable` and shared table primitive: surface/outline roles, softer header and row hover states.
- `OperatorSectionCard`, `OperatorToolbar`, `OperatorTableShell`: surface roles and consistent elevation.
- `OperatorLoadingState` and `OperatorErrorState`: accessible `role="status"` and `role="alert"`.
- `OperatorMetricCard`, `OperatorMetricTile`, and statistics KPI card: Material containers and primary-container icon chips.
- Auth forms: consolidated into `AuthPageShell` with shared field primitives.
- `OperatorInsetPanel`: added as the standard sub-section surface for settings groups, dialogs, drawers, review panels, metadata rows, and dense form subsections.
- Dialog and sheet primitives: aligned to `surface`, `outline-variant`, and operator panel elevation.

Next component work:

- Add `OperatorFilterBar` for dense filter panels used by request logs, statistics, and settings.
- Add `OperatorDialogLayout` and `OperatorDrawerSection` to align dialogs, sheets, and detail drawers.
- Decide whether `OperatorInsetPanel` is sufficient as the shared form-section primitive or whether a typed `OperatorFormSection` wrapper should be added later.
- Move more table footers and pagination variants to `shared/table/operationalTable`.
- Replace page-local skeleton panels with shared loading states where skeleton fidelity is not needed.

## Page-By-Page Renovation

### Global Shell And Navigation

Status: first pass complete.

- Keep the inset shell but use light `surface`/`surface-container-low` roles.
- Make selected navigation calm and readable with primary-container semantics.
- Keep the selected profile and active runtime profile distinction unchanged.
- Preserve breadcrumbs, route metadata, and mobile sidebar behavior.
- Next: verify collapsed/mobile sidebar in browser after backend-authenticated data is available.

### Auth: `/auth/login`, `/auth/forgot-password`, `/auth/reset-password`

Status: first pass complete.

- Replaced three separate auth shells with `AuthPageShell`.
- Removed gradients, blur blobs, oversized shadows, and local card/control styling.
- Standardized form labels, field spacing, loading spinners, and top utility controls.
- Preserved existing redirects, submit handlers, toasts, and navigation targets.
- Next: visual QA in an auth-enabled backend session, because the frontend-only local run redirects auth pages when auth is disabled.

### Observe Dashboard: `/observe`

Status: first pass complete.

- Metric cards and compact tiles inherit Material surface roles.
- Empty states use the operator surface language.
- Overview cards, quick actions, recent activity, top spending, and tab controls use surface roles, gap spacing, and button-icon conventions.
- Routing diagram shell, mobile list, pending canvas, and node cards use outline/surface tokens without changing layout math or React Flow contracts.
- Next: tune chart tooltip tokens and responsive chart height with authenticated data.

### Dashboard Analytics: `/observe?tab=analytics`

Status: first pass complete.

- KPI and table cards now inherit better card defaults.
- Date/range controls, chart toggles, usage tables, model selector, breakdown panels, and overview KPI icons now use surface roles and button-icon conventions.
- Next: extract the controls into an `OperatorFilterBar` when a shared filter primitive is added.

### Dashboard Routing: `/observe?tab=routing`

Status: first pass complete.

- Keep React Flow ownership and CSS import unchanged.
- Aligned routing diagram shell, loading skeleton, empty state, toolbar, pending canvas, node cards, mobile relation groups, metric pills, and inspector spacing with surface/container roles.
- Next: browser-check desktop and mobile routing views against live backend payloads.

### Models List: `/models`

Status: first pass complete.

- Existing page already uses `OperatorPageShell`, `OperatorPageHeader`, and `ModelsTable`.
- Models table border tokens, detail loading state, model dialog sections, access-target editor, target empty/error states, and enable switch now use shared surfaces.
- Next: browser-check create/edit dialog flows with representative target data.

### Model Detail: `/models/:id`

Status: first pass complete.

- Header and overview cards now use Material section surfaces.
- Cost empty state now has a designed container.
- Next: renovate connection dialogs, detail tab sections, payload/code blocks, and ban-policy state reset surfaces with shared dialog/drawer and form-section components.

### Endpoints: `/route/endpoints`

Status: first pass complete.

- Page shell and toolbar already use operator components.
- Endpoint cards use section-surface roles, softened drag/overlay state, tokenized drag handles, and icon-button conventions.
- Create/edit endpoint dialog sections use shared inset panels.
- Next: browser-check drag overlay contrast and keyboard focus.

### Ban Policies: `/route/ban-policies`

Status: first pass complete.

- Replaced dense one-line local cards with `OperatorSectionCard`.
- Standardized current-state and events loading/empty states.
- Events table now uses surface/outline roles and designed pagination.
- Event detail sheet and Ban Policy strategy dialog now use inset sections and grouped selects.
- Next: browser-check Ban Policy create/edit and event detail sheet.

### Pricing: `/route/pricing`

Status: first pass complete.

- Existing table already uses `operator-table-shell` and `OperatorEmptyState`.
- Pricing table header spacing, row/action icons, create/edit sections, usage dialog, delete summary, dependency table, and error callouts now use shared surfaces.
- Next: browser-check conflict/delete dialog with usage rows.

### Request Logs: `/observe/requests`

Status: first pass complete.

- Page now uses `OperatorPageShell`.
- Filter bar uses a Material section surface.
- Virtualized table uses `operator-table-shell`, surface-container header/footer roles, and visible-viewport empty state anchoring.
- Error callout remains behavior-compatible and intentionally shows backend parse errors during frontend-only local runs.
- Detail sheet, overview cards, payload blocks, filters, dedicated audit cards, and audit detail headers now use surface/outline roles and flex gap spacing.
- Next: extract `OperatorFilterBar` and tune horizontal-scroll affordance.

### Request Log Audit: `/observe/requests/:id/audit` or mounted audit detail

Status: first pass complete.

- Existing page already uses `OperatorPageHeader` and badges.
- Local cards, loading cards, audit detail header, and body/diff blocks now use outline/surface roles.
- Next: browser-check dedicated audit page with body-capture and metadata-only states.

### Proxy Keys: `/control/proxy-keys`

Status: first pass complete.

- Existing page uses `OperatorPageHeader`.
- Auth enforcement, issue form, detail sheet switch, ledger, loading skeletons, and secret reveal dialog now use shared section/inset/callout surfaces.
- Next: browser-check create/rotate/delete flows with global auth settings loaded.

### Settings: `/system/settings`

Status: first pass complete.

- Settings section nav now uses the operator surface language.
- Authentication, billing/currency, retention/deletion, timezone, backup/import, audit configuration, profile dialogs, and settings tabs now use shared section/inset/callout/table surfaces.
- Next: consider extracting recurring settings groups into `OperatorFormSection` if duplication returns.

### Startup Settings

Status: first pass complete.

- Existing startup tab is dense and form-heavy.
- Startup group shells, file-status metadata, server/database/runtime/telemetry/mail/auth/secrets cards, disclosures, review panels, and dangerous confirmation surfaces now use shared section/inset/callout/table patterns.
- Next: browser-check review panel sticky behavior and long-form responsiveness.

### Error, Empty, Loading, Forbidden, Not Found

Status: partial.

- Route fallback, profile bootstrap loading, request-log empty, ban-policy empty/loading, and common operator states are improved.
- Next: find remaining plain text loading/error surfaces and replace with `OperatorLoadingState`, `OperatorErrorState`, or `OperatorCallout`.

## Migration Order

1. Foundation tokens and primitive defaults.
2. Global shell, sidebar, top header, page shell.
3. Auth shell and route fallback/profile loading states.
4. List/table pages: request logs, models, endpoints, pricing, proxy keys.
5. Detail pages: model detail and request-log detail sheets.
6. Settings/startup form sections.
7. Modal, drawer, and confirm-dialog consistency.
8. Remaining skeleton, empty, error, and not-found surfaces.
9. Responsive visual QA across desktop and mobile widths.

## Validation Commands

Run after each meaningful checkpoint:

```bash
pnpm run build
pnpm run lint
pnpm run test
pnpm run test:lib
pnpm run test:server
```

Run targeted browser checks:

```bash
pnpm run dev
# Visual QA:
# /observe
# /observe/requests
# /models
# /models/:id
# /route/endpoints
# /route/ban-policies
# /route/pricing
# /control/proxy-keys
# /system/settings
# /auth/login with auth enabled
```

Latest checkpoint on 2026-06-15:

- Passed `pnpm exec tsc -b`.
- Passed `pnpm run lint`.
- Passed `pnpm run build` after the final primitive-token cleanup.
- Passed earlier full checkpoint suites: `pnpm run test`, `pnpm run test:lib`, `pnpm run test:server`, and `pnpm run test:config`.
- Passed `git diff --check`.
- Product-layer token scan only reports intentional primitive-level no-elevation defaults and muted status coloring.
- Visual smoke screenshots saved under `output/playwright/`: `observe.png`, `models.png`, `pricing.png`, `settings-startup.png`, `proxy-keys.png`, `observe-requests.png`, `observe-analytics.png`, and `observe-routing.png`.
- Frontend-only smoke runs intentionally show backend bootstrap/API JSON parse errors or loading shells where a live backend is required; those errors stayed visually contained.

## Visual QA Checklist

- Page title, description, breadcrumbs, and primary action alignment are consistent.
- Sidebar selected state is visible in light and dark themes.
- Cards use `surface`/`surface-container-*` roles and subtle elevation.
- Tables have readable headers, row hover states, pagination, loading, empty, and error states.
- Empty states stay inside the visible viewport for horizontally scrollable tables.
- Filter bars wrap without overlap at tablet and mobile widths.
- Forms have consistent label, helper text, validation, disabled, and loading states.
- Dialogs and sheets have consistent title, description, body spacing, footer actions, and destructive emphasis.
- Icon-only buttons have accessible names and visible focus.
- No text overlaps controls or adjacent content.
- No decorative gradients, blur blobs, or unrelated imagery are introduced.
- Frontend-only API errors are visually contained and do not break layout.

## Risks And Non-Goals

- Do not change route paths, redirects, auth state, API calls, query keys, cache invalidation, permissions, or workflow logic.
- Do not remove compatibility wrappers until a separate cleanup task confirms they are unused in product and tests.
- Do not introduce a new UI library, CSS framework, icon pack, or routing framework.
- Do not over-animate admin workflows; motion must stay subtle and respect reduced-motion.
- Do not hide technical backend errors unless product requirements explicitly ask for rewritten copy.
- Dark mode is supported by tokens, but the target product direction is light-first admin UI.
