# BACKEND STARTUP PLATFORM KNOWLEDGE BASE

## OVERVIEW
`platform/startup/` owns backend startup sequencing after bootstrap config load: migrations, canonical seed data, default profile and settings rows, default auth state, proxy-key seed behavior, and secret normalization. It is the bridge between plaintext startup config and PostgreSQL-backed product state.

## STRUCTURE
```text
startup/
├── service.go    # Startup orchestration entrypoint
├── seeds.go      # Database seed rows and default product state
├── profiles.go   # Default profile creation and invariants
├── defaults.go   # Canonical default values
├── crypto.go     # Secret normalization/encryption helpers
└── *_test.go     # Startup service coverage
```

## WHERE TO LOOK
- Startup orchestration and migration handoff: `service.go`
- Canonical product-state seeds: `seeds.go`
- Default profile id `1` invariants: `profiles.go`
- Fresh bootstrap defaults and startup constants: `defaults.go`
- Secret normalization boundaries: `crypto.go`
- Integration coverage: `../../../tests/integration/startup_test.go`, `../../../tests/integration/launcher_startup_contract_test.go`

## CONVENTIONS
- Keep backend canonical defaults aligned with `../config/`: fresh seeds use backend `0.0.0.0:8000`, frontend CORS `5173`, launcher PostgreSQL host port `15432`, CPU-derived pool/admission sizing, transport `100/16/16/300s/90s/0s/10s/1s`, and side-effect timeout `10s`.
- Preserve existing valid bootstrap files. Reset defaults by stopping Prism, removing or relocating the bootstrap file, then restarting.
- Keep startup config file edits restart-applied; do not reintroduce a management API that hot-publishes external bootstrap edits.
- Keep parse-compatible legacy mail and telemetry fields out of runtime behavior.

## ANTI-PATTERNS
- Do not seed long-lived steady-state settings from new environment variables except bootstrap/process wiring such as `DATABASE_URL` or `PRISM_CONFIG_PATH`.
- Do not make startup rewrite existing valid bootstrap files just to refresh defaults.
- Do not bypass migration/schema-history guards with ad hoc table creation.
