# FRONTEND FEATURES KNOWLEDGE BASE

## OVERVIEW
`frontend/src/features/` owns the active protected route modules mounted by `src/app/router`, while `src/pages/` remains the oracle-compatible source for legacy page clusters and helper surfaces still reused by feature routes and tests.

## STRUCTURE
```text
features/
├── _contracts/       # Rewrite-route contract matrix fixtures
├── endpoints/        # `/route/endpoints` feature page and endpoint dialogs/hooks
├── loadbalance/      # `/route/ban-policies` feature page and Ban Policy form state
├── models/           # `/models` list feature and `/models/$modelId` detail adapter
├── observe/          # `/observe` dashboard adapter
├── pricing/          # `/route/pricing` feature page and pricing-template flows
├── proxy-keys/       # `/control/proxy-keys` global proxy-key surface
├── request-logs/     # `/observe/requests` list and audit detail adapters
├── settings/         # `/system/settings` feature adapter plus startup leaf
└── sidecars/         # `/control/sidecars` global sidecar control-plane surface
```

## WHERE TO LOOK
- Route mounting, route scopes, legacy redirects, and search schemas: `../app/AGENTS.md`, `../app/router/appRouter.tsx`, `../app/router/rewriteRoutes.ts`
- Feature-owned endpoints, Ban Policy, models, pricing, proxy-key, request-log, settings, observe, and sidecar route components: each feature directory's `*FeaturePage.tsx`
- Startup tab implementation: `settings/startup/AGENTS.md`
- Global sidecar control-plane implementation: `sidecars/AGENTS.md`
- Legacy/oracle page clusters and nested page docs still referenced by feature modules: `../pages/AGENTS.md`
- Typed backend API, shared request plumbing, and selected-profile header rules: `../lib/AGENTS.md`, `../lib/api/AGENTS.md`
- Cross-route query, invalidation, server-validation, table, and design-system helpers: `../shared/AGENTS.md`

## CONVENTIONS
- Keep route modules thin at the boundary: route params/search and feature-local composition belong here; reusable backend contracts stay in `../lib`, and oracle page clusters stay in `../pages` until migrated.
- Keep selected-profile features (`endpoints`, `loadbalance`, `models`, `pricing`, `request-logs`) separate from global control surfaces (`proxy-keys`, `sidecars`) and mixed settings surfaces.
- Keep global control pages free of selected-profile assumptions unless backend route scope explicitly says otherwise.
- Prefer feature-local schema/payload builders beside the feature page when they are only used by that route.

## ANTI-PATTERNS
- Do not add new protected routes without updating `../app/router/rewriteRoutes.ts`, `../app/router/appRouter.tsx`, navigation metadata, tests, and these docs.
- Do not move backend API calls directly into presentational tables when a feature data hook already owns the mutation/fetch flow.
- Do not create leaf AGENTS for every small feature helper; add leaves only for distinct ownership like startup and sidecars.
