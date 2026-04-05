# FRONTEND CONTEXT KNOWLEDGE BASE

## OVERVIEW
`src/context/` owns Prism's global auth bootstrap and management-profile scoping. Keep the selected-profile versus active-runtime split explicit here, because this is the layer that feeds `X-Profile-Id` for management calls without touching proxy routing.

## STRUCTURE
```
context/
├── AuthContext.tsx    # Provider wiring over auth bootstrap, mutations, and refresh helpers
├── auth-context.ts    # Shared context type and createContext() export
├── useAuth.ts         # Guard hook for auth consumers
├── auth/              # Bootstrap, mutation, and passive/proactive refresh helpers
├── auth/AGENTS.md     # Helper-layer auth bootstrap, mutation, and refresh ownership
├── ProfileContext.tsx # Provider wiring over profile bootstrap, actions, persistence, and selection
├── profile/           # Bootstrap, actions, persistence, and selection helpers
└── profile/AGENTS.md  # Helper-layer profile bootstrap, persistence, selection, and CRUD ownership
```

## WHERE TO LOOK

- Auth bootstrap mode selection, in-flight reuse, proactive refresh timer, and visibility-triggered refresh: `AuthContext.tsx`
- Auth bootstrap, mutation, and refresh helpers: `auth/AGENTS.md`
- Auth context type/export split and guarded hook: `auth-context.ts`, `useAuth.ts`
- Selected-profile persistence, active-profile sync, `setApiProfileId()` updates, and revision triggers: `ProfileContext.tsx`
- Profile bootstrap, CRUD actions, local-storage persistence, and selected-profile resolution: `profile/AGENTS.md`

## CHILD DOCS

- `auth/AGENTS.md`: helper-layer auth bootstrap flow, login/logout mutation helpers, and passive/proactive refresh rules.
- `profile/AGENTS.md`: helper-layer profile bootstrap, local-storage persistence, selection fallback, and CRUD action orchestration.

## CONVENTIONS

- Use `useAuth()` and `useProfileContext()` instead of consuming contexts directly.
- Keep auth bootstrap async and reuse in-flight work instead of duplicating fetches.
- Keep `selectedProfile` and `activeProfile` distinct in UI and docs. `selectedProfile` scopes management APIs, it does not switch proxy traffic.
- Treat `ProfileContext.revision` as the shared invalidation signal when selected scope changes.
- Keep bootstrap and helper logic in `auth/` and `profile/`, with the provider files focused on composition and exposed state.
- Let the child AGENTS files own helper-layer detail so this parent stays provider-focused.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not invent local profile state in pages.
- Do not inject `X-Profile-Id` from pages or hooks.
- Do not assume `selectedProfile` affects proxy traffic.
- Do not bypass the `auth/` or `profile/` helper modules with duplicate bootstrap, persistence, or selection logic.
