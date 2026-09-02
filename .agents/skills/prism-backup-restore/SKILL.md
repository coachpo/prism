---
name: prism-backup-restore
description: Create, validate, inventory, retain, and restore Prism PostgreSQL and plaintext bootstrap backups with quiesced snapshots, checksum manifests, strict keep-three pruning, and side-by-side database recovery. Use for Prism backup, rollback, restore, or disaster-recovery work; destructive execution always needs current explicit authorization.
metadata:
  short-description: Back up and restore Prism with safe gates
---

# Prism Backup Restore

Automate repeatable backup and recovery mechanics without treating task intent as deletion or restore authorization.

## Backup

1. Run `scripts/prism_backup.py plan --host <host> --service <service>`.
2. Prefer `quiesced` for upgrade/rollback points. `online` is allowed only when the caller accepts that writes after the snapshot are outside the restore point.
3. Execute only with `--confirm-backup <service>`. A verified backup contains the database archive, config copy, archive list, checksums, preflight evidence, and a secret-free manifest written last.
4. If backup fails after stopping the app, restart the original app and verify health.

Read [references/backup-manifest.md](references/backup-manifest.md).

## Restore

1. Run `scripts/prism_restore.py plan --manifest <remote-path> ...` and review the generated target database and confirmation token.
2. Execute only with `--confirm-restore <service>:<manifest-sha-prefix>`.
3. Restore beside the source database, atomically switch only `database.url`, and keep the original database. Never overwrite or drop it automatically.

Read [references/restore.md](references/restore.md).

## Retention

Use `scripts/prism_prune_backups.py`. Keep the newest three verified managed backups per service. Execute only with `--confirm-prune <service>:keep-3`. Read [references/retention.md](references/retention.md).

For `capy`, read [../prism-ops-inspect/references/capy.md](../prism-ops-inspect/references/capy.md).

## Boundaries

- Backup authorization does not authorize restore or prune; restore authorization does not authorize dropping the source database.
- Never print config contents, database URLs, environment values, or credentials.
- Never treat legacy/unmanaged/incomplete backups as retention candidates.
