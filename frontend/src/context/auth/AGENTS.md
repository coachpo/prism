# FRONTEND AUTH HELPER KNOWLEDGE BASE

## OVERVIEW
`context/auth/` is the helper layer behind `../AuthContext.tsx`. It owns the process-local operator auth session coordinator (bootstrap, phase machine, epoch, singleflight refresh, fail-closed recovery), login/logout mutation wrappers, cross-tab auth broadcasts, and passive or proactive refresh rules for cookie-backed operator auth. The coordinator is a process-level singleton (`coordinatorInstance.ts`) so concurrent and late 401s from any protected management request produce exactly one coordination event and immediately block false zeros.

## STRUCTURE
```
auth/
├── sessionCoordinator.ts   # Phase machine, epoch, singleflight refresh, classifiers
├── coordinatorInstance.ts  # Process-level coordinator singleton + shared browser listeners
├── refreshOutcome.ts       # Typed refresh outcome contract (recovery/passive/disabled probe)
├── authExempt.ts           # Auth-exempt path/query matcher shared with api/request and gates
├── crossTab.ts             # Cross-tab generation + auth-state broadcast signaling
├── refresh.ts              # Proactive timer interval and passive refresh guards
└── *.test.ts               # Session-coordinator phase/epoch/singleflight coverage
```

## WHERE TO LOOK

- Coordinator phase machine (`BOOTSTRAPPING`, `AUTH_DISABLED`, `AUTH_DISABLED_VERIFYING`, `ANONYMOUS`, `AUTHENTICATED`, `REFRESHING`, `SESSION_EXPIRED`, `LOGGING_OUT`, `AUTH_TRANSITION_FAIL_CLOSED`, `AUTH_UNAVAILABLE`), epoch rotation, refresh singleflight and replay rules, logout intent, disabled-401 breaker probe, and classifier entrypoints (`applyBootstrapStatus`, `applyLoginSuccess`, `ensureRecoveryFlight`, `ensurePassiveFlight`, `beginDisabledVerification`, `getEpoch`, `getPhase`, `subscribe`): `sessionCoordinator.ts`
- Singleton wiring, storage of `prism.authSessionGeneration`, and shared storage/broadcast listeners: `coordinatorInstance.ts`, `crossTab.ts`
- Typed refresh outcome mapping (recovery success, expired, disabled, protocol inconsistency, unavailable): `refreshOutcome.ts`
- Auth-exempt request classification used by the API client and route gates: `authExempt.ts`
- Proactive refresh cadence, visibility-triggered refresh rules, and mutation-aware passive refresh guard: `refresh.ts`
- Provider-owned state composition, epoch-boundary query/cache purging, cross-tab listener lifecycle, and timer lifecycle: `../AuthContext.tsx`
- Route gates and global blocking surface: `../../app/router/authGates.ts`, `../../app/router/GlobalAccessLayer.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `sessionCoordinator.ts` as the single process-local coordinator. Views and hooks must not build a parallel state machine; they subscribe and read `getPhase`/`getEpoch`.
- Keep refresh singleflight process-local: concurrent protected-management 401s share one refresh flight; each original request replays at most once; late responses are epoch-fenced and never refill caches.
- Keep session epoch rotation strict: any epoch change invalidates React Query and shared reference data at the boundary (`App.tsx`), so auth boundaries never carry last-good snapshots.
- Keep `/v1` and `/v1beta` runtime proxy-key 401s out of management session invalidation; management 401 classification lives in `authExempt.ts` plus the `api/request.ts` classifier.
- Keep the disabled-401 probe singleflight and generation-bound: the probe uses the ordinary `GET /api/models` route through normal auth middleware with an internal purpose only; failures become a generation-bound exhausted incident, never a refresh or redirect loop.
- Keep cross-tab signaling non-secret: `prism.authSessionGeneration` rotates only on new identity or auth-mode change; tabs re-bootstrap through the shared generation fence.
- Keep proactive refresh cadence and visibility-refresh rules centralized in `refresh.ts`; passive refresh must not fire while a login/logout mutation is in flight.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not duplicate bootstrap fallback logic inside route components or page hooks.
- Do not call auth refresh ad hoc from views when the coordinator already owns singleflight, epoch, timer, visibility, and broadcast behavior.
- Do not treat a runtime proxy-key 401 or an unregistered/domain 401 as a session-expiry event.
- Do not swallow auth-mode differences in `AuthContext.tsx`; the coordinator owns the tagged public status union and all blocking phases render `GlobalAccessLayer`.
- Do not keep old authenticated UI visible during blocking phases; protected pages must not show stale data, false zeros, or page-level 401 toasts.
