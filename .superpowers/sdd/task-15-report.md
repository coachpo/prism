STATUS: DONE

Commits:
- `feat: replace endpoint drag reorder with move buttons` (created by this task; final hash reported by the assistant after commit creation)

Files changed:
- Replaced endpoint drag-and-drop wiring with button-based move controls:
  - `frontend/src/features/endpoints/EndpointsFeaturePage.tsx`
  - `frontend/src/features/endpoints/useEndpointsFeatureData.ts`
  - `frontend/src/pages/endpoints/EndpointCard.tsx`
  - `frontend/src/pages/endpoints/useEndpointReorder.ts`
- Removed `@dnd-kit/*` package and lockfile entries:
  - `frontend/package.json`
  - `frontend/pnpm-lock.yaml`
- Added endpoint move i18n keys and removed stale endpoint drag copy:
  - `frontend/src/i18n/messages/en.ts`
  - `frontend/src/i18n/messages/zh-CN.ts`
- Updated the endpoint leaf ownership doc:
  - `frontend/src/pages/endpoints/AGENTS.md`
- Added focused endpoint button reorder coverage:
  - `frontend/src/test/task-15-endpoint-reorder.test.tsx`

Tests and commands:
- PASS: `rg "dnd-kit" frontend` returned 0 matches.
- PASS: `rg -n "useSortable|DndContext|DragOverlay|SortableContext|arrayMove|@dnd-kit|SortableEndpointCard|dragToReorder\\(|activeDragEndpoint|handleDrag|visibleEndpointIds" frontend/src frontend/package.json frontend/pnpm-lock.yaml` returned 0 matches.
- PASS: `cd frontend && pnpm exec vitest run src/test/task-15-endpoint-reorder.test.tsx`.
- PASS: `cd frontend && CI=1 pnpm run test` ran 14 files / 37 tests.
- PASS: `cd frontend && pnpm run test:lib` ran 71 tests.
- PASS: `cd frontend && pnpm run test:server` ran 4 tests.
- PASS: `cd frontend && pnpm run build`; Vite reported existing large-chunk warnings.
- PASS: `cd frontend && pnpm run lint`.

Concerns:
- None. Manual browser/backend-data proof was not run; the focused MSW-backed Vitest asserts the move button sends `PATCH /api/endpoints/:id/position` with the expected `to_index` and updates the rendered order.
