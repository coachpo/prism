# FRONTEND CONTEXT KNOWLEDGE BASE

## OVERVIEW
`src/context/` owns Prism's auth bootstrap, management-profile scoping, and reporting-currency provider state. Keep the selected-profile versus active-runtime split explicit here, because this layer feeds `X-Profile-Id` for management calls and gates selected-profile keyed reporting-currency state without touching proxy routing.

## STRUCTURE
```
context/
├── AuthContext.tsx              # Provider wiring over auth bootstrap, mutations, and refresh helpers
├── auth-context.ts              # Shared context type and createContext() export
├── useAuth.ts                   # Guard hook for auth consumers
├── auth/                        # Bootstrap, mutation, and passive/proactive refresh helpers
├── auth/AGENTS.md               # Helper-layer auth bootstrap, mutation, and refresh ownership
├── ProfileContext.tsx           # Provider wiring over profile bootstrap, actions, persistence, and selection
├── profile/                     # Bootstrap, actions, persistence, and selection helpers
├── profile/AGENTS.md            # Helper-layer profile bootstrap, persistence, selection, and CRUD ownership
└── ReportingCurrencyContext.tsx # Provider over selected-profile keyed reporting-currency cache, readiness, and refresh
```

## WHERE TO LOOK

- Auth bootstrap mode selection, in-flight reuse, proactive refresh timer, and visibility-triggered refresh: `AuthContext.tsx`
- Auth bootstrap, mutation, and refresh helpers: `auth/AGENTS.md`
- Auth context type/export split and guarded hook: `auth-context.ts`, `useAuth.ts`
- Selected-profile persistence, active-profile sync, `setApiProfileId()` updates, and revision triggers: `ProfileContext.tsx`
- Profile bootstrap, CRUD actions, local-storage persistence, and selected-profile resolution: `profile/AGENTS.md`
- Reporting-currency readiness, selected-profile keyed cache handoff, `prime()` and `refresh()` behavior, default fallback currency, and `useReportingCurrencyContext()`: `ReportingCurrencyContext.tsx`, `../lib/reportingCurrency.ts`

## CHILD DOCS

- `auth/AGENTS.md`: helper-layer auth bootstrap flow, login/logout mutation helpers, and passive/proactive refresh rules.
- `profile/AGENTS.md`: helper-layer profile bootstrap, local-storage persistence, selection fallback, and CRUD action orchestration.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- Use `useAuth()`, `useProfileContext()`, and `useReportingCurrencyContext()` instead of consuming context objects directly.
- Keep auth bootstrap async and reuse in-flight work instead of duplicating fetches.
- Keep `selectedProfile` and `activeProfile` distinct in UI and docs. `selectedProfile` scopes management APIs; it does not switch proxy traffic.
- Treat `ProfileContext.revision` as the shared invalidation signal when selected scope changes.
- Keep reporting-currency readiness keyed to `selectedProfileId`, with fallback and `prime()`/`refresh()` ownership in `ReportingCurrencyContext.tsx` and cache and normalization in `../lib/reportingCurrency.ts`.
- Keep bootstrap and helper logic in `auth/` and `profile/`, with the provider files focused on composition and exposed state.
- Let the child AGENTS files own helper-layer detail so this parent stays provider-focused.

## ANTI-PATTERNS

- Do not invent local profile state in pages.
- Do not inject `X-Profile-Id` from pages or hooks.
- Do not assume `selectedProfile` affects proxy traffic.
- Do not fetch, normalize, or cache reporting currency directly in page hooks when `ReportingCurrencyContext.tsx` and `../lib/reportingCurrency.ts` already own that seam.
- Do not bypass the `auth/` or `profile/` helper modules with duplicate bootstrap, persistence, or selection logic.
