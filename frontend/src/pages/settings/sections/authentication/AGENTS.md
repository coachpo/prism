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

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep operator account and recovery email flows separate in copy and behavior.
- Keep `AuthenticationFieldShell.tsx` as the shared field wrapper for operator-account and recovery-email cards.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS

- Do not duplicate presentation metadata in multiple auth components.
- Do not introduce costing concerns or shell-state ownership here.
