# FRONTEND PROFILE HELPER KNOWLEDGE BASE

## OVERVIEW
`context/profile/` is the helper layer behind `../ProfileContext.tsx`. It owns profile bootstrap snapshots, CRUD action orchestration, selected-profile persistence, and deterministic selected-profile fallback rules.

## STRUCTURE
```
profile/
├── bootstrap.ts    # `profiles.bootstrap()` snapshot loader and optional in-flight reuse
├── actions.ts      # Refresh, create, update, activate, and delete orchestration
├── persistence.ts  # localStorage key and parse/write helpers
└── selection.ts    # Stored-profile -> active -> default -> first fallback rule
```

## WHERE TO LOOK

- Profile bootstrap snapshot loading and in-flight reuse: `bootstrap.ts`
- CRUD actions, activation conflict refresh, and snapshot re-application after mutations: `actions.ts`
- Persisted selected-profile key `prism.selectedProfileId`: `persistence.ts`
- Selected-profile fallback order for stale or missing persisted ids: `selection.ts`
- Provider-owned state, `revision` bumps, and `setApiProfileId()` wiring: `../ProfileContext.tsx`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep helper modules snapshot-based. `actions.ts` should refresh and re-apply the latest profile snapshot after mutations.
- Keep selected-profile persistence limited to `persistence.ts` and the provider that calls it.
- Preserve the fallback order in `selection.ts`: stored profile, then active profile, then default profile, then first available profile.
- Keep `expected_active_profile_id` activation conflict handling in `actions.ts` so stale active-profile snapshots are refreshed centrally.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS

- Do not invent a second localStorage key or alternate selected-profile persistence path.
- Do not mutate profile lists inline in page code when `ProfileContext.tsx` and `actions.ts` already own snapshot updates.
- Do not confuse selected-profile UI scope with active runtime profile activation.
