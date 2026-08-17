# BACKEND PLATFORM CONFIG KNOWLEDGE BASE

## OVERVIEW
`platform/config/` owns Prism's plaintext bootstrap document contract: load, seed defaults, validate, and secret metadata snapshots.

## STRUCTURE
```text
config/
├── bootstrap.go                    # Bootstrap manager lifecycle
├── bootstrap_document.go           # Bootstrap document schema
├── bootstrap_telemetry.go          # Bootstrap telemetry section
├── bootstrap_snapshot.go           # Bootstrap safe snapshot
├── bootstrap_secrets.go            # Bootstrap secret metadata
├── bootstrap_seed.go               # Bootstrap default seed
├── bootstrap_file.go               # Bootstrap file persistence
├── bootstrap_legacy_format.go      # Bootstrap legacy-format rejection
├── bootstrap_field_constraints.go  # Bootstrap field constraints
├── config.go                       # Runtime config loading helpers
└── config_test.go
```

## WHERE TO LOOK
- Bootstrap manager lifecycle: `bootstrap.go`
- Bootstrap document schema and section validation: `bootstrap_document.go`
- Bootstrap telemetry compatibility section: `bootstrap_telemetry.go`
- Bootstrap safe snapshot projection and canonical payload: `bootstrap_snapshot.go`
- Bootstrap secret metadata and masking: `bootstrap_secrets.go`
- Bootstrap default seed: `bootstrap_seed.go`
- Bootstrap file persistence: `bootstrap_file.go`
- Bootstrap legacy-format rejection: `bootstrap_legacy_format.go`
- Bootstrap field constraints: `bootstrap_field_constraints.go`
- Startup/runtime config loading from `PRISM_CONFIG_PATH`, `DATABASE_URL`, and build metadata: `config.go`
- Startup seed/default consumer: `../startup/`

## CONVENTIONS
- Keep steady-state Prism settings in the plaintext bootstrap JSON; avoid adding env knobs except bootstrap-critical process wiring.
- Keep backend canonical defaults here. Fresh seeds inherit these values; existing valid bootstrap files are preserved until manual reset.
- Keep external bootstrap file edits restart-applied after R2.
- Keep the `runtime.transport` section removed: outbound upstream requests carry no connection or timeout limits, and a leftover `runtime.transport` block is rejected with a readable migration error.
- Keep secret metadata snapshots metadata-only; never expose stored secrets, hashes, or proxy-key hashes.
- Keep mail bootstrap fields parse-compatible for live `config.json`; delivery behavior has been removed.

## ANTI-PATTERNS
- Do not make external file edits behave like watched hot state.
- Do not move PostgreSQL-backed profile settings into bootstrap config.
- Do not preserve retired encrypted bootstrap formats as live compatibility layers.
