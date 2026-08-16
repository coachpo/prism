# Project Status: Prism

## Lifecycle

Active development at version 1.0.16. Development proceeds as continuous iteration driven by the operator's own hands-on use of a running instance: work is prioritized from gaps observed in day-to-day operation rather than from an external roadmap. Upgrade work keeps the clean architecture and takes the best current implementation, so legacy shapes are preserved only when explicitly requested.

## Deployment

Verified self-hosted deployment models:

- Root Docker Compose bundle (one Prism app image plus PostgreSQL, named volumes for data and config).
- Single app image (`docker build .`) with an external PostgreSQL.
- Local launcher (`./start.sh full|headless`) for development.

Prism runs as a personal deployment on a home LAN and is reached from that network. It is not exposed to the public internet and is not operated as a service for anyone else.

## Users

No external users. The single operator is the person running the home-LAN instance, with optional operator login and optional proxy API keys. Product direction comes from that operator's own usage experience.

## Data

- PostgreSQL holds management state, request logs, audit logs, usage events, and loadbalance events (partitioned retained tables plus live management tables).
- A plaintext bootstrap file (`config.json`, path set by `PRISM_CONFIG_PATH`) owns startup and runtime transport settings.
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
