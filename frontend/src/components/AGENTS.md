# FRONTEND COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`src/components/` holds Prism's shared shell chrome and reusable UI. The dense shell-state cluster lives under `layout/app-layout/`, while this parent owns the shared widgets and design-system leaves that multiple routes or shell surfaces reuse.

## STRUCTURE
```text
components/
├── AnimatedListItem.tsx                               # Shared animated list row used across route surfaces
├── ApiFamilyIcon.tsx + ApiFamilySelect.tsx            # Shared API-family icon and picker helpers
├── CompactMetricTile.tsx                              # Shared compact metric tile
├── CopyButton.tsx                                     # Shared copy affordance
├── EmptyState.tsx                                     # Shared empty-state shell
├── IconActionGroup.tsx                                # Shared icon action cluster
├── MetricCard.tsx                                     # Shared metric summary card
├── MonitoringProbeHistoryStrip.tsx                    # Shared monitoring history strip
├── PageHeader.tsx                                     # Shared page header shell
├── StatusBadge.tsx                                    # Shared status badge
├── SwitchController.tsx                               # Shared switch wrapper and control row
├── VendorIcon.tsx + VendorSelect.tsx + vendorIcon*.tsx # Shared vendor icon and picker helpers
├── WebSocketStatusIndicator.tsx                       # Shared realtime connection badge
├── layout/app-layout/AGENTS.md                        # Dense shell-state cluster behind AppLayout
├── loadbalance/AGENTS.md                              # Shared loadbalance renderers and tests
├── statistics/AGENTS.md                               # Shared statistics renderers
└── ui/AGENTS.md                                       # shadcn/ui primitives and local wrappers
```

## WHERE TO LOOK
- Shell chrome and layout handoff: `layout/AppLayout.tsx`
- Shell state cluster, nav/version ownership, and profile-switcher behavior: `layout/app-layout/AGENTS.md`
- Shared language/theme controls: `GlobalPreferencesControls.tsx`, `LanguageSwitcher.tsx`, `ThemeToggle.tsx`
- Shared list, compact metric, copy, empty-state, icon action, metric, header, status badge, and switch widgets: `AnimatedListItem.tsx`, `ApiFamilyIcon.tsx`, `ApiFamilySelect.tsx`, `CompactMetricTile.tsx`, `CopyButton.tsx`, `EmptyState.tsx`, `IconActionGroup.tsx`, `MetricCard.tsx`, `MonitoringProbeHistoryStrip.tsx`, `PageHeader.tsx`, `StatusBadge.tsx`, `SwitchController.tsx`
- Shared realtime connection badge: `WebSocketStatusIndicator.tsx`
- Shared vendor icon and picker surfaces: `VendorIcon.tsx`, `VendorSelect.tsx`, `vendorIconRegistry.tsx`, `vendorIconComponents.tsx`
- Shared loadbalance rendering: `loadbalance/LoadbalanceBadges.tsx`, `loadbalance/LoadbalanceEventsTable.tsx`, `loadbalance/LoadbalanceEventDetailSheet.tsx`, `loadbalance/__tests__/`
- Shared statistics rendering: `statistics/TopSpendingCard.tsx`, `statistics/TokenMetricCell.tsx`
- Design-system primitives and local wrappers: `ui/`

## CHILD DOCS
- `layout/app-layout/AGENTS.md`: shell chrome, profile switcher, profile dialogs, and visible version-label ownership.
- `loadbalance/AGENTS.md`: shared loadbalance badges, table, event detail sheet, and tests.
- `statistics/AGENTS.md`: shared statistics cards and token metric cells.
- `ui/AGENTS.md`: shadcn/ui primitives and local wrappers in `src/components/ui/`.

## CONVENTIONS
- Keep shared components presentation-first.
- Keep data fetching and route state out of this tree.
- Keep shell-state ownership in `layout/app-layout/`; the direct components here should stay compositional or presentational.
- Keep locale and theme controls in the shared preference widgets instead of duplicating them in auth pages or shell headers.
- Keep shared websocket-health presentation in `WebSocketStatusIndicator.tsx` rather than rebuilding connection badges in pages.
- Reuse `ui/` primitives before adding one-off markup.
- Keep the leaf docs in `ui/` for primitive-level wrappers, and keep this parent focused on the shared widgets above them.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not move nav-link, profile-dialog, or version-label logic out of `layout/app-layout/`.
- Do not put page-specific fetches or route-state parsing in shared components.
- Do not duplicate vendor icon fallback logic or websocket-health badges across route folders.
