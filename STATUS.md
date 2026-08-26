# Project Status: Prism

## Lifecycle

Active development at version 1.0.24. Development proceeds as continuous iteration driven by the operator's own hands-on use of a running instance: work is prioritized from gaps observed in day-to-day operation rather than from an external roadmap. Upgrade work keeps the clean architecture and takes the best current implementation, so legacy shapes are preserved only when explicitly requested.

Development Tier: MVP

## Deployment

Verified self-hosted deployment models:

- Root Docker Compose bundle (one Prism app image plus PostgreSQL, named volumes for data and config).
- Single app image (`docker build .`) with an external PostgreSQL.
- Local launcher (`./start.sh full|headless`) for development.

Prism runs as a personal deployment on a home LAN and is reached from that network. It is not exposed to the public internet and is not operated as a service for anyone else.

Development and deployment convenience is prioritized over data-security hardening: on this personal home-LAN deployment, the convenient path wins when the two trade off. Consistent with that posture, security controls stay optional and minimal (optional operator auth and proxy API keys, a plaintext bootstrap file, no TLS termination, operator-owned network exposure). The priority concerns security hardening; it does not by itself authorize destructive data resets or dropping the explicit-authorization-plus-backup bar for retained data, which remain governed by the policies below.

## Users

No external users. The single operator is the person running the home-LAN instance, with optional operator login and optional proxy API keys. Product direction comes from that operator's own usage experience.

## Data

- PostgreSQL holds management state, request logs, audit logs, usage events, and loadbalance events (partitioned retained tables plus live management tables).
- A plaintext bootstrap file (`config.json`, path set by `PRISM_CONFIG_PATH`) owns startup settings. The `runtime.transport` config section was removed outright (no compatibility shell) in v1.0.20: Prism no longer applies connection or timeout limits to outbound provider requests, and a leftover `runtime.transport` block fails startup with a readable migration error.
- models.dev catalog metadata is introduced by `000024_model_catalog_metadata.sql` as a purely additive migration: a `model_catalog_bindings` table (one optional management-only metadata row per model), optional `catalog_provider_id`/`catalog_model_id` columns plus a partial unique index on `pricing_templates`, and `revision_source`/`catalog_revision` evidence columns on `pricing_template_revisions`. All retained models, templates, revisions, references, and logs survive unchanged; catalog metadata never enters the runtime snapshot and its writes never invalidate planning.
- The client model-config export (`/route/models/export`, backed by `GET/POST /api/models/exports/{platform}/(source|render)`) ships without any migration, persisted export state, or digest cache. Source reads one consistent snapshot and returns a clock-free `source_digest`; render performs no network I/O, accepts no request-carried models.dev candidate as truth, and recomputes the current catalog-backed and no-enrichment candidates while rechecking current database facts. A digest matching neither returns `export_source_stale`. Both routes and their errors are M3, planning-neutral, and `private, no-store`. The operator supplies the Prism gateway origin/provider id and chooses in the final dialog whether to omit the client key slot or include the trimmed typed string (an explicitly confirmed empty string is valid); export never reads stored upstream endpoint keys. Price truth comes only from current pricing-template revisions under fail-closed gates, including a stable `pricing_component_missing` warning when `reasoning_price` or another required component is null; otherwise the model remains and the whole `cost` group is omitted. The pinned outputs are Pi 0.84.3 `prism-pi-models.json` (Pi `models.json` format) and OpenCode 1.18.23 `opencode-prism.json`.
- Pricing-template typed cards and peak/valley windows are introduced by `000023_pricing_template_kind_cards.sql` as a fresh-only, destructive shape migration. Before DDL, any retained pricing or currency-migration row raises a readable rebuild-required error; the existing instance must be exported or discarded and restarted with an empty database. This does not waive the backup and explicit-authorization requirement for the retained operating history.
- The running home-LAN instance holds real accumulated operating history. Backing up an instance means `pg_dump` plus a copy of the plaintext config. The database and bootstrap file hold retained product state and are not disposable without backup.

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
