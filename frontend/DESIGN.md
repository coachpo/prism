# Prism Design System

This is the binding UI design contract. Component and route references identify the current implementation owners. The requirements below remain binding where an existing implementation differs.

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
| Still loading | A skeleton, or `指标仍在读取中`. **Never the reason a value would be absent** |

Supporting rules:

- **`?? 0` in a render path is a defect.** Absent goes to `—`; failed goes to an error surface.
- **Stale data is labelled stale, not repainted as fresh.** On a failed refresh keep the last successful data and attach the staleness badge carrying the last success time and the failure reason. Exactly one staleness badge design exists site-wide.
- **Every data block answers "when is this from".** Pages with a time window carry a freshness bar. Its timestamp is when the data was **observed** (the read model's `generated_at`), never when some configuration row was last edited; with no successful read yet it renders `OperatorMissingValue`, never `刚刚`. A refresh control must actually refresh — all of the reads that feed the page, not a third of them — and it shows its pending state.
- **Columns and KPIs with a time window consume the backend's `coverage`.** When the declared window is clipped by retention, the header states the window actually in effect and carries the `保留期外` badge. A 30-day column that only holds three days of data is not a 30-day column.
- **Basis must be labelled.** When one column is a window total and another is the last bucket, say so in the column header. When a block uses a different time window than the page, say so on the block.
- **Never leak enum keys.** `priced`, `unpriced`, `healthy`, `stream_outcome` and friends pass through a Chinese label dictionary before display. Dotted registry identifiers such as `anthropic.messages` are enum keys too: brand nouns like Anthropic and Gemini stay untranslated, the identifier around them does not reach the screen. A label dictionary needs a named fallback for the key it has not learned yet, never the key itself.

### Observability Attribution

- `入口请求` (`ingress`) counts one finalized ingress once and attributes it to the requested entry model.
- `最终承载` (`final_execution`) counts one finalized request once and attributes it to the actual final target model and winning Terminal Target.
- `路由尝试` (`route_attempt`) counts each actual upstream attempt, including failed retries. It does not aggregate or infer provider cost; an unknown failed-attempt charge renders as `—` with an explanation.
- Parent request cost and child attempt cost are composition, not two totals to add. Request-chain surfaces label the parent and child values explicitly and keep routing completeness separate from pricing/cost evidence.
- Every URL-backed metrics scope switch — the models list's three scopes, the model detail's two — is the same controlled single-select segmented control (never tabs), with the same keyboard model: an arrow key selects, it does not merely preview. It carries a visible label, shows a fixed attribution-basis note for the active scope, sits adjacent to the columns it re-bases, keeps the selection URL-backed, and re-selecting the active scope never clears it. A scope a page cannot serve is rendered disabled **with its reason**, not omitted.

## Visual Direction

The product name supplies the concept. A prism splits one incident beam into a spectrum — exactly what the gateway does: one entry, many exits.

- **The incident beam is the primary blue.** Every "entry" semantic shares one blue: primary actions, active navigation, links, focus rings. There is one blue, not a light and a dark one.
- **The spectrum is the categorical palette.** Providers, models, and terminal targets — the "many exits" — are separated by a controlled six-hue spectrum.
- **Separation is contextual, not chromatic.** Spectrum colors appear only in data-encoding contexts (chart series, legends, group tags). Status colors appear only in status contexts (status badges, table row stripes). The two palettes may share hues — spectrum 1 is the primary and spectrum 3 is the healthy green — because context already separates them and status always carries a shape and a label as well. Forcing the spectrum to dodge green, amber, and red would crowd all six series into the blue-violet range and make them harder to tell apart.
- Light-first admin surfaces, both themes fully specified.
- Layering comes from 1px outlines, not shadows. Shadows are reserved for genuinely floating overlays.
- No decorative gradients, blur blobs, heavy shadows, or marketing-style hero layouts.

## Tokens

Tokens live in [`src/index.css`](src/index.css) and are declared by `operatorColorTokens` in [`src/shared/design-system/foundation.ts`](src/shared/design-system/foundation.ts). The values below are implemented in both light and dark themes. [`tests/lib/design_token_contract.test.mjs`](tests/lib/design_token_contract.test.mjs) checks token presence, exact values, shadcn aliases, contrast, spectrum separation, density variables, and status markers.

### Primary

| Role | Light | Dark |
| --- | --- | --- |
| `primary` | `#1E4FD8` | `#9FBEFF` |
| `on-primary` | `#FFFFFF` | `#04266E` |
| `primary-soft` | `#E4EBFF` | `#1B2E63` |
| `on-primary-soft` | `#123A9E` | `#CFDDFF` |

### Surfaces — three tiers plus an inset

`canvas`, `panel`, and `raised` form the surface tiers; `inset` marks contained sub-blocks. Express layering with a 1px outline. Dark mode makes shadows nearly invisible, so shadow can never be the only layering device.

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

`operatorStatusTiers` declares the four mutually exclusive tiers below. Notice severity is a separate axis: `OperatorCallout` uses `info`, `success`, `warning`, `danger`, or `muted`; those intents do not add runtime status tiers.

| Tier | Meaning | Light | Dark | Shape |
| --- | --- | --- | --- | --- |
| `healthy` | Serving normally | `#0F7B4F` | `#66D9A0` | ● |
| `degraded` | Retrying, partial failure, nearing a limit | `#8C5200` | `#F5C063` | ◐ |
| `failing` | Banned, consecutive failures, unreachable | `#C0342B` | `#FF8A80` | ▲ |
| `idle` | Idle, not enabled, no data | `#5B6370` | `#8A93A1` | ○ |

**Status is always color plus shape plus text.** Badge geometry: 8px shape marker, 12px label, 20px tall, 4px radius, 6px horizontal padding; background at 10% of the status color, outline at 25%.

`destructive` shares the `failing` hue but not its meaning: it marks **irreversible operations only** (delete buttons, confirmation dialogs) and never describes runtime state. It is its own token in both themes (`#C0342B` light, `#FF6B61` dark) and carries `on-destructive` (`#FFFFFF` light, `#2B0705` dark) for text on a filled destructive surface — never a hard-coded `text-white`, whose 1.9:1 on the dark ground fails outright.

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

The foreground values below are measured against their theme's `panel` using the WCAG 2.1 relative-luminance formula. `text-disabled` is excluded from informative text: its light-theme contrast is 3.10:1, below the 4.5:1 text requirement.

| Token | Light | Dark |
| --- | --- | --- |
| `primary` | 6.63 | 9.29 |
| `text` | 18.16 | 14.21 |
| `text-muted` | 6.00 | 6.72 |
| `healthy` | 5.29 | 9.88 |
| `degraded` | 6.32 | 10.38 |
| `failing` | 5.57 | 7.58 |
| `idle` | 6.06 | 5.58 |
| `destructive` | 5.57 | 6.20 |
| Spectrum 1–6 | 4.83 – 6.63 | > 7 |

`on-destructive` is measured against `destructive`, not `panel`: 5.57 light, 6.63 dark. Dark `idle` is the floor of the whole table — 5.58 on `panel`, 5.10 on `raised` (dialogs, dropdowns, drawers), 5.85 on `inset` — so it is the token to re-measure first whenever a surface changes.

A badge's background is an **opaque mix** (`color-mix(... , var(--color-panel))`), not a translucent overlay: the same badge lands on `panel` and on `inset`, and a translucent ground makes its contrast depend on whichever container it happens to sit in. Against `inset` the measured floor stays above 4.5 — `degraded` 5.64, `idle` 5.41.

**Recompute this table when adding or changing any color. Never approve a color by eye.** Update the CSS and `operatorColorTokens` together and run `pnpm run test:lib` from `frontend/`.

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

4px base grid with an **8px** card radius — large radii make adjacent cards read as loose in a dense console.

| Token | Value |
| --- | --- |
| Radius: badge / marker | 4px |
| Radius: control / input | 6px |
| Radius: card / table container | 8px |
| Radius: dialog / drawer | 12px |
| Shadow: overlay | `0 8px 24px -12px rgba(16,22,30,.28)`; dark `0 12px 32px -16px rgba(0,0,0,.6)` |

Two density modes, `standard` and `compact`, are declared in `foundation.ts` and switchable from the header's `DensityToggle`. The shell applies the selection through `data-density` on `<html>` and saves it locally; `standard` is the default.

| Token | Compact | Standard (default) |
| --- | --- | --- |
| Page padding | 16px | 24px |
| Section gap | 16px | 20px |
| Card padding | 12px | 16px |
| Control height | 30px | 34px |
| Table header height | 32px | 38px |
| Table row height | 34px | 42px |
| Control height (sm) | 28px | 30px |
| Control height (xs) | 24px | 26px |

Row height is a **minimum**, not a target: at standard density a cell defaults to one line and may carry one identifier sub-line, never more than two lines total. A density mode must shrink cell leading and gaps along with the row height — a compact mode that only lowers the row minimum saves nothing. Neither density mode may take a hit area below 28×28; `xs` is only for secondary icon controls that already sit inside a ≥28px hit area.

## Navigation And Information Architecture

Three sidebar groups follow **path prefix = sidebar group = first breadcrumb segment**. The shared route metadata in `src/components/layout/app-layout/useShellNavigation.ts` owns sidebar entries and breadcrumbs; `src/app/router/appRouter.tsx` owns mounted routes and redirects.

| Group | Prefix | Pages |
| --- | --- | --- |
| 可观测性 | `/observe`, `/observe/*` | 仪表盘, 请求日志, 路由健康; 请求审计 is a request detail route |
| 路由配置 | `/route/*` | 端点 → 价格模板 → 路由策略 → 模型配置; 模型配置详情 and 导出客户端配置 are child routes |
| 系统 | `/system/*` | 设置, 代理密钥 |

Route relationships:

- **Routing health has its own sidebar entry** at `/observe/routing-health`. It is a triage entry point and must not be buried in a tab. The former `/observe?tab=events` entry redirects with the routing-health window and filters.
- **Model configuration lives at `/route/models`**. `/models` and `/models/:modelId` redirect to the corresponding route under `/route/models`, so the configuration chain reads down the sidebar in dependency order. Configuring top to bottom is itself the guided path.
- **Request audit is a detail page** at `/observe/requests/:requestId/audit`; it keeps 请求日志 active in the sidebar. Model detail and export similarly keep 模型配置 active.

Shell:

- Desktop sidebar 240px, `panel` ground, 1px right outline. Group labels 11px `text-muted`. Items 32px tall, 6px radius, 16px icon plus 13px label. Active item: `primary-soft` ground, `on-primary-soft` text, 2px primary bar on the left edge — not a solid blue reverse fill. Collapses to a 56px icon rail, not off-canvas. Render each entry's icon from `useShellNavigation.ts`. Three bands, no cliff between them: **≥1280** expanded 240px; **768–1279** defaults to the icon rail (the operator's explicit choice is remembered and wins); **<768** only, the shared sidebar primitive uses a sheet and closes it after navigation. Below `sm` the header's global search collapses to a single icon button and the breadcrumb renders only its last two levels — a full trail there compresses every level to a couple of pixels.
- Header 48px, `panel` ground, 1px bottom outline. Breadcrumb on the left at 12px. Right side: global search, density toggle, theme toggle, account menu. Theme and account controls belong in the header.
- Breadcrumbs are fixed at **group › page › entity**. A detail page leaf must be the entity name, not a generic word: `路由配置 › 模型配置 › GPT-4o Mini 主线`, never `模型配置 › 配置`.

### Freshness Bar

Every page carrying a time window must show `OperatorFreshnessBar` directly under the page header, with a 32px minimum height and wrapping when needed:

```
上次更新于 14:32:07 (UTC+8)   ·   ⟳ 刷新   ·   口径：24h 窗口        [◐ 缓存滞后 8.2s]
```

Update time, refresh controls, and window basis sit on the left; staleness and lag badges sit on the right, present only when abnormal. Show the optional auto-refresh selector only when the owning page supplies a functioning refresh schedule; the current Observe page refreshes on entry, URL changes, or explicit action. This is how the Honesty Contract's "when is this from" requirement is discharged.

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
- **Never render the same content twice.** Two views showing the same KPIs and the same chart should be one view with a content switcher, not two tabs. One count is rendered in one place: a subtitle, a KPI, and a pagination line that each report "how many" will eventually disagree.
- **A control sits next to what it re-bases.** A scope switch that renames four columns belongs in those columns' header group, not a thousand pixels away in the card header. It carries a visible label, not just an `aria-label`.
- **Sections are ordered by task frequency**, and a writable configuration surface comes before the read-only evidence that supports it. Reference panels that are not part of the task default to collapsed and remember the operator's choice.
- **`OperatorInsetPanel` does not nest.** Key-value rows inside one wrap at 48rem — a label on the far left and its value a thousand pixels away is not a row anyone reads.
- **A page has exactly one `main` landmark**, provided by the shell. Section titles are real `h2` elements and their cards are labelled by them.
- **KPI cards that filter the table below must look pressed when they are**: `aria-pressed`, a primary border and edge bar, plus a filter affordance on the label. An anomaly card with a non-zero value carries its own badge and tone; at zero it stays ordinary, because zero anomalies is good news.

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

- **Header**: sticky, `inset` ground, 12px/500 `text-muted`, not uppercase, 1px bottom outline. Sticky requires the table's nearest scrolling ancestor to be the page or the table's own scroll container — a bare `overflow-x-auto` wrapper becomes the containing block and silently kills it, so a table that scrolls horizontally must put both axes on the same container. Sortable columns carry a 12px direction glyph in `text-muted` (never `text-disabled`, which never carries information) and the column in effect renders its glyph in `primary`; the sort hit area is the whole header cell.
- **Rows**: height per density mode, 1px bottom outline, hover at `primary-soft` 20%.
- **Status stripe**: a 2px status-colored bar on the row's left edge, **for runtime state only**. Idle rows get no stripe. Never encode a non-runtime attribute (such as "has references") as a runtime status color.
- **Alignment**: text left; values, currency, latency, and token counts right-aligned in mono with tabular numerals; status badge columns fixed-width and left-aligned.
- **Identifier columns**: mono 13px, middle-elided when long (`gpt-4o-mini-2024…0718`), full value plus a copy control on hover.
- **Model-config exit mapping** (models list): the targets cell projects the DIRECT `access_targets` rows in shared `(position, id)` order and shows the first two — Terminal Target rows as `端点 → 实际上游模型 ID`, Model Target rows prefixed `模型目标 →` — with the count and the first row sharing one line and the second row sharing a line with the remainder, capped at two lines. When exactly one row would remain, render it instead of folding: folding the last one saves no height. Otherwise the remainder folds into a `还有 N 项，见详情` pointer to the model-config detail. `入口同名` applies only when the owning model configuration has `direct_request_enabled=true` and its ID exactly matches the upstream ID; every upstream identity on a non-entry configuration is `仅上游`, even when the strings match. These identity states and non-participating (`未参与`) rows carry text, never color alone. A missing endpoint or upstream identity renders a reasoned `—`; the owning model configuration's `model_id` is never substituted for it, and the summary never follows Model Target rows recursively.
- **Row actions**: faded out by default under a hovering pointer, faded in on row hover or focus, reachable by keyboard; the overflow-menu trigger stays visible (dimmed) so a row never looks actionless, and under `(hover:none)` every row action is visible. More than three actions collapse into an overflow menu.
- **Frozen columns**: a table that scrolls horizontally freezes its identity column (`left-0`) and its actions column (`right-0`) against the panel ground with a 1px inset separator. Row actions that live past the scroll edge are not reachable.
- **Drag-to-reorder**: every drag ordering must have a keyboard and a menu equivalent — the handle is a 28×28 button handling ArrowUp/ArrowDown/Home/End, the row overflow menu carries 上移/下移/移到首位 (disabled at the boundaries), and each move is announced through `aria-live`. Only the handle arms the drag, so row text stays selectable, and the drop target renders a 2px insertion line.
- **Pagination**: `共 N 条` on the left, page controls and page size on the right.
- **Fill the table.** A list that claims eight rows renders eight rows.

## Dialogs And Drawers

- Keep dialog and sheet behavior, focus management, and close behavior on the existing Radix/shadcn primitives.
- Use consistent title, description, body spacing, and footer action placement.
- Use destructive button styling only for irreversible destructive actions.
- Use `OperatorCallout` for warnings or server validation summaries.
- Prefer `OperatorInsetPanel`, `OperatorCallout`, and shared form/section components inside dialogs and sheets rather than page-local nested cards.
- Dialog widths: small 420px, medium 560px, large 720px — selected through `DialogContent`'s `size` prop. Call sites do not write `max-w-*`.
- **Dialog height**: a dialog is never taller than the viewport minus twice the page padding. The body scrolls inside `DialogBody`; the header and footer stay put. A dialog must never rely on the page scrolling to reveal its own title or close button.
- Every dialog returns focus to the element that opened it on close, including controlled dialogs with no Radix trigger.
- A form dialog is a real `<form>`: Enter submits, required fields are marked, and the draft resets when the dialog opens a new session.
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
- Endpoint delete (`src/pages/endpoints/DeleteEndpointDialog.tsx`): asynchronous preflight state machine (`checking` → `eligible` / `blocked` / `check_error` / `integrity_error`) with a paginated reference list and retry for failed checks; the confirm action is hidden while blocked.
- Pricing template delete (`src/pages/pricing-templates/DeletePricingTemplateDialog.tsx`): terminal-target usage preflight, dependency table, and server conflict rows folded into the same blocked view.
- Loadbalance strategy delete (`src/pages/loadbalance-strategies/DeleteLoadbalanceStrategyDialog.tsx`): attached-model and default-strategy blocks.
- Proxy API key delete (`src/pages/proxy-api-keys/ProxyKeyDeleteAlertDialog.tsx`): no reference relationship, so impact summary only, with traffic-interruption and successor-key warnings.
- Model delete (`src/pages/models/DeleteModelDialog.tsx`): client-side referrer preflight over the loaded model configurations, blocked view listing the configurations that target this one, and delete failures reported inline instead of by toast. Reachable from both the list row menu and the detail page header.
- Access target removal (`DeleteAccessTargetDialog` in `src/pages/models/AccessTargetsEditor.tsx`): impact summary naming the target, its endpoint, upstream model ID, pricing template, routing participation and position, with an extra warning when it is the last enabled target.

Confirm buttons name the action (`删除端点`), never `确认`. The entry point that opens a blocked-delete dialog stays clickable so the operator can inspect the reason; the dialog's confirm action remains disabled or hidden. Silent disabling without a reason is not acceptable.

## Status And Feedback

- Use `OperatorStatusBadge` for **runtime state only** — the four runtime tiers carry a shape marker and a color the eye reads as "how is it doing right now". A configuration boolean such as `is_enabled`, or a derived classification such as coverage, is not runtime state: use `OperatorTypeBadge` or plain text. A grid of green dots that only means "enabled" reads as "all healthy" when nothing has been observed at all.
- Use `OperatorTypeBadge` for categories and classifications.
- Use `OperatorValueBadge` for raw values such as HTTP codes, methods, priorities, and percentages.
- Use `OperatorCallout` for warnings, information, success, destructive, and muted notices.
- Use `OperatorEmptyState`, `OperatorLoadingState`, and `OperatorErrorState` for common state surfaces.
- Use `toast()` from `sonner` for notifications.
- Loading surfaces should have `role="status"` and polite live-region behavior.
- Error surfaces should be visually contained and use `role="alert"` where appropriate.
- Empty states should explain what happened and provide the next useful action when one exists.

State surface specifications:

- **Loading is not absence.** While a value is being read, render a skeleton or say `指标仍在读取中`; never render the reason a value would be missing (`窗口内没有已最终化请求`, `没有可用延迟样本`) for a read that has not come back. A whole metric group enters and leaves the pending state together.
- **Loading**: skeletons shaped like the real content — table skeletons draw rows, card skeletons draw blocks. Table pages keep the shell and swap in skeleton rows rather than collapsing to a panel spinner. Design-system components must not ship visible default copy; pass `undefined` and let the caller supply localized text.
- **Empty**: centered, 48px `text-disabled` icon, 15px/600 title, 12px description, primary action. **The description states the next step** — `还没有配置端点。先添加一个供应商端点，模型才能路由到它。` First-load empty is page-level with a primary action; a filtered-to-nothing result renders inside the table body with a clear-filters action.
- **Error**: `destructive` outlined card naming what happened, an actionable next step, and a retry control. Status codes and traces collapse behind `查看详情`.
- **Partially degraded**: render the content and attach a `degraded` notice naming which part is unavailable.

Feedback routing: while a dialog stays open, report inline only — every validation branch included, and the inline callout sits at the top of `DialogBody`, not below the fold; once it closes, or for inline row actions, report by toast. Never both. Toasts render bottom-right so they never cover the shell controls or the page header's primary action.

An inline switch that writes immediately needs no confirmation, but its success toast carries an 撤销 action; changing the state of several rows at once lists what it will affect and asks first.

## Accessibility

- Body text at 4.5:1 or better; large text and graphics at 3:1 or better.
- Focus ring: 2px primary with a 2px offset, visible on **every** interactive element. `outline: none` is never acceptable.
- Table row actions, drawers, dialogs, and menus are all reachable by keyboard. Dialogs trap focus and return it to the trigger on close.
- Status, deltas, and chart series are never distinguished by color alone; add a shape, an arrow, a stroke style, or a label.
- Icon-only controls carry `aria-label` and a hover tooltip.
- Any reason, basis, or help text must be reachable by keyboard and assistive technology. A `title` attribute or an `aria-hidden` glyph is not an implementation — use `OperatorHelpHint` (a 28×28 button with a tooltip) or associate the text with `aria-describedby`.
- Minimum hit target 28×28px. This is the **hit area**, not the visible graphic: a 16px checkbox or an 18px switch reaches it through a transparent pseudo-element or its containing cell. A density mode never compresses a hit area below it.
- Tables use semantic roles: `columnheader` carries `aria-sort` and cells stay `table`/`cell`; a column header's visible name does not swallow its full basis text (associate the basis instead), and no row- or cell-level `aria-label` swallows the cell contents.

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

Fixed terminology: 端点, 终端目标, 终端配置 (孤立时称孤立终端配置), 访问目标, 模型配置, 模型目标, 上游模型 ID, 入口模型, 仅模型目标, 最终目标模型, 价格模板, 路由策略, 路由时段, 代理密钥, 最终承载, 路由尝试, 参与路由, 已知成本, 定价状态, 覆盖, 口径, 可信成本, 未归因. The inventory, detail, and CRUD surfaces for `model_configs` are named 模型配置. A model configuration with `direct_request_enabled=true` is classified as 入口模型; one with `direct_request_enabled=false` is classified as 仅模型目标. 模型配置, 入口模型, 仅模型目标, 模型目标, and 上游模型 ID are distinct terms and never substitute for each other. 已知成本 is the single name for `known_cost_micros`; a scope difference belongs in the column basis or the KPI detail, never in a new noun such as 可信已知成本. 覆盖 refers to routing-target coverage only — incomplete sampling is 样本不全, and a window clipped by retention is 保留期外.

Left untranslated: Prism, Gateway, API family, epoch, cutoff, generation, FX, preflight, curl, OpenAI, Anthropic, Gemini.

- Buttons are verb phrases (`添加端点`, `保存更改`, `重置封禁`), never `确定` or `提交`.
- Values carry units: `342 ms`, `$12.48`, `1.2M`, `99.2%`.
- Times follow the global timezone setting; relative times carry an absolute tooltip.
- Errors say what happened and where to go: `端点连接失败（401）。检查该端点的 API 密钥是否有效。`
- The honesty copy already shipped must not be weakened or rewritten.

Product code should import operator components from `@/shared/design-system` directly instead of adding compatibility wrappers under `@/components`.
