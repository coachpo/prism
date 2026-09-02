---
name: prism-ops-inspect
description: Inspect Prism repository and self-hosted deployment state read-only, including release/CI/image identity, Compose topology, container health, PostgreSQL schema and counts, config hashes, backup inventory, capacity, and drift. Use for Prism operations reconnaissance, preflight, audits, or status reports; do not mutate, deploy, back up, restore, or prune.
metadata:
  short-description: Inspect Prism operational state safely
---

# Prism Ops Inspect

## Outcome

Produce a timestamped, secret-safe snapshot and a concise report of current facts, drift, limitations, and the next safe action. This skill never mutates a repository or deployment.

## Inspect

1. Resolve the repository root and read the root `AGENTS.md`, `STATUS.md`, `release.sh`, container contract, and relevant deployment adapter.
2. Read [references/evidence-contract.md](references/evidence-contract.md). Read [references/capy.md](references/capy.md) only for `capy`, `prism-a`, or `prism-b` work.
3. Run `scripts/prism_ops_snapshot.py` with the requested host and services. Use stdout by default; use `--output` only when retained evidence was requested.
4. Treat discovered Compose labels, mounts, image identity, health, migration history, and counts as observation-time truth. Treat adapter values as assertions to verify.

## Autonomy

- Read-only files, Git, logs, SSH inventory, Docker/Compose inspection, health requests, and read-only SQL are in scope without further confirmation.
- Do not execute release, deployment, provider smoke, backup, restore, prune, migration, or service-lifecycle actions. Route authorized mutations to `$prism-release-deploy` or `$prism-backup-restore`.
- Return config and credential evidence only as presence, mode, size, timestamp, or hash. Never expose config contents, environment values, database URLs, credentials, auth material, or provider payloads.

## Completion

- If `--check` fails or sources conflict, stop at diagnosis and identify the exact drift and smallest missing evidence.
- Lead the report with the operational conclusion. Include supporting identities/counts, material caveats, unverified scope, and the next safe action; omit raw command noise and repeated facts.
