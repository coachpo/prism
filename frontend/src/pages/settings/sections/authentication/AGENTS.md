# FRONTEND SETTINGS AUTHENTICATION CLUSTER KNOWLEDGE BASE

## OVERVIEW
`pages/settings/sections/authentication/` owns the local authentication setup cluster inside the settings section UI. Keep operator account state.

## STRUCTURE
```
authentication/
├── OperatorAccountFields.tsx   # Operator account username, password, and field composition
├── AuthenticationFieldShell.tsx # Shared field framing for operator-account inputs
├── authenticationPassword.ts   # Password bounds and localized validation
└── types.ts                    # Shared auth-section props
```

## WHERE TO LOOK

- Authentication status and setup composition: `../AuthenticationSection.tsx` (the parent section owns the card and the save action)
- Shared field framing for operator-account inputs: `AuthenticationFieldShell.tsx`
- Operator account username and password fields: `OperatorAccountFields.tsx`
- Password bounds and validation: `authenticationPassword.ts`
- Shared auth-section types: `types.ts`
- E2E seam for auth session lifecycle and protected-shell auth behavior: `../../../../../tests/e2e/auth-session-lifecycle.spec.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep `AuthenticationFieldShell.tsx` as the shared field wrapper for operator-account cards.
- Keep password bounds and validation in `authenticationPassword.ts`; do not duplicate them in fields or settings hooks.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not duplicate presentation metadata in multiple auth components.
- Do not introduce costing concerns or shell-state ownership here.
