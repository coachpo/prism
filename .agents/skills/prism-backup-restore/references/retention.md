# Keep-three retention

Retention is per service and runs only after a new managed backup is verified. During rollout, defer it until that service passes deployment, smoke, and observation.

An eligible directory must:

- be a direct, non-symlink child of the exact resolved `<backup-root>/<service>` directory;
- contain a regular `manifest.json` with `schema_version: 1`, `status: verified`, and the matching service;
- contain the complete dump/config/list artifact set and preflight evidence; each actual byte hash must match both the manifest and `SHA256SUMS`;
- not be named by `--protect`, used as the current restore source, or among the newest three eligible backups.

`plan` lists candidates without deleting. `execute` requires the exact `service:keep-3` token, re-runs discovery immediately before deletion, and refuses any path whose real parent differs from the service backup root. Incomplete, legacy, unmanaged, malformed, and symlinked paths remain visible in inventory but are never deleted automatically.
