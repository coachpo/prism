# FRONTEND FEATURES KNOWLEDGE BASE

## OVERVIEW

`frontend/src/features/` owns the active protected route modules mounted by `src/app/router`, while `src/pages/` remains the oracle-compatible source for legacy page clusters and helper surfaces still reused by feature routes and tests.

## STRUCTURE

```text
features/
├── endpoints/        # `/route/endpoints` feature page and endpoint dialogs/hooks (`endpoints/AGENTS.md`)
├── loadbalance/      # `/route/ban-policies` page with strategy collection, mutation, and impact owners
├── models/           # `/models` list feature, `/models/$modelId` detail adapter, and `models/export/` client-config export owners
├── observe/          # `/observe` dashboard adapter plus routing-health query/context, event, and current-state owners
├── pricing/          # `/route/pricing` feature page with collection/editor/detail/import owners
├── proxy-keys/       # `/system/proxy-keys` global proxy-key surface: ledger, four mutation lanes, secret session, access panel (`proxy-keys/AGENTS.md`)
├── routing-health/    # Observe 路由健康 tab: global current state, events timeline, reset flow
├── runtime-self-test/ # Shared four-layer runtime self-test: effective origin, curl builder, direct runner, dialog
├── request-logs/     # `/observe/requests` list and audit detail adapters
└── settings/         # `/system/settings` feature adapter
```

## WHERE TO LOOK

- Route mounting, route scopes, and search schemas: `../app/AGENTS.md`, `../app/router/appRouter.tsx`, `../app/router/rewriteRoutes.ts`
- Legacy/oracle page clusters and nested page docs still referenced by feature modules: `../pages/AGENTS.md`
- Typed backend API, shared request plumbing, and pinned profile-header rules: `../lib/AGENTS.md`, `../lib/api/AGENTS.md`
- Cross-route query, invalidation, server-validation, table, and design-system helpers: `../shared/AGENTS.md`
- Routing-health event context/page and global current-state owners: `observe/useRoutingHealthQueryContext.ts`, `observe/useRoutingHealthEventPage.ts`, `observe/useRoutingHealthCurrentStateRead.ts`, `observe/useRoutingHealthCurrentStateReset.ts`
- Model-export source/selection, upload review, render lifecycle, and presentation owners: `models/export/useModelExportSource.ts`, `models/export/useModelExportUploadReview.ts`, `models/export/useModelExportRender.ts`, `models/export/ModelExportSelectionPanel.tsx`, `models/export/ModelExportSourcePanel.tsx`, `models/export/ModelExportUploadPanel.tsx`, and `models/export/ModelExportDestinationPanel.tsx`

## CONVENTIONS

- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Keep route modules thin at the boundary: route params/search and feature-local composition belong here; reusable backend contracts stay in `../lib`, and oracle page clusters stay in `../pages` until migrated.
- The `models/export/` surface owns the managed Pi 0.84.3 `prism-pi-models.json` (Pi `models.json` format) / OpenCode 1.18.23 `opencode-prism.json` export. Selection truth comes from the backend source snapshot (first load adopts defaults, refetches intersect user selections, platform switch resets); the operator supplies the Prism origin and provider id, and the credential modes are an omitted key slot or one explicitly included, trimmed final-dialog string (including empty). Never read Prism endpoint keys or derive the client URL from an upstream endpoint. Source/render calls bypass HTTP caches, key-bearing render responses never enter a query cache, and closing results, switching platform, or leaving the route clears content and revokes Blob URLs.
- Uploaded client files are parsed entirely in-browser, matched by the complete public model id, and recursively stripped of sensitive camelCase/snake_case keys before anything uploads; remaining non-sensitive headers require item-by-item confirmation. Full-content copy, fixed-name/MIME download, and a real new-tab raw view reuse exactly one rendered content string. Pi's separate merge-fragment action parses that same string locally and copies only `{ "<provider_id>": { ...provider... } }\n` for insertion beneath an existing `models.json` `providers` object; it never re-renders or implies replacing other providers.
- Keep global control pages free of profile-scope assumptions unless backend route scope explicitly says otherwise.
- Prefer feature-local schema/payload builders beside the feature page when they are only used by that route.
- Keep Ban Policy strategy collection/read, CRUD/default mutations, and impact cursor pagination in their named feature owners; the page hook only composes them.
- Keep pricing-template collection/editor mutations, usage/history detail reads, and import preview/commit in their named feature owners; preserve the two-phase import contract and last-good states.
- Keep proxy-key ledger/query state, create/edit/rotate/delete mutation lifecycles, mutation reconciliation/error mapping, and the one-time raw-secret session in their named owners; mutations must preserve cache invalidation, capacity reconciliation, and no-store secret handling.
- Keep request-log attempt reads and ingress-chain reads in separate owners; the page hook only selects and combines the active view while preserving committed filter metadata for cross-view fallback.
- Keep routing-health signed query context, event paging/detail selection, Current State reads/resets, and presentation in their named owners; `RoutingHealthTab.tsx` only composes them.
- Keep model-export source snapshot/selection reconciliation, uploaded config review/enhancement, and render/key/result lifecycle in their named hooks; `ModelExportPage.tsx` only composes page presentation. `PlatformKeyDialog.tsx` and `ExportResultSheet.tsx` remain the credential-decision and generated-result presentation owners.

## ANTI-PATTERNS

- Do not add new protected routes without updating `../app/router/rewriteRoutes.ts`, `../app/router/appRouter.tsx`, navigation metadata, tests, and these docs.
- Do not move backend API calls directly into presentational tables when a feature data hook already owns the mutation/fetch flow.
