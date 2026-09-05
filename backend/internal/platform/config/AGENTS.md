# Bootstrap Configuration

`bootstrap_document.go` owns the plaintext document schema, `config.go` canonical defaults/loading, `bootstrap_seed.go` missing-file seeds, and `bootstrap_file.go` persistence. Safe projections live in `bootstrap_snapshot.go` and `bootstrap_secrets.go`.

- Keep steady-state startup settings in the selected plaintext JSON and PostgreSQL-backed profile settings out of this package. Environment overrides remain bootstrap/process inputs.
- Preserve existing valid files; defaults affect fresh seeds only. External edits take effect after restart through the startup snapshot, never a file watcher or management hot-publish path.
- Keep `runtime.sideEffects.attemptTimeout` required and `runtime.secretEncryptionKey` preserve-only. Secret snapshots expose metadata, not values, hashes, or proxy-key hashes.
- Reject the removed `runtime.transport` section with the migration error in the parser; upstream connection/time limits are not configurable here.
- Retained mail and telemetry fields are parse-compatible data, not delivery/runtime behavior. Reject retired encrypted bootstrap formats via `bootstrap_legacy_format.go`.
- Coordinate default/schema changes with `config_test.go` and startup/launcher integration tests; canonical defaults must remain shared by fresh bootstrap paths.
