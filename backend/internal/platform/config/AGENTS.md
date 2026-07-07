# BACKEND PLATFORM CONFIG KNOWLEDGE BASE

## OVERVIEW
`platform/config/` owns Prism's plaintext bootstrap document contract: load, seed defaults, validate, restart-applied field classification, and secret metadata snapshots.

## STRUCTURE
```text
config/
├── bootstrap.go             # Bootstrap document types, defaults, load/save helpers
├── bootstrap_apply.go       # Startup field classification and diff helpers
├── config.go                # Runtime config loading helpers
├── bootstrap_apply_test.go
└── config_test.go
```

## WHERE TO LOOK
- Bootstrap schema, canonical defaults, secret metadata, and file persistence: `bootstrap.go`
- Startup field classification and diff helpers: `bootstrap_apply.go`
- Startup/runtime config loading from `PRISM_CONFIG_PATH`, `DATABASE_URL`, and build metadata: `config.go`
- HTTP hot runtime publisher: `../http/AGENTS.md`
- Startup seed/default consumer: `../startup/`

## CONVENTIONS
- Keep steady-state Prism settings in the plaintext bootstrap JSON; avoid adding env knobs except bootstrap-critical process wiring.
- Keep backend canonical defaults here. Fresh seeds inherit these values; existing valid bootstrap files are preserved until manual reset.
- Keep `runtime.transport.requestTimeout` in the startup runtime transport snapshot.
- Keep external bootstrap file edits restart-applied after R2.
- Keep secret metadata snapshots metadata-only; never expose stored secrets, hashes, or proxy-key hashes.
- Keep mail bootstrap fields parse-compatible for live `config.json`; delivery behavior has been removed.

## ANTI-PATTERNS
- Do not make external file edits behave like watched hot state.
- Do not move PostgreSQL-backed profile settings into bootstrap config.
- Do not preserve retired encrypted bootstrap formats as live compatibility layers.
