# Startup Sequencing

`service.go` owns the ordered startup steps, bridging the loaded bootstrap configuration and PostgreSQL state. Bootstrap file/default ownership remains in [config](../config/AGENTS.md).

- Keep migration, pricing-transition, observability-upgrade, OpenAI text-mode check, and retention-cutover gates before ordinary seeds. `../../openaimodecheck/` provides the same read-only equality check used by upgrade preflight.
- `profiles.go` preserves Default profile id `1`. Per-resource seed files own product defaults; `seeds.go` normalizes endpoint secret metadata using `../../endpointdomain/` primitives.
- `observability_upgrade.go` owns the exclusive offline v1 telemetry drain and three-domain scrub backfill. These are synchronous startup steps with durable per-batch checkpoints, not scheduler background work.
- Preserve `000011` finalize guards until v1 drain and all backfill domains are ready; retain raw-shadow null gating. Use the `safediag` scrub/cap boundary, stream-accepted tombstones, and legacy redaction rather than resurrecting raw headers.
- Run settings finalization only after profile/auth seeds; preserve its complete-population validation and no-op behavior for staged migration prefixes without the transition table.
- Do not bypass migration history or upgrade gates with ad hoc table creation. Verify sequencing/preservation changes through `../../../tests/integration/startup_test.go` and launcher-specific changes through `launcher_startup_contract_test.go` in that suite.
