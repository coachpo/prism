---
name: prism-ops-inspect
description: Inspect Prism repository and self-hosted deployment state read-only, including release/CI/image identity, Compose topology, container health, PostgreSQL schema and counts, config hashes, backup inventory, capacity, and drift. Use for Prism operations reconnaissance, preflight, audits, or status reports; do not mutate, deploy, back up, restore, or prune.
metadata:
  short-description: Inspect Prism operational state safely
---

# Prism Ops Inspect

Collect a bounded, secret-safe snapshot before making operational claims or proposing mutations.

## Workflow

1. Resolve the repository root and read the root `AGENTS.md`, `STATUS.md`, `release.sh`, container contract, and relevant deployment adapter.
2. Run `scripts/prism_ops_snapshot.py` with the requested host and services. Default to stdout JSON; write `--output` only when the user requested retained evidence.
3. Treat discovered Compose labels, mounts, image references, health, migration history, and counts as authoritative for the observation time. Treat adapter values as expectations to verify, never as live truth.
4. Report secrets as presence/hash metadata only. Never read or return config content, environment values, database URLs, keys, tokens, credentials, or provider response bodies.
5. If `--check` fails or evidence conflicts, return the drift and the smallest missing evidence. Do not repair it under this skill.

Read [references/evidence-contract.md](references/evidence-contract.md) for the output and redaction contract. For the home-LAN environment, also read [references/capy.md](references/capy.md).

## Boundaries

- Read-only shell, Git, SSH, Docker inspect, Compose inventory, HTTP health, and `BEGIN TRANSACTION READ ONLY` SQL are allowed.
- Do not run `pull`, `up`, `stop`, `restart`, `down`, `pg_dump`, `pg_restore`, migrations, release helpers, workflow reruns, provider smoke traffic, or backup deletion.
- A previous task's authorization never converts this skill into a mutation workflow. Route authorized release/deploy work to `$prism-release-deploy` and backup/restore work to `$prism-backup-restore`.
