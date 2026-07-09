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
├── settings/         # `/system/settings` feature adapter
```

## WHERE TO LOOK
- Route mounting, route scopes, and search schemas: `../app/AGENTS.md`, `../app/router/appRouter.tsx`, `../app/router/rewriteRoutes.ts`
- Legacy/oracle page clusters and nested page docs still referenced by feature modules: `../pages/AGENTS.md`
- Typed backend API, shared request plumbing, and pinned profile-header rules: `../lib/AGENTS.md`, `../lib/api/AGENTS.md`
- Cross-route query, invalidation, server-validation, table, and design-system helpers: `../shared/AGENTS.md`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep route modules thin at the boundary: route params/search and feature-local composition belong here; reusable backend contracts stay in `../lib`, and oracle page clusters stay in `../pages` until migrated.
- Keep global control pages free of profile-scope assumptions unless backend route scope explicitly says otherwise.
- Prefer feature-local schema/payload builders beside the feature page when they are only used by that route.

## ANTI-PATTERNS
- Do not add new protected routes without updating `../app/router/rewriteRoutes.ts`, `../app/router/appRouter.tsx`, navigation metadata, tests, and these docs.
- Do not move backend API calls directly into presentational tables when a feature data hook already owns the mutation/fetch flow.
