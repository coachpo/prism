# Application wiring

- Keep `router/appRouter.tsx` and `router/rewriteRoutes.ts` aligned for mounted routes, search schemas, and scope metadata. Protected routes mount `ReportingCurrencyProvider` and the `Page` shell only after auth gating; public login remains outside that shell.
- Use `router/authGates.ts` for redirects and `router/GlobalAccessLayer.tsx` for blocking auth phases. Preserve return search/hash state instead of redirecting from feature components.
- `/observe/routing-health` owns routing-health events/current state. The old `/observe?tab=events` link redirects there with its event/runtime filters; dashboard chart parameters are discarded by `pickRoutingHealthSearch`.
- Model configuration/detail/export use `/route/models`, `/route/models/$modelId`, and `/route/models/export`; `/models` and `/models/$modelId` are redirects. Keep entity remount keyed to `modelId` so drafts cannot survive under a different model URL.
- Keep feature modules lazy-loaded at this boundary. Resource queries and form state belong to their feature/domain owners.
- `providers/queryClient.ts` disables query/mutation retries and uses zero query stale time. `forms/rewriteProfileScopeForm.ts` accepts only Default profile id `1`.

Route/gate behavior is covered by `../test/route-helpers.test.ts`, `../test/rewrite-harness.test.tsx`, and `../../tests/lib/profile_scope_header_contract.test.mjs`.
