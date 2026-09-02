# Side-by-side restore

Default recovery preserves the current database.

1. Validate the direct non-symlink manifest/artifact paths, complete artifact set, actual manifest/artifact/preflight hashes, archive list, service identity, config JSON, and absolute `database.url` before returning a plan.
2. Generate or accept a new PostgreSQL identifier such as `prism_restore_YYYYMMDDTHHMMSSZ`; reject existing names and invalid identifiers.
3. Record current app image/config hash, stop the app, create the new database owned by the container's configured PostgreSQL user, and restore with `pg_restore --single-transaction --exit-on-error --no-owner --no-acl`.
4. Verify restored migration history and entity counts against the manifest.
5. Copy the manifest config to a candidate file and replace only the parsed URL path/database name. Preserve scheme, user info, host, port, query, fragment, and every unrelated JSON field.
6. Save the current config in a recovery evidence directory, atomically install the candidate config, start the manifest's source image by default, and require healthy startup.
7. On failure after switching config: stop the candidate app, atomically restore the original config, restart the original image, verify health, and retain the new database for diagnosis.

Success leaves both databases in place. Dropping either database, deleting recovery evidence, or switching again is a separate destructive task with separate authorization.
