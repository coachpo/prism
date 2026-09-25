# Project Status: Prism

## Lifecycle

Active development at version 1.1.20. The operator's day-to-day use of the running home-LAN instances drives priorities. Upgrade work prefers clean architecture and the best current implementation; legacy shapes are preserved only when explicitly requested.

Development Tier: MVP

The existing tier remains the development default. The retained-data and authorization policies below remain binding; the tier does not make either deployed instance disposable. [CONTRIBUTING.md](CONTRIBUTING.md#current-development-strategy) owns the selected static strategy and its transition conditions.

## Deployment

Prism is a personal home-LAN deployment, not a public internet service. Development and deployment convenience takes priority over security hardening; optional operator login and proxy keys, plaintext bootstrap ownership, and operator-managed network exposure remain the established posture. This does not relax the retained-data policy.

Both instances were deployed and verified on 2026-09-25 at 00:01 UTC through the `capy` SSH adapter, selected Docker/Compose fields, PostgreSQL metadata, and the health, Requests, `/v1/models`, and loadbalance strategy list and preview APIs:

| Instance | Dashboard | App / PostgreSQL health | App version | Latest migration |
| --- | --- | --- | --- | --- |
| `prism-a` | [Observe A](http://192.168.1.222:8087/observe?tab=overview&metric=requests&scope=ingress&group_by=none&interval=auto) | healthy / healthy | 1.1.20 | `000035_request_scoped_reroute` |
| `prism-b` | [Observe B](http://192.168.1.222:8088/observe?tab=overview&metric=requests&scope=ingress&group_by=none&interval=auto) | healthy / healthy | 1.1.20 | `000035_request_scoped_reroute` |

Both run PostgreSQL 16.15 and the same app image, `ghcr.io/coachpo/prism:v1.1.20@sha256:a6a5d46a7401ced2c421a2e9db5de90c57077e34dc34007ff30eb4dda39852b6`, pinned in each instance's Compose env file, with independent databases and bind-mounted plaintext bootstrap files. Only the app containers were recreated; PostgreSQL kept running. Both app restart counters were zero at observation. Quiesced PostgreSQL and matching bootstrap backups were verified before each update; schema history, entity counts, and config hashes were preserved. Each instance passed a 300-second observation with 30 samples. No real-provider smoke was performed. Migration `000035_request_scoped_reroute` was applied by v1.1.18, deployed earlier that day and superseded by v1.1.20; each upgrade kept the preceding schema history as a prefix. Tags `v1.1.16` and `v1.1.19` have no published image and were never deployed. These are observation-time facts, not continuous health guarantees.

The repository supports the root Compose app-plus-PostgreSQL bundle, the single app image with an external PostgreSQL, and the local `start.sh` launcher. [README.md](README.md#quick-start) owns ordinary startup; [architecture.md](docs/architecture.md#23-local-tooling-and-build-workflow) owns the packaging model.

## Users

No external users. One operator uses the two home-LAN instances, with optional operator login and proxy API keys. Product direction comes from that operator's own usage experience.

## Data

- Both instances contain retained PostgreSQL management state and operating history, plus a plaintext bootstrap `config.json`. They are not disposable workspaces. Recovery requires a PostgreSQL backup and the matching bootstrap file; an inventory listing alone does not verify restorability.
- Startup applies the checked-in migration sequence and its guards. An old database is not assumed compatible merely because the current version starts on a fresh database. In particular, `000023_pricing_template_kind_cards.sql` refuses retained old pricing/currency-migration rows before its destructive shape change; any rebuild requires explicit authorization and a verified backup.
- Later catalog bindings, Pi bind-time identity, output-rate evidence, Terminal Target upstream identity, direct-entry qualification, and request-scoped reroute status codes have data-preserving migration contracts. Historical gaps remain explicit rather than being fabricated from current configuration. The detailed schemas and evidence semantics belong in [architecture.md](docs/architecture.md#15-data-model-reference).
- The tracked [direct-entry reclassification runbook](docs/operations/direct-request-entry-reclassification.md), its `scripts/operations/direct-request-entry-reclassification.sql` companion, and the integration acceptance test ship in source. The procedure is operator-invoked only and never runs during startup; its existence or the 000032 migration stamp does not prove that it has been applied to an instance.
- Bootstrap settings are file-backed and restart-applied. Valid existing files are preserved. The removed `runtime.transport` block is rejected; mail and exporter telemetry fields are parse-only compatibility inputs. The full startup contract is in [architecture.md](docs/architecture.md#10a-startup-config-file).

## Compatibility Policy

- Repo convention: when doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; preserve legacy shapes only when explicitly requested.
- Machine contracts (the operation registry, the file-backed bootstrap v1 schema, the partitioned log-retention tables) evolve within their documented ownership and stay covered by backend regression tests.
- Bootstrap v1 is preserve-only for `runtime.secretEncryptionKey`, parse-only for mail compatibility fields, and restart-required for external file edits.
- The no-shim convention governs code, API, and schema shape. It does not make the running instance's data disposable: a shape change that cannot carry existing data forward needs an explicit decision and a verified backup first.

## Allowed and Prohibited Changes

Allowed:

- Minimal, correct changes within the existing architecture and documented boundaries.
- Clean-architecture-first upgrades per the repo convention, replacing legacy shapes rather than wrapping them in compatibility layers.
- Evolution of existing contracts (operation registry, DB lanes, partitioned retention, durable outboxes) within their documented ownership and with the applicable regression coverage.

Prohibited:

- Destructive data resets or data loss without explicit authorization and a verified backup.
- Deleting, moving, or reorganizing existing documents or machine contracts without explicit authorization.
- External deployment, publication, or release actions without explicit confirmation.

## Re-review Triggers

Update this file when lifecycle, deployment, user, or data-policy status changes, and when a release changes the version recorded above.
