# Backup manifest

A managed backup directory is mode `0700`; its files are mode `0600`. `manifest.json` is written atomically last and uses schema version 1.

Required artifacts:

- `database.dump`: PostgreSQL custom archive;
- `database.list`: successful `pg_restore --list` output;
- `config.json`: exact plaintext bootstrap copy;
- `SHA256SUMS`: database, list, and config hashes generated and independently re-read from the backup directory;
- `preflight.json`: image, health, restart count, database size, migration versions, entity counts, config hash, and topology identifiers without secret values;
- `manifest.json`: `status: verified`, service/host/timestamps, backup mode/compression, safe relative filenames and hashes, source image reference, schema versions, entity counts, and config hash.

The backup remains `.incomplete` and is not usable if any step fails. Manifest paths are relative and may not escape the backup directory. `pg_dump`, `pg_restore`, PostgreSQL, and config-copy errors are fatal.

The source image is resolved to `tag@sha256:digest` even when the container was configured with a tag. For a quiesced backup, stop only the app with a graceful timeout, keep PostgreSQL healthy, then capture counts/config evidence and start the archive; the evidence is marked `quiesced_exact`. Online evidence is marked `online_advisory` because concurrent writes may advance after the observation. Leave the app stopped on successful quiesced backup for the caller's cutover unless restart-on-success was explicitly requested.
