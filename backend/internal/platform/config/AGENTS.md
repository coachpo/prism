# BACKEND PLATFORM CONFIG KNOWLEDGE BASE

## OVERVIEW
`platform/config/` owns Prism's plaintext bootstrap document contract: load, seed defaults, validate, hot-apply planning, safe secret metadata, and update classification.

## STRUCTURE
```text
config/
├── bootstrap.go             # Bootstrap document types, defaults, load/save helpers
├── bootstrap_apply.go       # Hot-apply planning and restart-required classification
├── config.go                # Runtime config loading helpers
├── bootstrap_apply_test.go
├── bootstrap_management_test.go
└── config_test.go
```

## WHERE TO LOOK
- Bootstrap schema, canonical defaults, secret metadata, and file persistence: `bootstrap.go`
- Hot-eligible versus restart-required fields and apply-result shaping: `bootstrap_apply.go`
- Startup/runtime config loading from `PRISM_CONFIG_PATH`, `DATABASE_URL`, and build metadata: `config.go`
- Management API consumer: `../../httpapi/management/bootstrapconfig/AGENTS.md`
- HTTP hot runtime publisher: `../http/AGENTS.md`
- Startup seed/default consumer: `../startup/`

## CONVENTIONS
- Keep steady-state Prism settings in the plaintext bootstrap JSON; avoid adding env knobs except bootstrap-critical process wiring.
- Keep backend canonical defaults here. Fresh seeds inherit these values; existing valid bootstrap files are preserved until manual reset.
- Keep `runtime.transport.requestTimeout` hot-applicable through the runtime transport snapshot.
- Keep listener, database URL, pool budgets, `runtime.sideEffects.attemptTimeout`, runtime secret encryption key, and JWT signing key restart-required.
- Keep safe secret responses metadata-only; never expose stored secrets, hashes, reset codes, verification tokens, or proxy-key hashes.
- Keep enabled SMTP fail-fast. Invalid enabled mail config must not fall back to no-op delivery.

## ANTI-PATTERNS
- Do not make external file edits behave like watched hot state.
- Do not move PostgreSQL-backed profile settings into bootstrap config.
- Do not preserve retired encrypted bootstrap formats as live compatibility layers.
