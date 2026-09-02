# Evidence contract

## Snapshot fields

The JSON snapshot uses `schema_version: 1` and contains:

- `observed_at`, repository root, HEAD, branch, worktree cleanliness, upstream/remote identity, version surfaces, and matching CI/tag evidence when available;
- per service: Compose project/config path, app and PostgreSQL container identity, image reference and digest evidence, state/health/restarts, published ports, config mount path and SHA-256, PostgreSQL version/latest migration/entity counts/database size, backup inventory, and filesystem capacity;
- `checks`: typed invariant failures; and `limitations`: unavailable tools or evidence.

Default output is stdout. `--output` writes the same JSON atomically with parent directories created only after the caller requested an artifact.

## Secret handling

- Never return container environment values, config contents, database URLs, API keys, auth headers, cookies, tokens, or provider payloads.
- Config and backup artifacts are represented by paths, modes, sizes, timestamps, and SHA-256 only.
- Command errors are sanitized with case-insensitive redaction for password, passwd, token, secret, api key, credential, authorization, cookie, and database URL material.
- Do not use `docker inspect` output wholesale; select named fields.

## Check semantics

`--check` exits nonzero when a requested service is missing/ambiguous, its app or PostgreSQL container cannot be uniquely resolved, health is not healthy, the config mount is missing, migration history is unavailable, or secret-bearing output is detected. Ordinary differences between A/B configuration are reported, not treated as drift.
