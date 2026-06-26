# FRONTEND APP KNOWLEDGE BASE

## OVERVIEW
`frontend/src/app/` owns the browser application shell wiring that sits above route features: TanStack router construction, legacy-route redirects, auth/public gates, provider-safe route suspense, rewrite route metadata, and shared QueryClient defaults.

## STRUCTURE
```text
app/
├── forms/       # Rewrite-era shared form schemas
├── providers/   # React Query client factory
├── router/      # TanStack route tree, auth gates, route ids, search schemas, redirects
└── index.ts     # Public app-layer exports used by tests and App.tsx
```

## WHERE TO LOOK
- Top-level app composition: `../App.tsx`, which creates the router/query client and wraps `BrowserRouter`, `RoutedAuthProvider`, and `RouterProvider`
- Current mounted route tree: `router/appRouter.tsx`
- Static route ids, route scopes, search schemas, legacy redirect map, and path builders: `router/rewriteRoutes.ts`
- Public/protected auth redirect rules and return-state preservation: `router/authGates.ts`
- React Query defaults for rewrite routes and tests: `providers/queryClient.ts`
- Rewrite profile-scope form schema and options: `forms/rewriteProfileScopeForm.ts`
- Route contract tests and rewrite harness coverage: `../test/route-helpers.test.ts`, `../test/rewrite-harness.test.tsx`, `../../tests/lib/profile_scope_header_contract.test.mjs`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Treat `router/appRouter.tsx` and `router/rewriteRoutes.ts` as the route source of truth; frontend docs should not invent routes outside those files.
- Keep public auth routes under `/auth/*`; legacy `/login`, `/forgot-password`, and `/reset-password` redirect through the compatibility map.
- Keep protected route wrappers responsible for auth gating plus `ProfileProvider`, `ReportingCurrencyProvider`, and `Page` shell handoff.
- Keep feature route modules lazy-loaded from `../features/` and legacy/oracle page clusters behind those features where still needed.
- Keep query defaults deterministic for tests: no React Query retries and zero stale time unless this factory changes intentionally.

## ANTI-PATTERNS
- Do not put feature data fetching or UI state in `src/app/`; route modules own first handoff, and shared API/cache helpers live in `src/lib` or `src/shared`.
- Do not add new legacy route names without updating `rewriteCompatibilityRoutePaths`, `legacyRouteRedirects`, tests, and frontend docs.
- Do not bypass the auth gate helpers with ad hoc redirect code in feature pages.
