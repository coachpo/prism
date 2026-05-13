# FRONTEND AUTH HELPER KNOWLEDGE BASE

## OVERVIEW
`context/auth/` is the helper layer behind `../AuthContext.tsx`. It owns auth bootstrap loading, login/logout mutation wrappers, cross-tab auth broadcasts, and passive or proactive refresh rules for cookie-backed operator auth.

## STRUCTURE
```
auth/
├── bootstrap.ts   # Public-vs-full bootstrap loader and 401 refresh fallback
├── broadcast.ts   # Cross-tab auth refresh channel helpers
├── mutations.ts   # Login/logout mutation wrappers over API callbacks
└── refresh.ts     # Proactive timer interval and passive refresh guards
```

## WHERE TO LOOK

- Public-vs-full bootstrap sequencing, in-flight reuse, `status` gate, and session-to-refresh fallback: `bootstrap.ts`
- Cross-tab auth refresh broadcast helpers used by `AuthContext.tsx`: `broadcast.ts`
- Login and logout mutation wrappers used by `AuthContext.tsx`: `mutations.ts`
- Proactive refresh cadence, visibility-triggered refresh rules, and mutation-aware passive refresh guard: `refresh.ts`
- Provider-owned state composition, cross-tab listener lifecycle, and timer lifecycle: `../AuthContext.tsx`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- Keep `bootstrap.ts` as the only place that decides between `publicBootstrap`, `status`, `session`, and `refresh` during auth initialization.
- Keep `broadcast.ts` as the only auth cross-tab signaling layer.
- Keep `mutations.ts` thin and callback-driven so `AuthContext.tsx` can own state updates while the helpers stay reusable.
- Keep passive refresh mutation-aware. `refresh.ts` should return early while a login/logout mutation is in flight.
- Keep the proactive refresh interval and visibility-refresh rules centralized in `refresh.ts`.

## ANTI-PATTERNS

- Do not duplicate bootstrap fallback logic inside route components or page hooks.
- Do not call auth refresh ad hoc from views when `AuthContext.tsx` already owns timer, visibility, and broadcast behavior.
- Do not swallow auth-mode differences in `AuthContext.tsx`; `bootstrap.ts` owns the public-vs-full split.
