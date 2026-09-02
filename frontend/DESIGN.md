# Prism Design System

Prism uses a two-layer UI system:

- `src/components/ui`: shadcn/ui primitives checked into the repo. Keep this folder primitive-only.
- `src/shared/design-system`: Prism operator components for reusable management UI patterns.

New feature and page code should use `@/shared/design-system` first, then `@/components/ui` for primitive composition.

## Product Character

Prism is a self-hosted LLM gateway operated by one person. The console is a cockpit, not a marketing surface. Operators arrive with one of three tasks: is it healthy right now, why did this request fail, how should this route be configured.

Four rules, in priority order:

1. **Honesty over tidiness.** See Honesty Contract. This outranks every aesthetic consideration.
2. **Density over whitespace.** One screen must hold KPIs, a time series, and a long table. Breathing room never costs screen information.
3. **State must be scannable.** Anomalies are always more prominent than normal, and are never conveyed by color alone.
4. **Numbers are the subject.** Metrics, identifiers, and timestamps use the mono family with tabular numerals so columns compare vertically.

## Honesty Contract

The backend models uncertainty as first-class data: retention coverage completeness, pricing trust, best-effort event ledgers, payload truncation. Shipped copy already promises `结果不会用零值填补已删除历史` and `覆盖不完整，不能确认没有事件`.

The UI must not contradict that. These four situations must look different from each other:

| Situation | Rendering |
| --- | --- |
| Genuinely zero | `0`, normal numeric styling |
| Value absent | `—` (em dash) in `text-muted`. Never `0`, never blank |
| Read failed | Replace the whole block with `OperatorErrorState` plus a retry action. **Never degrade to an empty state** |
| Clipped or truncated | Render normally plus an explicit badge (`保留期外`, `载荷已截断`, `覆盖不完整`) |

Supporting rules:

- **`?? 0` in a render path is a defect.** Absent goes to `—`; failed goes to an error surface.
- **Stale data is labelled stale, not repainted as fresh.** On a failed refresh keep the last successful data and attach the staleness badge carrying the last success time and the failure reason. Exactly one staleness badge design exists site-wide.
- **Every data block answers "when is this from".** Pages with a time window carry a freshness bar. A refresh control must actually refresh.
- **Basis must be labelled.** When one column is a window total and another is the last bucket, say so in the column header. When a block uses a different time window than the page, say so on the block.
- **Never leak enum keys.** `priced`, `unpriced`, `healthy`, `stream_outcome` and friends pass through a Chinese label dictionary before display. Dotted registry identifiers such as `anthropic.messages` are enum keys too: brand nouns like Anthropic and Gemini stay untranslated, the identifier around them does not reach the screen. A label dictionary needs a named fallback for the key it has not learned yet, never the key itself.

### Observability Attribution

- `入口请求` (`ingress`) counts one finalized ingress once and attributes it to the requested entry model.
- `最终承载` (`final_execution`) counts one finalized request once and attributes it to the actual final target model and winning Terminal Target.
- `路由尝试` (`route_attempt`) counts each actual upstream attempt, including failed retries. It does not aggregate or infer provider cost; an unknown failed-attempt charge renders as `—` with an explanation.
- Parent request cost and child attempt cost are composition, not two totals to add. Request-chain surfaces label the parent and child values explicitly and keep routing completeness separate from pricing/cost evidence.
- The models-list scope switch is a controlled single-select segmented control (never tabs): it shows the three scope labels plus a fixed attribution-basis note for the active scope, keeps the selection URL-backed, and re-selecting the active scope never clears it.

## Visual Direction

The product name supplies the concept. A prism splits one incident beam into a spectrum — exactly what the gateway does: one entry, many exits.

- **The incident beam is the primary blue.** Every "entry" semantic shares one blue: primary actions, active navigation, links, focus rings. There is one blue, not a light and a dark one.
- **The spectrum is the categorical palette.** Providers, models, and terminal targets — the "many exits" — are separated by a controlled six-hue spectrum.
- **Separation is contextual, not chromatic.** Spectrum colors appear only in data-encoding contexts (chart series, legends, group tags). Status colors appear only in status contexts (status badges, table row stripes). The two palettes may share hues — spectrum 1 is the primary and spectrum 3 is the healthy green — because context already separates them and status always carries a shape and a label as well. Forcing the spectrum to dodge green, amber, and red would crowd all six series into the blue-violet range and make them harder to tell apart.
- Light-first admin surfaces, both themes fully specified.
- Layering comes from 1px outlines, not shadows. Shadows are reserved for genuinely floating overlays.
- No decorative gradients, blur blobs, heavy shadows, or marketing-style hero layouts.

## Tokens

Tokens live in `src/index.css` and are described by `operatorTokenContract` in `src/shared/design-system/foundation.ts`. Both must be migrated to the set below.

### Primary

| Role | Light | Dark |
| --- | --- | --- |
| `primary` | `#1E4FD8` | `#9FBEFF` |
| `on-primary` | `#FFFFFF` | `#04266E` |
| `primary-soft` | `#E4EBFF` | `#1B2E63` |
| `on-primary-soft` | `#123A9E` | `#CFDDFF` |

### Surfaces — three tiers, not four

The current `surface-container-low / container / container-high` ladder is too low-contrast to read as separate surfaces, so card boundaries have to be guessed. Collapse to three tiers and express layering with a 1px outline. Dark mode makes shadows nearly invisible, so shadow can never be the only layering device.

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| `canvas` | `#F6F7F9` | `#0F1319` | Page ground |
| `panel` | `#FFFFFF` | `#161B22` | Cards, tables, sidebar |
| `raised` | `#FFFFFF` | `#1C232C` | Dialogs, popovers, drawers, tooltips |
| `inset` | `#F0F2F5` | `#11161D` | Nested blocks, code blocks, table headers |
| `border` | `#DDE1E7` | `#2A323D` | Default outline |
| `border-strong` | `#C3C9D2` | `#3A4552` | Inputs, emphasized dividers |

### Text — two informative tiers only

A third grey is exactly the grey noise this system removes, and it necessarily falls below 4.5:1. The third grey survives only for disabled controls and decorative glyphs, and never carries information.

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| `text` | `#11161D` | `#E6E9EE` | Body, values, headings |
| `text-muted` | `#5A6472` | `#98A2B3` | Labels, table headers, help text, axes, breadcrumb non-leaf |
| `text-disabled` | `#8A93A1` | `#6E7885` | **Disabled controls and ≥24px decorative icons only** |

### Runtime status — four tiers

This replaces `success / healthy / warning / downgrade / info / unhealthy`. In that set `success` duplicated `healthy`, `warning` duplicated `downgrade`, and `unhealthy` was conflated with `destructive`. The four tiers below are mutually exclusive.

| Tier | Meaning | Light | Dark | Shape |
| --- | --- | --- | --- | --- |
| `healthy` | Serving normally | `#0F7B4F` | `#66D9A0` | ● |
| `degraded` | Retrying, partial failure, nearing a limit | `#A56200` | `#F5C063` | ◐ |
| `failing` | Banned, consecutive failures, unreachable | `#C0342B` | `#F4A9A3` | ▲ |
| `idle` | Idle, not enabled, no data | `#69717F` | `#8A93A1` | ○ |

**Status is always color plus shape plus text.** Badge geometry: 8px shape marker, 12px label, 20px tall, 4px radius, 6px horizontal padding; background at 10% of the status color, outline at 25%.

`destructive` shares the `failing` hue but not its meaning: it marks **irreversible operations only** (delete buttons, confirmation dialogs) and never describes runtime state.

### Spectrum — categorical palette

Chart series and provider/model group tags, taken in order. Beyond six series, fall back to neutral grey and merge the remainder into `其他` in the legend.

| # | Light | Dark |
| --- | --- | --- |
| 1 | `#1E4FD8` | `#9FBEFF` |
| 2 | `#0B7285` | `#5FD3E8` |
| 3 | `#0F7B4F` | `#66D9A0` |
| 4 | `#A56200` | `#F5C063` |
| 5 | `#B22D6E` | `#F58AB8` |
| 6 | `#6741D9` | `#B197FC` |

Separation is measured perceptually, not by hue angle: every adjacent pair clears CIE76 ΔE 30 and no pair in the set falls under ΔE 15, in both themes. Hue angle alone would misjudge pairs such as spectrum 1 and 2, which sit only 35° apart yet differ sharply in lightness and chroma. Color-blind separation is reinforced by alternating solid and dashed stroke styles on adjacent series.

### Measured contrast

Every value above was computed against its own `panel` using the WCAG 2.1 relative-luminance formula. Three draft values failed and were replaced: `degraded` from `#B26A00` (4.24), `idle` from `#7A8494` (3.78), and the third text grey from `#8A93A1` (3.10), which could not be darkened without collapsing into `text-muted` and was therefore demoted to decoration only.

| Token | Light | Dark |
| --- | --- | --- |
| `primary` | 6.63 | 9.29 |
| `text` | 18.16 | 14.21 |
| `text-muted` | 6.00 | 6.72 |
| `healthy` | 5.29 | 9.88 |
| `degraded` | 4.83 | 10.38 |
| `failing` | 5.57 | 9.10 |
| `idle` | 4.92 | 5.58 |
| Spectrum 1–6 | 4.83 – 6.63 | > 7 |

**Recompute this table when adding or changing any color. Never approve a color by eye.** The pre-redesign tokens shipped light-theme `warning` text at 1.81:1 and a focus ring at 2.34:1, which is how this rule earned its place.

### Other token rules

- Spacing and density: use `--density-page-gap`, `--density-card-gap`, `--density-card-pad-x`, `--density-control-h`, and the table density variables.
- Motion must not gate functionality. Respect the reduced-motion rules in `src/index.css`.

Use these surface classes before creating local card styling:

- `operator-section-surface`: standard page sections, filters, forms, detail cards.
- `operator-table-shell`: card-framed tables and virtualized table containers.
- `operator-state-surface`: loading, empty, and error-state panels.

Avoid raw Tailwind status colors like `bg-amber-500/10`, page-local pulse skeletons, `bg-card/95`, `bg-muted/20`, and ad hoc dark-mode color overrides.

## Typography

- **Sans**: Inter at 400 / 500 / 600. Nothing heavier.
- **Mono**: Geist Mono at 400 / 500.

Mono is **mandatory** for metrics, identifiers (model ids, endpoint hosts, request ids, key fingerprints), timestamps, currency, latency, and token counts. All numerals carry `font-variant-numeric: tabular-nums`. **Never set Chinese names or prose in mono** — mixed-script runs tear.

| Role | Size / line | Weight | Family |
| --- | --- | --- | --- |
| Page title | 22 / 28 | 600 | sans |
| Section title | 15 / 20 | 600 | sans |
| Card title | 13 / 18 | 600 | sans |
| Body | 13 / 20 | 400 | sans |
| Caption | 12 / 16 | 400 | sans, `text-muted` |
| Label | 11 / 14 | 500 | sans, `text-muted`, +0.04em |
| Table header | 12 / 16 | 500 | sans, `text-muted`, not uppercase |
| Table cell | 13 / 18 | 400 | sans for text, mono for values and identifiers |
| KPI value | 26 / 30 | 600 | mono |
| Code / payload | 12 / 18 | 400 | mono |

## Density And Scale

4px base grid. Radius drops from a 12px base to **8px** — large radii make adjacent cards read as loose in a dense console.

| Token | Value |
| --- | --- |
| Radius: badge / marker | 4px |
| Radius: control / input | 6px |
| Radius: card / table container | 8px |
| Radius: dialog / drawer | 12px |
| Shadow: overlay | `0 8px 24px -12px rgba(16,22,30,.28)`; dark `0 12px 32px -16px rgba(0,0,0,.6)` |

Two density modes, switchable from the header. `foundation.ts` already declares `compact / balanced / expanded` with no control wired up; expose it for real and keep two modes.

| Token | Compact | Standard (default) |
| --- | --- | --- |
| Page padding | 16px | 24px |
| Section gap | 16px | 20px |
| Card padding | 12px | 16px |
| Control height | 30px | 34px |
| Table header height | 32px | 38px |
| Table row height | 34px | 42px |

## Navigation And Information Architecture

Four sidebar groups collapse to three so that **path prefix = sidebar group = first breadcrumb segment**. Every existing URL is preserved; migrated paths keep a redirect.

| Group | Prefix | Pages |
| --- | --- | --- |
| 可观测性 | `/observe/*` | 仪表盘, 请求日志, 请求审计, 路由健康 |
| 路由配置 | `/route/*` | 端点 → 价格模板 → 路由策略 → 入口模型 → 入口模型详情 |
| 系统 | `/system/*` | 设置, 代理密钥 |

Two structural moves:

- **Routing health is promoted out of the dashboard tab** to `/observe/routing-health`. It is a triage entry point and must not be buried in a tab.
- **Models moves from bare `/models` into `/route/models`** (old path redirects), so the configuration chain reads down the sidebar in real dependency order. Configuring top to bottom is itself the guided path.

Shell:

- Sidebar 240px, `panel` ground, 1px right outline. Group labels 11px `text-muted`. Items 32px tall, 6px radius, 16px icon plus 13px label. Active item: `primary-soft` ground, `on-primary-soft` text, 2px primary bar on the left edge — not a solid blue reverse fill. Collapses to a 56px icon rail, not off-canvas. Render the eight icons already declared in `useShellNavigation.ts`.
- Header 48px, `panel` ground, 1px bottom outline. Breadcrumb on the left at 12px. Right side: global search, density toggle, theme toggle, account menu. Theme and account move up from the sidebar footer.
- Breadcrumbs are fixed at **group › page › entity**. A detail page leaf must be the entity name, not a generic word: `路由配置 › 入口模型 › GPT-4o Mini 主线`, never `入口模型 › 配置`.

### Freshness Bar

Every page carrying a time window shows a 32px row directly under the page header:

```
上次更新于 14:32:07 (UTC+8)   ·   自动刷新 30 秒 ▾   ·   ⟳ 刷新        [◐ 缓存滞后 8.2s]
```

Update time and auto-refresh on the left; staleness and lag badges on the right, present only when abnormal. This is how the Honesty Contract's "when is this from" requirement is discharged.

## Layout Rules

- Use `OperatorPageShell` for protected management pages.
- Use `OperatorPageHeader` for title, description, and actions.
- Use `OperatorSectionCard` for repeated section panels.
- Use `OperatorInsetPanel` for nested form groups, dialog summaries, drawer sections, metadata rows, review panels, and other contained sub-sections inside an existing page section.
- Keep route state and API calls in pages/features, not in design-system components.
- Use `gap-*` or density variables instead of `space-x-*` and `space-y-*`.
- Keep page content inside `OperatorPageShell` unless the route is a public auth page.
- Keep app shell, navigation, and breadcrumbs in layout components, not page code.

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

### Layout Discipline

Any of these is rework:

- **The title appears once.** The page header owns the page title; a card header must not repeat it. Card headers carry a state summary (`6 个端点 · 1 个降级`) and that card's own controls.
- **No card inside a card.** Sub-blocks inside a card use `OperatorInsetPanel`.
- **Primary actions live in the page header.** Cards hold only their own secondary actions.
- **Never render the same content twice.** Two views showing the same KPIs and the same chart should be one view with a content switcher, not two tabs.

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
- Labels sit above the input. A placeholder never substitutes for a label.
- Help text is 12px `text-muted` under the input. An error replaces the help text in place and turns the input outline `destructive`.
- Server field errors route through `extractServerValidation` / `fieldErrorsFromServerValidation`; no hook may discard them.

## Tables

- Use `OperatorTableShell` for card-framed management tables.
- Use `shared/table/operationalTableState` for client sorting/page calculations and `shared/table/operationalTable` for skeleton rows/sortable headers; use `shared/table/paginationStates` and `shared/table/paginationControls` for async/append pagination states and controls.
- Empty table states should use `OperatorEmptyState`.
- Keep domain-specific columns and cell renderers colocated with the feature.
- Horizontally scrollable tables must keep empty/loading states aligned to the visible viewport, not the full scroll width.
- Row actions should use icon buttons with accessible names when the action is already clear from the context.

Shared specification, applied by every list page:

- **Header**: sticky, `inset` ground, 12px/500 `text-muted`, not uppercase, 1px bottom outline. Sortable columns carry a 12px direction glyph, and the highlighted column must be the column actually in effect.
- **Rows**: height per density mode, 1px bottom outline, hover at `primary-soft` 20%.
- **Status stripe**: a 2px status-colored bar on the row's left edge, **for runtime state only**. Idle rows get no stripe. Never encode a non-runtime attribute (such as "has references") as a runtime status color.
- **Alignment**: text left; values, currency, latency, and token counts right-aligned in mono with tabular numerals; status badge columns fixed-width and left-aligned.
- **Identifier columns**: mono 13px, middle-elided when long (`gpt-4o-mini-2024…0718`), full value plus a copy control on hover.
- **Entry-model exit mapping** (models list): the targets cell projects the DIRECT `access_targets` rows in shared `(position, id)` order and shows the first two — Terminal Target rows as `端点 → 实际上游模型 ID`, Model Target rows as the logical target only — and folds the remainder into a `还有 N 项，见详情` pointer to the entry-model detail. Decoupled (`上游 ID 已解耦`) and non-participating (`未参与`) rows carry a textual state, never a color-only one. A missing endpoint or upstream identity renders a reasoned `—`; the entry `model_id` is never substituted for it, and the summary never follows Model Target rows recursively.
- **Row actions**: hidden by default, faded in on row hover or focus, reachable by keyboard. More than three actions collapse into an overflow menu.
- **Pagination**: `共 N 条` on the left, page controls and page size on the right.
- **Fill the table.** A list that claims eight rows renders eight rows.

## Dialogs And Drawers

- Keep dialog and sheet behavior, focus management, and close behavior on the existing Radix/shadcn primitives.
- Use consistent title, description, body spacing, and footer action placement.
- Use destructive button styling only for irreversible destructive actions.
- Use `OperatorCallout` for warnings or server validation summaries.
- Prefer `OperatorInsetPanel`, `OperatorCallout`, and shared form/section components inside dialogs and sheets rather than page-local nested cards.
- Dialog widths: small 420px, medium 560px, large 720px.
- Detail drawers slide from the right at 560px, or 720px for payload-bearing detail. Payload and JSON render in an `inset` code block at 12px mono with copy and collapse controls.

### Destructive Flows

Every destructive operation uses `OperatorDestructiveDialog` from `src/shared/design-system/destructive-dialog.tsx`. Do not hand-roll a delete confirmation, and do not confirm a delete from a toast, a menu item, or an inline button alone.

The flow is:

1. **Preflight the dependencies** when the target can be referenced by other configuration. Keep confirmation disabled while the preflight is in flight, and treat a preflight that cannot produce complete facts as blocking rather than as a silent pass.
2. **Blocked when dependencies exist.** List the blocking references in the dialog body and prevent the delete — disable the confirm action, or hide it entirely when the block is terminal — instead of letting the request fail server-side. A conflict the server returns during the delete (`409`) folds back into the same blocked view rather than into a bare error toast.
3. **Impact summary when nothing blocks.** Show the target's identity plus the facts that make it recognizable and the consequences of removing it, and state that the action cannot be undone. A target with no reference relationship skips step 1 and 2 and goes straight to this summary, including any consequence warnings the deletion triggers (for example traffic interruption).
4. **Typed confirmation phrase for high-risk deletes** whose scope is not an enumerable list of rows — batch and bulk data cleanup. Keep confirmation disabled until both the phrase matches and the preflight facts are complete.

Current adopters, and what each one contributes:

- Batch retention cleanup (`src/pages/settings/dialogs/DeleteConfirmDialog.tsx`): retention preflight with matched/retained row counts, non-cascade notes and warnings, plus the typed confirmation phrase.
- Endpoint delete (`src/pages/endpoints/DeleteEndpointDialog.tsx`): asynchronous preflight state machine (`checking` → `eligible` / `blocked` / `integrity_error`) with a paginated reference list; the confirm action is hidden while blocked.
- Pricing template delete (`src/pages/pricing-templates/DeletePricingTemplateDialog.tsx`): terminal-target usage preflight, dependency table, and server conflict rows folded into the same blocked view.
- Loadbalance strategy delete (`src/pages/loadbalance-strategies/DeleteLoadbalanceStrategyDialog.tsx`): attached-model and default-strategy blocks.
- Proxy API key delete (`src/pages/proxy-api-keys/ProxyKeyDeleteAlertDialog.tsx`): no reference relationship, so impact summary only, with traffic-interruption and successor-key warnings.
- Model delete (`src/pages/models/DeleteModelDialog.tsx`): impact summary with API family and access-target count.

Confirm buttons name the action (`删除端点`), never `确认`. A blocked delete keeps its button clickable and explains the block inside the dialog; silent disabling without a reason is not acceptable.

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

State surface specifications:

- **Loading**: skeletons shaped like the real content — table skeletons draw rows, card skeletons draw blocks. Table pages keep the shell and swap in skeleton rows rather than collapsing to a panel spinner. Design-system components must not ship visible default copy; pass `undefined` and let the caller supply localized text.
- **Empty**: centered, 48px `text-disabled` icon, 15px/600 title, 12px description, primary action. **The description states the next step** — `还没有配置端点。先添加一个供应商端点，模型才能路由到它。` First-load empty is page-level with a primary action; a filtered-to-nothing result renders inside the table body with a clear-filters action.
- **Error**: `destructive` outlined card naming what happened, an actionable next step, and a retry control. Status codes and traces collapse behind `查看详情`.
- **Partially degraded**: render the content and attach a `degraded` notice naming which part is unavailable.

Feedback routing: while a dialog stays open, report inline only; once it closes, or for inline row actions, report by toast. Never both.

## Accessibility

- Body text at 4.5:1 or better; large text and graphics at 3:1 or better.
- Focus ring: 2px primary with a 2px offset, visible on **every** interactive element. `outline: none` is never acceptable.
- Table row actions, drawers, dialogs, and menus are all reachable by keyboard. Dialogs trap focus and return it to the trigger on close.
- Status, deltas, and chart series are never distinguished by color alone; add a shape, an arrow, a stroke style, or a label.
- Icon-only controls carry `aria-label` and a hover tooltip.
- Minimum hit target 28×28px.
- Tables use semantic roles: `columnheader` carries `aria-sort`, cells are exposed as `gridcell`, and no row-level `aria-label` swallows the cell contents.

## Charts

- Horizontal grid lines only, `border` color, 1px. No vertical grid lines.
- Axis labels 11px `text-muted`; Y-axis values in mono.
- Single series uses the primary; multi-series takes the spectrum in order with alternating solid and dashed strokes.
- Area fills at 12% of the stroke color.
- Hover crosshair plus an overlay on `raised`, 8px radius, 12px text, series name left and value right in mono.
- Legend above the chart, right-aligned, 12px, clickable to toggle series.
- X-axis renders local time through `useTimezone`, never a raw RFC3339 string. Y-axis carries the unit for the selected metric.
- **No data draws no empty axes** — render the empty state instead.
- Series colors come from `var(--chart-N)` tokens. Hard-coded hex series colors are a defect. No wrapper component is prescribed; charts compose the plotting library directly and carry the rules above themselves.

## Copy

Interface language is Simplified Chinese, single locale. All visible strings and every `aria-label` / `sr-only` string go through `messages`; no new hard-coded literals.

Fixed terminology: 端点, 终端目标, 终端配置 (孤立时称孤立终端配置), 访问目标, 模型目标, 上游模型 ID, 入口模型, 最终目标模型, 价格模板, 路由策略, 路由时段, 代理密钥, 最终承载, 路由尝试, 参与路由, 已知成本, 定价状态, 覆盖, 口径, 可信成本, 未归因. The entry-model management surface (`model_configs`) is named 入口模型 everywhere (sidebar, breadcrumbs, page titles, search, filters, KPI cards, CRUD); 入口模型, 模型目标, and 上游模型 ID are distinct terms and never substitute for each other.

Left untranslated: Prism, Gateway, API family, epoch, cutoff, generation, FX, preflight, curl, OpenAI, Anthropic, Gemini.

- Buttons are verb phrases (`添加端点`, `保存更改`, `重置封禁`), never `确定` or `提交`.
- Values carry units: `342 ms`, `$12.48`, `1.2M`, `99.2%`.
- Times follow the global timezone setting; relative times carry an absolute tooltip.
- Errors say what happened and where to go: `端点连接失败（401）。检查该端点的 API 密钥是否有效。`
- The honesty copy already shipped must not be weakened or rewritten.

Product code should import operator components from `@/shared/design-system` directly instead of adding compatibility wrappers under `@/components`.
