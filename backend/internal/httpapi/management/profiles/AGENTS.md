# BACKEND MANAGEMENT PROFILES KNOWLEDGE BASE

## OVERVIEW
`management/profiles/` owns profile lifecycle management under `/api/profiles*`. It lists profiles, reports the active/bootstrap profile state, creates and updates editable profiles, activates a profile with conflict checks, and soft-deletes inactive non-default profiles.

## STRUCTURE
```text
profiles/
├── service.go    # Service construction and profile route mounting
├── routes.go     # List, active, bootstrap, create, update, activate, delete
└── types.go      # Profile request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- List, active, and bootstrap responses: `routes.go`.
- Create, update, activate, and delete flows: `routes.go`.
- Shared profile invariants and limits: `profiledomain`.

## CONVENTIONS
- Keep selected-profile headers separate from active runtime profile routing.
- Keep default profile guardrails intact: locked names and no delete.
- Activation must check the expected active profile before switching.
- Profile deletion is soft delete and only allowed for inactive non-default profiles.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- Profile lifecycle changes should preserve management/runtime separation across all LLM operation shapes; `X-Profile-Id` remains management-only.

## ANTI-PATTERNS
- Do not let selected-profile headers change runtime proxy routing.
- Do not delete or rename the locked default profile outside supported guardrails.
- Do not activate a profile without checking the expected active profile ID.
- Do not hard-delete profiles from management routes.
