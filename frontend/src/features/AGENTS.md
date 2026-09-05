# Protected route features

- Feature route modules compose route/search state, page presentation, and named resource lifecycles. Keep model/domain helpers that are still imported from `../pages/` with their existing owners; the directory split is not a migration requirement.
- Dense resource guidance lives under [models](models/AGENTS.md), [endpoints](endpoints/AGENTS.md), [pricing](pricing/AGENTS.md), and [proxy-keys](proxy-keys/AGENTS.md). Requests and Settings adapters delegate to the corresponding [page-domain guides](../pages/AGENTS.md).
- `observe/` owns dashboard read/projection helpers. Trend/Errors share URL-backed attribution scope; Activity uses ingress independently, and Terminal Target drill-down explicitly chooses final execution or route attempts. Preserve signed Requests replay filters and scoped coverage on drill-down/export.
- `routing-health/` owns the independent `/observe/routing-health` route. Its event paging, signed query context, current-state read/reset, and presentation reuse the named owners under `observe/`; current state must not inherit event-window filters.
- `loadbalance/` keeps collection/read, CRUD/default mutations, and impact cursor pagination in `loadbalance/useBanPolicyStrategyCollection.ts`, `loadbalance/useBanPolicyMutations.ts`, and `loadbalance/useStrategyImpactPager.ts`. Page composition must not absorb those lifecycles.
- `runtime-self-test/` owns effective Prism origin, curl construction, and direct runtime execution. Its selectors offer only enabled direct-entry models, and runtime 401s stay outside management auth recovery.
