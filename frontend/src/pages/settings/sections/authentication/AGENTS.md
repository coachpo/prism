# FRONTEND SETTINGS AUTHENTICATION CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/authentication/` owns the local authentication setup cluster inside the settings section UI. Keep operator account state and recovery email verification split along the live component boundaries.

## STRUCTURE
```
authentication/
├── AuthenticationStatusCard.tsx
├── AuthenticationSetupGrid.tsx
├── AuthenticationFieldShell.tsx
├── OperatorEmailCard.tsx
├── RecoveryEmailCard.tsx
└── types.ts
```

## WHERE TO LOOK

- Authentication status and setup composition: `AuthenticationStatusCard.tsx`, `AuthenticationSetupGrid.tsx`
- Shared field framing for operator-account and recovery-email inputs: `AuthenticationFieldShell.tsx`
- Operator account username, password, and save controls: `OperatorEmailCard.tsx`
- Recovery email verification and resend flow: `RecoveryEmailCard.tsx`
- Shared auth-section types: `types.ts`

## CONVENTIONS

- Keep operator account and recovery email flows separate in copy and behavior.
- Keep `AuthenticationFieldShell.tsx` as the shared field wrapper for operator-account and recovery-email cards.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not duplicate presentation metadata in multiple auth components.
- Do not introduce costing concerns or shell-state ownership here.
