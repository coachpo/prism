# Project Status: Prism

## Lifecycle

Active development at version 1.0.5. The repository conventions explicitly state that the project is still under development and has no users, so legacy shapes are preserved only when explicitly requested.

## Deployment

Verified self-hosted deployment models:

- Root Docker Compose bundle (one Prism app image plus PostgreSQL, named volumes for data and config).
- Single app image (`docker build .`) with an external PostgreSQL.
- Local launcher (`./start.sh full|headless`) for development.

No production deployment is recorded in this repository.

## Users

No external users. The product targets a single operator (developer or power user) running Prism locally or on a LAN, with optional operator login and optional proxy API keys.

## Data

- PostgreSQL holds management state, request logs, audit logs, usage events, and loadbalance events (partitioned retained tables plus live management tables).
- A plaintext bootstrap file (`config.json`, path set by `PRISM_CONFIG_PATH`) owns startup and runtime transport settings.
- Backing up an instance means `pg_dump` plus a copy of the plaintext config. The database and bootstrap file hold retained product state and are not disposable without backup.

## Compatibility Policy

- Repo convention: when doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; preserve legacy shapes only when explicitly requested.
- Machine contracts (the operation registry, the file-backed bootstrap v1 schema, the partitioned log-retention tables) evolve within their documented ownership and stay covered by backend regression tests.
- Bootstrap v1 is preserve-only for `runtime.secretEncryptionKey`, parse-only for mail compatibility fields, and restart-required for external file edits.

## MVP Fast Validation Mode: Disabled

## Allowed and Prohibited Changes

Allowed:

- Minimal, correct changes within the existing architecture and documented boundaries.
- Clean-architecture-first upgrades per the repo convention.
- Evolution of existing contracts (operation registry, DB lanes, partitioned retention, durable outboxes) within their documented ownership and with the applicable regression coverage.

Prohibited:

- Destructive data resets or data loss without explicit authorization and a verified backup.
- Deleting, moving, or reorganizing existing documents or machine contracts without explicit authorization.
- External deployment, publication, or release actions without explicit confirmation.

## Re-review Triggers

Update this file when lifecycle, deployment, user, or data-policy status changes.
