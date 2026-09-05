# Auth session coordination

- `sessionCoordinator.ts` owns the process-local phase/epoch machine; `coordinatorInstance.ts` exports its singleton and browser wiring. Providers and views subscribe instead of introducing another auth state machine.
- Protected-management 401 recovery shares one refresh flight. `../../lib/api/request.ts` permits at most one replay and fences responses by epoch. Runtime `/v1` and `/v1beta` proxy-key failures never invalidate the management session.
- `authExempt.ts` owns auth-exempt request classification. The auth-disabled 401 probe is the fixed ordinary `GET /api/models` read, single-flight and generation-bound; an exhausted incident must not restart a refresh/redirect loop.
- Keep proactive/visibility refresh rules in `refresh.ts`; passive refresh waits while login/logout mutations are active.
- `crossTab.ts` broadcasts only generation/auth-state signals. New identity or auth-mode changes cause re-bootstrap; never place session secrets in the broadcast or browser storage.
- Blocking phases render `../../app/router/GlobalAccessLayer.tsx` through the route gates, without exposing stale authenticated pages or page-level 401 toasts.

Use the colocated coordinator tests and `../../../tests/e2e/auth-session-lifecycle.spec.ts` for changes across provider/request/gate boundaries.
