STATUS: DONE

Commits:
- `feat: replace routing diagram flow rendering with plain list` (created by this task; final hash reported by the assistant after commit creation)

Files changed:
- Replaced `RoutingDiagramCard.tsx` desktop/compact switching with the existing plain list renderer.
- Moved retained routing graph/list shaping from `routingDiagramLayout.ts` to `routingDiagramData.ts` and removed all flow exports from `routingDiagram.ts`.
- Deleted the seven routing flow files required by the brief and deleted the stale routing-shell e2e spec.
- Salvaged the CI-included routing contract as `frontend/tests/lib/dashboard_routing_list_contract.test.mjs`; it no longer loads deleted modules.
- Removed `@xyflow/react` from `frontend/package.json`, `frontend/pnpm-lock.yaml`, and `frontend/src/main.tsx`.
- Removed i18n keys used only by deleted flow/desktop surfaces.
- Updated frontend/dashboard/routing/test AGENTS docs to describe the plain-list routing presentation.

Verification:
- `cd frontend && pnpm run test:lib`: PASS, 76 tests.
- `cd frontend && pnpm run test`: PASS, 12 files / 34 tests.
- `cd frontend && pnpm run build`: PASS; Vite reported existing large-chunk warnings.
- `cd frontend && pnpm run lint`: PASS.
- `rg -n "xyflow" frontend`: PASS, no matches.
- `rg -n "React Flow|@xyflow|dashboard_routing_flow|routingDiagramFlow|RoutingDiagramFlow|routing-diagram-desktop" frontend`: PASS, no matches.
- `git diff --check -- frontend .superpowers/sdd/task-13-report.md`: PASS.

Concerns:
- None.

---

STATUS: DONE

Review fix:
- Restored plain-list node inspection without adding React Flow or xyflow back.
- List node-card clicks now render the retained `RoutingDiagramInspectorContent`; explicit action buttons still call model/request-log navigation callbacks without opening the inspector.

Files changed:
- `frontend/src/pages/dashboard/RoutingDiagramCard.tsx`
- `frontend/src/pages/dashboard/routing-diagram/RoutingDiagramMobileList.tsx`
- `frontend/src/pages/dashboard/RoutingDiagramCard.test.tsx`
- `frontend/tests/lib/dashboard_routing_list_contract.test.mjs`

Verification:
- `cd frontend && pnpm exec vitest run src/pages/dashboard/RoutingDiagramCard.test.tsx`: PASS, 2 tests.
- `cd frontend && node --test tests/lib/dashboard_routing_list_contract.test.mjs`: PASS, 7 tests.
- `cd frontend && pnpm run test:lib`: PASS, 76 tests.
- `cd frontend && pnpm run test`: PASS, 13 files / 36 tests.
- `cd frontend && pnpm run build`: PASS; Vite reported existing large-chunk warnings.
- `cd frontend && pnpm run lint`: PASS.
- `rg -n "xyflow|React Flow|@xyflow|RoutingDiagramFlow|routingDiagramFlow" frontend`: PASS, no matches.
- `git diff --check -- frontend/src/pages/dashboard/RoutingDiagramCard.tsx frontend/src/pages/dashboard/routing-diagram/RoutingDiagramMobileList.tsx frontend/src/pages/dashboard/RoutingDiagramCard.test.tsx frontend/tests/lib/dashboard_routing_list_contract.test.mjs .superpowers/sdd/task-13-report.md`: PASS.

Concerns:
- None.
