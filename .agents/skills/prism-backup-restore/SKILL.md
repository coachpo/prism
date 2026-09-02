---
name: prism-backup-restore
description: Create, validate, inventory, retain, and restore Prism PostgreSQL and plaintext bootstrap backups with quiesced snapshots, checksum manifests, strict keep-three pruning, and side-by-side database recovery. Use for Prism backup, rollback, restore, or disaster-recovery work; destructive execution always needs current explicit authorization.
metadata:
  short-description: Back up and restore Prism with safe gates
---

# Prism Backup Restore

## Outcome

Produce a verified, secret-safe Prism backup; enforce managed keep-three retention when separately authorized; or restore beside the source database while preserving a tested path back to the original app.

## Authorization

- Plan, inventory, validation, and capacity checks are read-only.
- Backup execution requires current authorization plus `--confirm-backup <service>`.
- Restore and prune are separate destructive scopes requiring `--confirm-restore <service>:<manifest-sha-prefix>` or `--confirm-prune <service>:keep-3` respectively.
- Never infer authorization from another operation or task. Restore does not authorize dropping the source database.

## Backup

1. Run `scripts/prism_backup.py plan --host <host> --service <service>`.
2. Prefer `quiesced` for upgrade/rollback points. `online` is allowed only when the caller accepts that writes after the snapshot are outside the restore point.
3. Read [references/backup-manifest.md](references/backup-manifest.md), then execute the reviewed plan. A valid backup writes its verified manifest last.
4. If backup or app recovery fails, report both failures and the final observed service state.

## Restore

1. Run `scripts/prism_restore.py plan --manifest <remote-path> ...` and review the generated target database and confirmation token.
2. Read [references/restore.md](references/restore.md), then execute the reviewed target and manifest.
3. Restore beside the source database, switch only `database.url`, and retain the original database and recovery evidence.

## Retention

Use `scripts/prism_prune_backups.py` with [references/retention.md](references/retention.md). Keep the newest three complete, byte-verified managed backups per service; exclude incomplete, legacy, unmanaged, malformed, symlinked, and protected paths.

For `capy`, read [../prism-ops-inspect/references/capy.md](../prism-ops-inspect/references/capy.md).

## Completion

- Return config and credential evidence only as hashes or metadata; never expose config contents, database URLs, environment values, credentials, or auth material.
- Lead with the operation outcome. Include manifest and backup identities, verification results, retained/deleted paths, source and target database disposition, app health, recovery failures, limitations, and the next required action.
