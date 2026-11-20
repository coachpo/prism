# Protected route features

- In `observe/`, Trend/Errors share URL-backed attribution scope; Activity uses ingress independently, and Terminal Target drill-down explicitly chooses final execution or route attempts. Preserve signed Requests replay filters and scoped coverage on drill-down/export.
- `routing-health/` event paging, signed query context, current-state read/reset, and presentation reuse the named owners under `observe/`; current state must not inherit event-window filters.
- `loadbalance/` keeps collection/read, CRUD/default mutations, and impact cursor pagination in `loadbalance/useBanPolicyStrategyCollection.ts`, `loadbalance/useBanPolicyMutations.ts`, and `loadbalance/useStrategyImpactPager.ts`. Page composition must not absorb those lifecycles.
- `runtime-self-test/` selectors offer only enabled direct-entry models, and runtime 401s stay outside management auth recovery.
