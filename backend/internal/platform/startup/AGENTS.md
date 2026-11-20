# Startup Sequencing

- Keep the startup gates before ordinary seeds in the [documented order](../../../../ARCHITECTURE.md#process-lifecycle).
- `profiles.go` preserves Default profile id `1`. `seeds.go` normalizes endpoint secret metadata using `../../endpointdomain/` primitives.
- `observability_upgrade.go` owns the exclusive offline v1 telemetry drain and three-domain scrub backfill. These are synchronous startup steps with durable per-batch checkpoints, not scheduler background work.
- Preserve `000011` finalize guards until v1 drain and all backfill domains are ready; retain raw-shadow null gating. Use the `safediag` scrub/cap boundary, stream-accepted tombstones, and legacy redaction rather than resurrecting raw headers.
- Run settings finalization only after profile/auth seeds; preserve its complete-population validation and no-op behavior for staged migration prefixes without the transition table.
- Do not bypass migration history or upgrade gates with ad hoc table creation. Verify sequencing/preservation changes through `../../../tests/integration/startup_test.go` and launcher-specific changes through `launcher_startup_contract_test.go` in that suite.
