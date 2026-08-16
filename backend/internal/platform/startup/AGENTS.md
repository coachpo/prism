# BACKEND STARTUP PLATFORM KNOWLEDGE BASE

## OVERVIEW
`platform/startup/` owns backend startup sequencing after bootstrap config load: migrations, canonical seed data, default profile and settings rows, default auth state, proxy-key seed behavior, and secret normalization. It is the bridge between plaintext startup config and PostgreSQL-backed product state.

## STRUCTURE
```text
startup/
├── service.go    # Startup orchestration entrypoint and step sequencing
├── seeds.go      # Database seed rows, default product state, endpoint secret normalization
├── profiles.go   # Default profile creation and invariants
├── defaults.go   # Canonical default values
├── strategies.go             # Canonical loadbalance strategy defaults
├── audit_settings_seed.go    # Per-profile audit settings seed
├── retention_coverage_seed.go # Retention coverage resource seed
├── settings_v2_cutover.go    # Settings v2 cutover step
├── settings_schema_finalizer.go  # Settings schema finalization run under the startup connection
├── observability_v2_upgrade.go   # v2 upgrade state machine (v1_drained → backfill_ready)
├── runtime_telemetry_v1_drain.go # Exclusive offline v1 outbox drain (scrub/cap/split, orphan tombstones)
├── request_audit_v2_backfill.go  # Three-domain backfill owner (request_urls/request_metadata/audit_headers_urls)
└── *_test.go     # Startup service coverage
```

## WHERE TO LOOK
- Startup orchestration and migration handoff: `service.go`
- Canonical product-state seeds: `seeds.go`
- Default profile id `1` invariants: `profiles.go`
- Fresh bootstrap defaults and startup constants: `defaults.go`
- Secret normalization boundaries: `seeds.go` (`normalizeEndpointSecrets`); the encrypt/decrypt/fingerprint primitives it calls live in `../../endpointdomain/`
- Integration coverage: `../../../tests/integration/startup_test.go`, `../../../tests/integration/launcher_startup_contract_test.go`

## CONVENTIONS
- Keep backend canonical defaults aligned with `../config/`: fresh seeds use backend `0.0.0.0:8000`, frontend CORS `5173`, launcher PostgreSQL host port `15432`, CPU-derived pool/admission sizing, transport `100/16/16/300s/90s/0s/10s/1s`, and side-effect timeout `10s`.
- Preserve existing valid bootstrap files. Reset defaults by stopping Prism, removing or relocating the bootstrap file, then restarting.
- Keep startup config file edits restart-applied; do not reintroduce a management API that hot-publishes external bootstrap edits.
- Keep parse-compatible legacy mail and telemetry fields out of runtime behavior.
- Keep the observability v2 upgrade state machine here: v1 drain and three-domain backfill run as startup-owned background steps; `000011` fails closed until `v1_drained` + all domains ready; raw legacy shadows stay null-gated before reads.
- Keep v1 drain scrubbing/capping/splitting with the `safediag` bottom line and never resurrect raw legacy header values (tombstones for stream_accepted, `[REDACTED-LEGACY]` wipe for unrescrubbable rows).

## ANTI-PATTERNS
- Do not seed long-lived steady-state settings from new environment variables except bootstrap/process wiring such as `DATABASE_URL` or `PRISM_CONFIG_PATH`.
- Do not make startup rewrite existing valid bootstrap files just to refresh defaults.
- Do not bypass migration/schema-history guards with ad hoc table creation.
- Do not skip the v2 upgrade gates (v1 drain, domain backfill, raw-shadow null gate) or write v2 rows while legacy shadows are still present.
