# FRONTEND SETTINGS AUTHENTICATION CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/authentication/` owns the local authentication setup cluster inside the settings section UI. Keep operator account state, recovery email verification, passkey ceremony, and presentation metadata split along the live component boundaries.

## STRUCTURE
```
authentication/
├── AuthenticationStatusCard.tsx
├── AuthenticationSetupGrid.tsx
├── AuthenticationFieldShell.tsx
├── OperatorEmailCard.tsx
├── RecoveryEmailCard.tsx
├── PasskeysCard.tsx
├── passkeyMetadata.ts
├── usePasskeyManagement.ts
├── types.ts
└── __tests__/                # Local auth-section coverage
```

## WHERE TO LOOK

- Authentication status and setup composition: `AuthenticationStatusCard.tsx`, `AuthenticationSetupGrid.tsx`
- Shared field framing for operator-account and recovery-email inputs: `AuthenticationFieldShell.tsx`
- Operator account username, password, and save controls: `OperatorEmailCard.tsx`
- Recovery email verification and resend flow: `RecoveryEmailCard.tsx`
- Passkey ceremony, list, register, and remove orchestration: `PasskeysCard.tsx`, `usePasskeyManagement.ts`
- Presentation metadata and small shared types: `passkeyMetadata.ts`, `types.ts`
- Local auth-section tests: `__tests__/`

## CONVENTIONS

- Keep operator account, recovery email, and passkey flows separate in copy and behavior.
- Keep `AuthenticationFieldShell.tsx` as the shared field wrapper for operator-account and recovery-email cards.
- Let `usePasskeyManagement.ts` own the ceremony and mutation orchestration, while `PasskeysCard.tsx` owns the UI and dialogs.
- Keep `passkeyMetadata.ts` limited to display metadata and labels.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not move passkey ceremony orchestration into the card UI.
- Do not duplicate presentation metadata in multiple auth components.
- Do not introduce costing concerns or shell-state ownership here.
