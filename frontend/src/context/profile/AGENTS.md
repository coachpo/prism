# FRONTEND PROFILE HELPER KNOWLEDGE BASE

## OVERVIEW
`context/profile/` is the helper layer behind `../ProfileContext.tsx`. It owns parallel bootstrap loading, CRUD action orchestration, selected-profile persistence, and deterministic selected-profile fallback rules.

## STRUCTURE
```
profile/
├── bootstrap.ts    # Parallel active-profile + profile-list bootstrap loader
├── actions.ts      # Refresh, create, update, activate, and delete orchestration
├── persistence.ts  # localStorage key and parse/write helpers
└── selection.ts    # Stored-profile → default → active → first fallback rule
```

## WHERE TO LOOK

- Parallel bootstrap of profile list plus active profile, with optional in-flight reuse: `bootstrap.ts`
- CRUD actions, activation conflict refresh, and snapshot re-application after mutations: `actions.ts`
- Persisted selected-profile key `prism.selectedProfileId`: `persistence.ts`
- Selected-profile fallback order for stale or missing persisted ids: `selection.ts`
- Provider-owned state, `revision` bumps, and `setApiProfileId()` wiring: `../ProfileContext.tsx`

## CONVENTIONS

- Keep helper modules snapshot-based. `actions.ts` should refresh and re-apply the latest profile snapshot after mutations.
- Keep selected-profile persistence limited to `persistence.ts` and the provider that calls it.
- Preserve the fallback order in `selection.ts`: stored profile, then default profile, then active profile, then first available profile.
- Keep `expected_active_profile_id` activation conflict handling in `actions.ts` so stale active-profile snapshots are refreshed centrally.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not invent a second localStorage key or alternate selected-profile persistence path.
- Do not mutate profile lists inline in page code when `ProfileContext.tsx` and `actions.ts` already own snapshot updates.
- Do not confuse selected-profile UI scope with active runtime profile activation.
