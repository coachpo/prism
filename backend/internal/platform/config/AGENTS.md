# Bootstrap Configuration

- Defaults affect fresh seeds only. External edits take effect after restart through the startup snapshot, never a file watcher or management hot-publish path.
- Keep `runtime.sideEffects.attemptTimeout` required and `runtime.secretEncryptionKey` preserve-only. Secret snapshots expose metadata, not values, hashes, or proxy-key hashes.
- Reject the removed `runtime.transport` section with the migration error in the parser; upstream connection/time limits are not configurable here.
- Reject retired encrypted bootstrap formats via `bootstrap_legacy_format.go`.
- Coordinate default/schema changes with `config_test.go` and startup/launcher integration tests; canonical defaults must remain shared by fresh bootstrap paths.
